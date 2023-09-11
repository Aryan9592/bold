<<<<<<< HEAD
package stateprovider

import (
	"context"
	"testing"

	"github.com/OffchainLabs/bold/containers/option"
	l2stateprovider "github.com/OffchainLabs/bold/layer2-state-provider"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var (
	_ l2stateprovider.L2MessageStateCollector = (*L2StateBackend)(nil)
	_ l2stateprovider.MachineHashCollector    = (*L2StateBackend)(nil)
)

func TestHistoryCommitment(t *testing.T) {
	ctx := context.Background()
	wasmModuleRoot := common.Hash{}
	challengeLeafHeights := []l2stateprovider.Height{
		4,
		8,
		16,
	}
	numStates := uint64(10)
	states, _ := setupStates(t, numStates, 0 /* honest */)
	stateBackend, err := newTestingMachine(
		states,
		WithMaxWavmOpcodesPerBlock(uint64(challengeLeafHeights[1]*challengeLeafHeights[2])),
		WithNumOpcodesPerBigStep(uint64(challengeLeafHeights[2])),
		WithMachineAtBlockProvider(mockMachineAtBlock),
		WithForceMachineBlockCompat(),
	)
	require.NoError(t, err)
	stateBackend.challengeLeafHeights = challengeLeafHeights

	provider := l2stateprovider.NewHistoryCommitmentProvider(
		stateBackend,
		stateBackend,
		challengeLeafHeights,
		stateBackend,
		stateBackend,
	)
	_, err = provider.HistoryCommitment(
		ctx,
		wasmModuleRoot,
		0,
		nil, // No start heights provided.
		option.None[l2stateprovider.Height](),
	)
	require.ErrorContains(t, err, "must provide start height")

	t.Run("produces a block challenge commitment with height equal to leaf height const", func(t *testing.T) {
		got, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0},
			option.None[l2stateprovider.Height](),
		)
		require.NoError(t, err)
		require.Equal(t, uint64(challengeLeafHeights[0]), got.Height)
	})
	t.Run("produces a block challenge commitment with height up to", func(t *testing.T) {
		got, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0},
			option.Some(l2stateprovider.Height(2)),
		)
		require.NoError(t, err)
		require.Equal(t, uint64(2), got.Height)
	})
	t.Run("produces a subchallenge history commitment with claims matching higher level start end leaves", func(t *testing.T) {
		blockChallengeCommit, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0},
			option.Some(l2stateprovider.Height(1)),
		)
		require.NoError(t, err)

		subChallengeCommit, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0, 0},
			option.None[l2stateprovider.Height](),
		)
		require.NoError(t, err)

		require.Equal(t, uint64(challengeLeafHeights[1]), subChallengeCommit.Height)
		require.Equal(t, blockChallengeCommit.FirstLeaf, subChallengeCommit.FirstLeaf)
		require.Equal(t, blockChallengeCommit.LastLeaf, subChallengeCommit.LastLeaf)
	})
	t.Run("produces a small step challenge commit", func(t *testing.T) {
		blockChallengeCommit, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0},
			option.Some(l2stateprovider.Height(1)),
		)
		require.NoError(t, err)

		smallStepSubchallengeCommit, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0, 0, 0},
			option.None[l2stateprovider.Height](),
		)
		require.NoError(t, err)

		require.Equal(t, uint64(challengeLeafHeights[2]), smallStepSubchallengeCommit.Height)
		require.Equal(t, blockChallengeCommit.FirstLeaf, smallStepSubchallengeCommit.FirstLeaf)
	})
}
||||||| a907170c
=======
package stateprovider

import (
	"context"
	"testing"

	"github.com/OffchainLabs/bold/containers/option"
	l2stateprovider "github.com/OffchainLabs/bold/layer2-state-provider"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var (
	_ l2stateprovider.L2MessageStateCollector = (*L2StateBackend)(nil)
	_ l2stateprovider.MachineHashCollector    = (*L2StateBackend)(nil)
)

func TestHistoryCommitment(t *testing.T) {
	ctx := context.Background()
	wasmModuleRoot := common.Hash{}
	challengeLeafHeights := []l2stateprovider.Height{
		4,
		8,
		16,
	}
	numStates := uint64(10)
	states, _ := setupStates(t, numStates, 0 /* honest */)
	stateBackend, err := newTestingMachine(
		states,
		WithMaxWavmOpcodesPerBlock(uint64(challengeLeafHeights[1]*challengeLeafHeights[2])),
		WithNumOpcodesPerBigStep(uint64(challengeLeafHeights[2])),
		WithMachineAtBlockProvider(mockMachineAtBlock),
		WithForceMachineBlockCompat(),
	)
	require.NoError(t, err)
	stateBackend.challengeLeafHeights = challengeLeafHeights

	provider := l2stateprovider.NewHistoryCommitmentProvider(
		stateBackend,
		stateBackend,
		challengeLeafHeights,
	)
	_, err = provider.HistoryCommitment(
		ctx,
		wasmModuleRoot,
		0,
		nil, // No start heights provided.
		option.None[l2stateprovider.Height](),
	)
	require.ErrorContains(t, err, "must provide start height")

	t.Run("produces a block challenge commitment with height equal to leaf height const", func(t *testing.T) {
		got, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0},
			option.None[l2stateprovider.Height](),
		)
		require.NoError(t, err)
		require.Equal(t, uint64(challengeLeafHeights[0]), got.Height)
	})
	t.Run("produces a block challenge commitment with height up to", func(t *testing.T) {
		got, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0},
			option.Some(l2stateprovider.Height(2)),
		)
		require.NoError(t, err)
		require.Equal(t, uint64(2), got.Height)
	})
	t.Run("produces a subchallenge history commitment with claims matching higher level start end leaves", func(t *testing.T) {
		blockChallengeCommit, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0},
			option.Some(l2stateprovider.Height(1)),
		)
		require.NoError(t, err)

		subChallengeCommit, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0, 0},
			option.None[l2stateprovider.Height](),
		)
		require.NoError(t, err)

		require.Equal(t, uint64(challengeLeafHeights[1]), subChallengeCommit.Height)
		require.Equal(t, blockChallengeCommit.FirstLeaf, subChallengeCommit.FirstLeaf)
		require.Equal(t, blockChallengeCommit.LastLeaf, subChallengeCommit.LastLeaf)
	})
	t.Run("produces a small step challenge commit", func(t *testing.T) {
		blockChallengeCommit, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0},
			option.Some(l2stateprovider.Height(1)),
		)
		require.NoError(t, err)

		smallStepSubchallengeCommit, err := provider.HistoryCommitment(
			ctx,
			wasmModuleRoot,
			l2stateprovider.Batch(1),
			[]l2stateprovider.Height{0, 0, 0},
			option.None[l2stateprovider.Height](),
		)
		require.NoError(t, err)

		require.Equal(t, uint64(challengeLeafHeights[2]), smallStepSubchallengeCommit.Height)
		require.Equal(t, blockChallengeCommit.FirstLeaf, smallStepSubchallengeCommit.FirstLeaf)
	})
}
>>>>>>> main
