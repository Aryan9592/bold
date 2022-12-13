package validator

import (
	"context"
	"fmt"

	"github.com/OffchainLabs/new-rollup-exploration/protocol"
	"github.com/OffchainLabs/new-rollup-exploration/util"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Processes new challenge creation events from the protocol that were not created by self.
// This will fetch the challenge, its parent assertion, and create a challenge leaf that is
// relevant towards resolving the challenge. We then spawn a challenge tracker in the background.
func (v *Validator) onChallengeStarted(
	ctx context.Context, ev *protocol.StartChallengeEvent,
) error {
	if ev == nil {
		return nil
	}
	// Ignore challenges initiated by self.
	if isFromSelf(v.address, ev.Validator) {
		return nil
	}

	challenge, err := v.fetchProtocolChallenge(
		ctx,
		ev.ParentSeqNum,
		ev.ParentStateCommitment,
	)
	if err != nil {
		return err
	}

	// We then add a challenge vertex to the challenge.
	challengeVertex, err := v.addChallengeVertex(ctx, challenge)
	if err != nil {
		if errors.Is(err, protocol.ErrVertexAlreadyExists) {
			log.Infof(
				"Attempted to add a challenge leaf that already exists to challenge with "+
					"parent state commit: height=%d, stateRoot=%#x",
				challenge.ParentStateCommitment().Height,
				challenge.ParentStateCommitment().StateRoot,
			)
			return nil
		}
		return err
	}

	challengerName := "unknown-name"
	staker := challengeVertex.Challenger
	if name, ok := v.knownValidatorNames[staker]; ok {
		challengerName = name
	}
	log.WithFields(logrus.Fields{
		"name":                 v.name,
		"challenger":           challengerName,
		"challengingStateRoot": fmt.Sprintf("%#x", challenge.ParentStateCommitment().StateRoot),
		"challengingHeight":    challenge.ParentStateCommitment().Height,
	}).Warn("Received challenge for a created leaf, added own leaf with history commitment")

	// Start tracking the challenge.
	v.spawnBlockChallenge(ctx, challenge, challengeVertex)

	return nil
}

// Initiates a challenge on an assertion added to the protocol by finding its parent assertion
// and starting a challenge transaction. If the challenge creation is successful, we add a leaf
// with an associated history commitment to it and spawn a challenge tracker in the background.
func (v *Validator) challengeAssertion(ctx context.Context, ev *protocol.CreateLeafEvent) error {
	var challenge *protocol.Challenge
	var err error
	challenge, err = v.submitProtocolChallenge(ctx, ev.PrevSeqNum)
	if err != nil {
		if errors.Is(err, protocol.ErrChallengeAlreadyExists) {
			existingChallenge, fetchErr := v.fetchProtocolChallenge(ctx, ev.PrevSeqNum, ev.PrevStateCommitment)
			if fetchErr != nil {
				return fetchErr
			}
			challenge = existingChallenge
		} else {
			return err
		}
	}

	// We then add a challenge vertex to the challenge.
	challengeVertex, err := v.addChallengeVertex(ctx, challenge)
	if err != nil {
		return err
	}
	if errors.Is(err, protocol.ErrVertexAlreadyExists) {
		log.Infof(
			"Attempted to add a challenge leaf that already exists to challenge with "+
				"parent state commit: height=%d, stateRoot=%#x",
			challenge.ParentStateCommitment().Height,
			challenge.ParentStateCommitment().StateRoot,
		)
		return nil
	}

	// Start tracking the challenge.
	v.spawnBlockChallenge(ctx, challenge, challengeVertex)

	logFields := logrus.Fields{}
	logFields["name"] = v.name
	logFields["parentAssertionSeqNum"] = ev.PrevSeqNum
	logFields["parentAssertionStateRoot"] = fmt.Sprintf("%#x", ev.PrevStateCommitment.StateRoot)
	logFields["challengeID"] = fmt.Sprintf("%#x", ev.PrevStateCommitment.Hash())
	log.WithFields(logFields).Info("Successfully created challenge and added leaf, now tracking events")

	return nil
}

func (v *Validator) addChallengeVertex(
	ctx context.Context,
	challenge *protocol.Challenge,
) (*protocol.ChallengeVertex, error) {
	latestValidAssertionSeq := v.findLatestValidAssertion(ctx)

	var assertion *protocol.Assertion
	var err error
	if err = v.chain.Call(func(tx *protocol.ActiveTx, p protocol.OnChainProtocol) error {
		assertion, err = p.AssertionBySequenceNum(tx, latestValidAssertionSeq)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}

	historyCommit, err := v.stateManager.HistoryCommitmentUpTo(ctx, assertion.StateCommitment.Height)
	if err != nil {
		return nil, err
	}

	var challengeVertex *protocol.ChallengeVertex
	if err = v.chain.Tx(func(tx *protocol.ActiveTx, p protocol.OnChainProtocol) error {
		challengeVertex, err = challenge.AddLeaf(tx, assertion, historyCommit, v.address)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, errors.Wrapf(
			err,
			"could add challenge vertex to challenge with parent state commitment: height=%d, stateRoot=%#x",
			challenge.ParentStateCommitment().Height,
			challenge.ParentStateCommitment().StateRoot,
		)
	}
	return challengeVertex, nil
}

func (v *Validator) submitProtocolChallenge(
	ctx context.Context,
	parentAssertionSeqNum protocol.AssertionSequenceNumber,
) (*protocol.Challenge, error) {
	var challenge *protocol.Challenge
	var err error
	if err = v.chain.Tx(func(tx *protocol.ActiveTx, p protocol.OnChainProtocol) error {
		parentAssertion, readErr := p.AssertionBySequenceNum(tx, parentAssertionSeqNum)
		if readErr != nil {
			return readErr
		}
		challenge, err = parentAssertion.CreateChallenge(tx, ctx, v.address)
		if err != nil {
			return errors.Wrap(err, "could not submit challenge")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return challenge, nil
}

// Tries to retrieve a challenge from the protocol on-chain
// based on the parent assertion's state commitment hash.
func (v *Validator) fetchProtocolChallenge(
	ctx context.Context,
	parentAssertionSeqNum protocol.AssertionSequenceNumber,
	parentAssertionCommit protocol.StateCommitment,
) (*protocol.Challenge, error) {
	var err error
	var challenge *protocol.Challenge
	if err = v.chain.Call(func(tx *protocol.ActiveTx, p protocol.OnChainProtocol) error {
		challenge, err = p.ChallengeByCommitHash(
			tx,
			protocol.CommitHash(parentAssertionCommit.Hash()),
		)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "could not get challenge from protocol")
	}
	if challenge == nil {
		return nil, errors.New("got nil challenge from protocol")
	}
	return challenge, nil
}

// Spawns a block challenge worker in the background to manage the lifecycle of a specified challenge.
// This will worker will subscribe to events relevant to the challenge and perform the required actions
// as a participant in the protocol until the challenge is resolved in the background.
func (v *Validator) spawnBlockChallenge(
	ctx context.Context,
	challenge *protocol.Challenge,
	vertex *protocol.ChallengeVertex,
) {
	v.challengesLock.Lock()
	ch := make(chan protocol.ChallengeEvent, v.challengeEventsBufSize)
	v.chain.SubscribeChallengeEvents(ctx, ch)
	id := challenge.ParentStateCommitment().Hash()
	if _, ok := v.challenges[protocol.CommitHash(id)]; ok {
		v.challengesLock.Unlock()
		log.WithFields(logrus.Fields{
			"challengeParentAssertionStateCommit": fmt.Sprintf("%#x", id),
			"name":                                v.name,
		}).Error("Attempted to spawn challenge that is already in progress")
		return
	}
	vertices := util.NewThreadSafeSlice[*protocol.ChallengeVertex]()
	vertices.Append(vertex)
	worker := &blockChallengeWorker{
		challenge:          challenge,
		createdVertices:    vertices,
		validatorAddress:   v.address,
		reachedOneStepFork: make(chan struct{}),
		validatorName:      v.name,
		events:             ch,
	}
	v.challenges[protocol.CommitHash(id)] = worker
	v.challengesLock.Unlock()
	log.WithFields(logrus.Fields{
		"challengeID": fmt.Sprintf("%#x", id),
		"name":        v.name,
	}).Info("Spawning challenge lifecycle manager")
	go worker.runChallengeLifecycle(ctx, v, ch)
}
