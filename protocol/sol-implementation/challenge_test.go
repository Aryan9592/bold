package solimpl

import (
	"context"
	"math/big"
	"testing"

	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	"github.com/OffchainLabs/challenge-protocol-v2/util"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var _ = protocol.Challenge(&Challenge{})

func TestChallenge_BlockChallenge_AddLeaf(t *testing.T) {
	ctx := context.Background()
	tx := &activeTx{readWriteTx: true}
	height1 := uint64(1)
	height2 := uint64(1)
	a1, _, challenge, chain1, _ := setupTopLevelFork(t, ctx, height1, height2)
	t.Run("claim predecessor not linked to challenge", func(t *testing.T) {
		_, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			&Assertion{
				chain: chain1,
				id:    20,
			},
			util.HistoryCommitment{
				Height: height1,
				Merkle: common.BytesToHash([]byte("bar")),
			},
		)
		require.ErrorContains(t, err, "INVALID_ASSERTION_NUM")
	})
	t.Run("invalid height", func(t *testing.T) {
		// Pass in a junk assertion that has no predecessor.
		_, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			&Assertion{
				chain: chain1,
				id:    1,
			},
			util.HistoryCommitment{
				Height: 0,
				Merkle: common.BytesToHash([]byte("bar")),
			},
		)
		require.ErrorContains(t, err, "Invalid height")
	})
	t.Run("last state is not assertion claim block hash", func(t *testing.T) {
		t.Skip("Needs proofs implemented in solidity")
	})
	t.Run("winner already declared", func(t *testing.T) {
		t.Skip("Needs winner declaration logic implemented in solidity")
	})
	t.Run("last state not in history", func(t *testing.T) {
		t.Skip()
	})
	t.Run("first state not in history", func(t *testing.T) {
		t.Skip()
	})
	t.Run("OK", func(t *testing.T) {
		assertionNode, err := a1.fetchAssertionNode()
		require.NoError(t, err)
		history := util.HistoryCommitment{
			Height:        height1,
			Merkle:        common.BytesToHash([]byte("nyan")),
			LastLeaf:      assertionNode.StateHash,
			LastLeafProof: []common.Hash{assertionNode.StateHash},
		}
		_, err = challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a1,
			history,
		)
		require.NoError(t, err)

		v, err := challenge.RootVertex(ctx, tx)
		require.NoError(t, err)
		manager, err := a1.chain.CurrentChallengeManager(ctx, tx)
		require.NoError(t, err)
		caller, err := manager.GetCaller(ctx, tx)
		require.NoError(t, err)
		vertexId, err := caller.CalculateChallengeVertexId(
			challenge.assertionChain.callOpts,
			challenge.id,
			history.Merkle,
			big.NewInt(int64(history.Height)),
		)
		want, err := manager.GetVertex(ctx, tx, vertexId)
		require.NoError(t, err)
		require.Equal(t, want.Unwrap(), v)
	})
	t.Run("already exists", func(t *testing.T) {
		assertionNode, err := a1.fetchAssertionNode()
		_, err = challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a1,
			util.HistoryCommitment{
				Height:        height2,
				Merkle:        common.BytesToHash([]byte("nyan")),
				LastLeaf:      assertionNode.StateHash,
				LastLeafProof: []common.Hash{assertionNode.StateHash},
			},
		)
		require.ErrorContains(t, err, "already exists")
	})
}

func setupTopLevelFork(
	t *testing.T,
	ctx context.Context,
	height1,
	height2 uint64,
) (*Assertion, *Assertion, *Challenge, *AssertionChain, *AssertionChain) {
	t.Helper()
	tx := &activeTx{readWriteTx: true}
	chain1, accs, addresses, backend, headerReader := setupAssertionChainWithChallengeManager(t)
	prev := uint64(0)

	minAssertionPeriod, err := chain1.userLogic.MinimumAssertionPeriod(chain1.callOpts)
	require.NoError(t, err)

	latestBlockHash := common.Hash{}
	for i := uint64(0); i < minAssertionPeriod.Uint64(); i++ {
		latestBlockHash = backend.Commit()
	}

	prevState := &protocol.ExecutionState{
		GlobalState:   protocol.GoGlobalState{},
		MachineStatus: protocol.MachineStatusFinished,
	}
	postState := &protocol.ExecutionState{
		GlobalState: protocol.GoGlobalState{
			BlockHash:  latestBlockHash,
			SendRoot:   common.Hash{},
			Batch:      1,
			PosInBatch: 0,
		},
		MachineStatus: protocol.MachineStatusFinished,
	}
	prevInboxMaxCount := big.NewInt(1)
	a1, err := chain1.CreateAssertion(
		ctx,
		tx,
		height1,
		protocol.AssertionSequenceNumber(prev),
		prevState,
		postState,
		prevInboxMaxCount,
	)
	require.NoError(t, err)

	chain2, err := NewAssertionChain(
		ctx,
		addresses.Rollup,
		accs[2].txOpts,
		&bind.CallOpts{},
		accs[2].accountAddr,
		backend,
		headerReader,
	)
	require.NoError(t, err)

	postState.GlobalState.BlockHash = common.BytesToHash([]byte("evil"))
	a2, err := chain2.CreateAssertion(
		ctx,
		tx,
		height2,
		protocol.AssertionSequenceNumber(prev),
		prevState,
		postState,
		prevInboxMaxCount,
	)
	require.NoError(t, err)

	manager, err := chain2.CurrentChallengeManager(ctx, tx)
	require.NoError(t, err)
	assertionId, err := chain2.rollup.GetAssertionId(chain2.callOpts, 0)
	require.NoError(t, err)
	challengeId, err := manager.CalculateChallengeHash(ctx, tx, assertionId, protocol.BlockChallenge)
	require.NoError(t, err)

	return a1.(*Assertion), a2.(*Assertion), &Challenge{assertionChain: chain2, id: challengeId}, chain1, chain2
}
