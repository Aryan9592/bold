package validator

import (
	"context"
	"fmt"

	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	"github.com/OffchainLabs/challenge-protocol-v2/util"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// Determines the bisection point from parentHeight to toHeight and returns a history
// commitment with a prefix proof for the action based on the challenge type.
func (v *vertexTracker) determineBisectionHistoryWithProof(
	ctx context.Context,
	parentHeight,
	toHeight uint64,
) (util.HistoryCommitment, []byte, error) {
	bisectTo, err := util.BisectionPoint(parentHeight, toHeight)
	if err != nil {
		return util.HistoryCommitment{}, nil, errors.Wrapf(err, "determining bisection point failed for %d and %d", parentHeight, toHeight)
	}

	if v.challenge.GetType() == protocol.BlockChallenge {
		historyCommit, commitErr := v.cfg.stateManager.HistoryCommitmentUpTo(ctx, bisectTo)
		if commitErr != nil {
			return util.HistoryCommitment{}, nil, commitErr
		}
		proof, proofErr := v.cfg.stateManager.PrefixProof(ctx, bisectTo, toHeight)
		if proofErr != nil {
			return util.HistoryCommitment{}, nil, proofErr
		}
		return historyCommit, proof, nil
	}
	topLevelClaimVertex, err := v.challenge.TopLevelClaimVertex(ctx)
	if err != nil {
		return util.HistoryCommitment{}, nil, err
	}

	fromAssertionHeight := topLevelClaimVertex.HistoryCommitment().Height
	toAssertionHeight := fromAssertionHeight + 1

	var historyCommit util.HistoryCommitment
	var commitErr error
	var proof []byte
	var proofErr error
	switch v.challenge.GetType() {
	case protocol.BigStepChallenge:
		historyCommit, commitErr = v.cfg.stateManager.BigStepCommitmentUpTo(ctx, fromAssertionHeight, toAssertionHeight, bisectTo)
		proof, proofErr = v.cfg.stateManager.BigStepPrefixProof(ctx, fromAssertionHeight, toAssertionHeight, bisectTo, toHeight)
	case protocol.SmallStepChallenge:
		historyCommit, commitErr = v.cfg.stateManager.SmallStepCommitmentUpTo(ctx, fromAssertionHeight, toAssertionHeight, bisectTo)
		proof, proofErr = v.cfg.stateManager.SmallStepPrefixProof(ctx, fromAssertionHeight, toAssertionHeight, bisectTo, toHeight)
	default:
		return util.HistoryCommitment{}, nil, fmt.Errorf("unsupported challenge type: %s", v.challenge.GetType())
	}
	if commitErr != nil {
		return util.HistoryCommitment{}, nil, commitErr
	}
	if proofErr != nil {
		return util.HistoryCommitment{}, nil, proofErr
	}
	return historyCommit, proof, nil
}

// Performs a bisection move during a BlockChallenge in the assertion protocol given
// a validator challenge vertex. It will create a historical commitment for the vertex
// the validator wants to bisect to and an associated proof for submitting to the goimpl.
func (v *vertexTracker) bisect(
	ctx context.Context,
	validatorChallengeVertex protocol.ChallengeVertex,
) (protocol.ChallengeVertex, error) {
	commitment := validatorChallengeVertex.HistoryCommitment()
	toHeight := commitment.Height
	prev, err := validatorChallengeVertex.Prev(ctx)
	if err != nil {
		return nil, err
	}
	prevCommitment := prev.Unwrap().HistoryCommitment()
	parentHeight := prevCommitment.Height

	historyCommit, proof, err := v.determineBisectionHistoryWithProof(ctx, parentHeight, toHeight)
	if err != nil {
		return nil, err
	}
	bisectTo := historyCommit.Height
	bisected, err := validatorChallengeVertex.Bisect(ctx, historyCommit, proof)
	if err != nil {
		couldNotBisectErr := err
		validatorChallengeVertexHistoryCommitment := validatorChallengeVertex.HistoryCommitment()
		return nil, errors.Wrapf(
			couldNotBisectErr,
			"%s could not bisect to height=%d,commit=%s from height=%d,commit=%s",
			v.cfg.validatorName,
			bisectTo,
			util.Trunc(historyCommit.Merkle.Bytes()),
			validatorChallengeVertexHistoryCommitment.Height,
			util.Trunc(validatorChallengeVertexHistoryCommitment.Merkle.Bytes()),
		)
	}
	bisectedVertexIsPresumptiveSuccessor, err := bisected.IsPresumptiveSuccessor(ctx)
	if err != nil {
		return nil, err
	}
	isPresumptive := bisectedVertexIsPresumptiveSuccessor
	bisectedVertexCommitment := bisected.HistoryCommitment()
	validatorChallengeVertexHistoryCommitment := validatorChallengeVertex.HistoryCommitment()
	log.WithFields(logrus.Fields{
		"name":               v.cfg.validatorName,
		"challengeType":      v.challenge.GetType(),
		"isPs":               isPresumptive,
		"bisectedFrom":       validatorChallengeVertexHistoryCommitment.Height,
		"bisectedFromMerkle": util.Trunc(validatorChallengeVertexHistoryCommitment.Merkle.Bytes()),
		"bisectedTo":         bisectedVertexCommitment.Height,
		"bisectedToMerkle":   util.Trunc(bisectedVertexCommitment.Merkle[:]),
	}).Info("Successfully bisected to vertex")
	return bisected, nil
}

// Performs a merge move during a BlockChallenge in the assertion protocol given
// a challenge vertex and the sequence number we should be merging into. To do this, we
// also need to fetch vertex we are merging to by reading it from the goimpl.
func (v *vertexTracker) merge(
	ctx context.Context,
	mergingToCommit util.HistoryCommitment,
	proof []byte,
) (protocol.ChallengeVertex, error) {
	mergedTo, err := v.vertex.Merge(ctx, mergingToCommit, proof)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			"%s could not merge vertex at height=%d,commit=%s to height%d,commit=%s",
			v.cfg.validatorName,
			v.vertex.HistoryCommitment().Height,
			util.Trunc(v.vertex.HistoryCommitment().Merkle.Bytes()),
			mergingToCommit.Height,
			util.Trunc(mergingToCommit.Merkle.Bytes()),
		)
	}
	log.WithFields(logrus.Fields{
		"name":             v.cfg.validatorName,
		"mergedFrom":       v.vertex.HistoryCommitment().Height,
		"challengeType":    v.challenge.GetType(),
		"mergedFromMerkle": util.Trunc(v.vertex.HistoryCommitment().Merkle.Bytes()),
		"mergedTo":         mergingToCommit.Height,
		"mergedToMerkle":   util.Trunc(mergingToCommit.Merkle[:]),
	}).Info("Successfully merged to vertex")
	return mergedTo, nil
}
