package validator

import (
	"context"
	"testing"

	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	statemanager "github.com/OffchainLabs/challenge-protocol-v2/state-manager"
	"github.com/stretchr/testify/require"
)

func TestFullChallengeResolution(t *testing.T) {
	ctx := context.Background()

	// Start by creating a simple, two validator fork in the assertion
	// chain at height 1.
	createdData := createTwoValidatorFork(t, ctx, &createForkConfig{
		numBlocks:     1,
		divergeHeight: 1,
	})
	t.Log("Alice (honest) and Bob have a fork at height 1")
	honestManager, err := statemanager.New(
		createdData.honestValidatorStateRoots,
		statemanager.WithNumOpcodesPerBigStep(1),
		statemanager.WithMaxWavmOpcodesPerBlock(1),
	)
	require.NoError(t, err)

	evilManager, err := statemanager.New(
		createdData.evilValidatorStateRoots,
		statemanager.WithNumOpcodesPerBigStep(1),
		statemanager.WithMaxWavmOpcodesPerBlock(1),
		statemanager.WithBigStepStateDivergenceHeight(1),
		statemanager.WithSmallStepStateDivergenceHeight(1),
	)
	require.NoError(t, err)

	// Next, we create a challenge.
	honestChain := createdData.assertionChains[1]
	chal, err := honestChain.CreateSuccessionChallenge(ctx, 0)
	require.NoError(t, err)

	challengeType := chal.GetType()
	require.Equal(t, protocol.BlockChallenge, challengeType)
	t.Log("Created BlockChallenge")

	createdDataLeaf1Height, err := createdData.leaf1.Height()
	require.NoError(t, err)
	commit1, err := honestManager.HistoryCommitmentUpTo(ctx, createdDataLeaf1Height)
	require.NoError(t, err)
	createdDataLeaf2Height, err := createdData.leaf2.Height()
	require.NoError(t, err)
	commit2, err := evilManager.HistoryCommitmentUpTo(ctx, createdDataLeaf2Height)
	require.NoError(t, err)

	vertex1, err := chal.AddBlockChallengeLeaf(ctx, createdData.leaf1, commit1)
	require.NoError(t, err)
	t.Log("Alice (honest) added leaf at height 1")
	vertex2, err := chal.AddBlockChallengeLeaf(ctx, createdData.leaf2, commit2)
	require.NoError(t, err)
	t.Log("Bob added leaf at height 1")

	parentVertex, err := chal.RootVertex(ctx)
	require.NoError(t, err)

	areAtOSF, err := parentVertex.ChildrenAreAtOneStepFork(ctx)
	require.NoError(t, err)
	require.Equal(t, true, areAtOSF, "Children not at one-step fork")

	t.Log("Alice and Bob's BlockChallenge vertices that are at a one-step-fork")

	subChal, err := parentVertex.CreateSubChallenge(ctx)
	require.NoError(t, err)

	subChalType := subChal.GetType()
	require.NoError(t, err)
	require.Equal(t, protocol.BigStepChallenge, subChalType)
	t.Log("Created BigStepChallenge")

	commit1, err = honestManager.BigStepLeafCommitment(ctx, 0, 1)
	require.NoError(t, err)
	commit2, err = evilManager.BigStepLeafCommitment(ctx, 0, 1)
	require.NoError(t, err)

	vertex1, err = subChal.AddSubChallengeLeaf(ctx, vertex1, commit1)
	require.NoError(t, err)
	t.Log("Alice (honest) added leaf at height 1")
	vertex2, err = subChal.AddSubChallengeLeaf(ctx, vertex2, commit2)
	require.NoError(t, err)
	t.Log("Bob added leaf at height 1")

	parentVertex, err = subChal.RootVertex(ctx)
	require.NoError(t, err)

	areAtOSF, err = parentVertex.ChildrenAreAtOneStepFork(ctx)
	require.NoError(t, err)
	require.Equal(t, true, areAtOSF, "Children in BigStepChallenge not at one-step fork")

	t.Log("Alice and Bob's BigStepChallenge vertices are at a one-step-fork")

	subChal, err = parentVertex.CreateSubChallenge(ctx)
	require.NoError(t, err)

	subChalGetType := subChal.GetType()
	require.NoError(t, err)
	require.Equal(t, protocol.SmallStepChallenge, subChalGetType)
	t.Log("Created SmallStepChallenge")

	commit1, err = honestManager.SmallStepLeafCommitment(ctx, 0, 1)
	require.NoError(t, err)
	commit2, err = evilManager.SmallStepLeafCommitment(ctx, 0, 1)
	require.NoError(t, err)

	_, err = subChal.AddSubChallengeLeaf(ctx, vertex1, commit1)
	require.NoError(t, err)
	t.Log("Alice (honest) added leaf at height 1")
	_, err = subChal.AddSubChallengeLeaf(ctx, vertex2, commit2)
	require.NoError(t, err)
	t.Log("Bob added leaf at height 1")

	parentVertex, err = subChal.RootVertex(ctx)
	require.NoError(t, err)

	areAtOSF, err = parentVertex.ChildrenAreAtOneStepFork(ctx)
	require.NoError(t, err)
	require.Equal(t, true, areAtOSF, "Children in SmallStepChallenge not at one-step fork")

	t.Log("Alice and Bob's BigStepChallenge vertices are at a one-step-fork")
	t.Log("Reached one-step-proof in SmallStepChallenge")
}
