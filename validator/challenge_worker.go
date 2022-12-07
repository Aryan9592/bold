package validator

import (
	"context"
	"fmt"

	"github.com/OffchainLabs/new-rollup-exploration/protocol"
	"github.com/OffchainLabs/new-rollup-exploration/util"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Each challenge has a lifecycle we need to manage. A single challenge's entire lifecycle should
// be managed in a goroutine specific to that challenge. A challenge goroutine will exit if
//
// - A winner has been found (meaning all subchallenges are resolved), or
// - The validator's chess clock times out
//
// The validator has is able to dispatch events from the global feed
// to specific challenge goroutines. A challenge goroutine is spawned upon receiving
// a ChallengeStarted event.
type challengeWorker struct {
	challenge          *protocol.Challenge
	validatorAddress   common.Address
	reachedOneStepFork chan struct{}
	validatorName      string
	createdVertices    *util.ThreadSafeSlice[*protocol.ChallengeVertex]
	events             chan protocol.ChallengeEvent
}

func (w *challengeWorker) runChallengeLifecycle(
	ctx context.Context,
	v *Validator,
	blockChallengeEvents chan protocol.ChallengeEvent,
) {
	for {
		select {
		// When we receive events related to a BlockChallenge, we take the required actions.
		case genericEvent := <-blockChallengeEvents:
			var address common.Address
			var history util.HistoryCommitment
			var seqNum protocol.SequenceNum

			// Extract the values we need from the challenge event to act on a block challenge.
			switch ev := genericEvent.(type) {
			case *protocol.ChallengeLeafEvent:
				address = ev.Validator
				history = ev.History
				seqNum = ev.SequenceNum
			case *protocol.ChallengeBisectEvent:
				address = ev.Validator
				history = ev.History
				seqNum = ev.SequenceNum
			case *protocol.ChallengeMergeEvent:
				address = ev.Validator
				history = ev.History
				seqNum = ev.ShallowerSequenceNum
			default:
				log.WithField("ev", fmt.Sprintf("%+v", ev)).Error("Not a recognized challenge event")
			}
			go func() {
				if err := w.actOnBlockChallenge(ctx, v, address, history, seqNum); err != nil {
					log.WithError(err).Error("Could not process challenge leaf added event")
				}
			}()
		// TODO: Add cases for subchallenges.
		case <-w.reachedOneStepFork:
			log.WithField(
				"name", w.validatorName,
			).Infof("Reached a one-step-fork in the challenge, now awaiting subchallenge resolution")
			// TODO: Trigger subchallenge!
			return
		case <-ctx.Done():
			return
		}
	}
}

func (w *challengeWorker) actOnBlockChallenge(
	ctx context.Context,
	validator *Validator,
	eventActor common.Address,
	eventHistoryCommit util.HistoryCommitment,
	eventSequenceNum protocol.SequenceNum,
) error {
	if isFromSelf(w.validatorAddress, eventActor) {
		return nil
	}
	if w.createdVertices.Empty() {
		return nil
	}
	// Go down the tree to find the first vertex we created that has a commitment height >
	// the vertex seen from the merge event.
	vertexToActUpon := w.createdVertices.Last().Unwrap()
	numVertices := w.createdVertices.Len()
	for i := numVertices - 1; i > 0; i-- {
		vertex := w.createdVertices.Get(i).Unwrap()
		if vertex.Commitment.Height > eventHistoryCommit.Height {
			vertexToActUpon = vertex
			break
		}
	}

	mergedToOurs := eventHistoryCommit.Hash() == vertexToActUpon.Commitment.Hash()
	if mergedToOurs {
		log.WithFields(logrus.Fields{
			"name":                w.validatorName,
			"mergedHeight":        eventHistoryCommit.Height,
			"mergedHistoryMerkle": eventHistoryCommit.Merkle,
		}).Info("Other validator merged to our vertex")
	}

	// Make a merge move.
	if validator.stateManager.HasHistoryCommitment(ctx, eventHistoryCommit) && !mergedToOurs {
		if err := validator.merge(ctx, w.challenge, vertexToActUpon, eventSequenceNum); err != nil {
			// TODO: Find a better way to exit if a merge is invalid than showing a scary log to the user.
			// Validators currently try to make merge moves they should not during the challenge game.
			if errors.Is(err, protocol.ErrInvalid) {
				return nil
			}
			return errors.Wrap(err, "failed to merge")
		}
	}

	hasPresumptiveSuccessor := vertexToActUpon.IsPresumptiveSuccessor()
	currentVertex := vertexToActUpon

	for !hasPresumptiveSuccessor {
		if currentVertex.Commitment.Height == currentVertex.Prev.Commitment.Height+1 {
			w.reachedOneStepFork <- struct{}{}
			break
		}
		bisectedVertex, err := validator.bisect(ctx, currentVertex)
		if err != nil {
			// TODO: Find another way of cleanly ending the bisection process so that we do not
			// end on a scary "state did not allow this operation" log.
			if errors.Is(err, protocol.ErrWrongState) {
				log.WithError(err).Debug("State incorrect for bisection")
				return nil
			}
			if errors.Is(err, protocol.ErrVertexAlreadyExists) {
				return nil
			}
			return err
		}
		currentVertex = bisectedVertex
		w.createdVertices.Append(currentVertex)
	}
	return nil
}
