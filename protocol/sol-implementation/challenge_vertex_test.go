package solimpl

import (
	"context"
	"testing"

	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	"github.com/OffchainLabs/challenge-protocol-v2/util"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var _ = protocol.ChallengeVertex(&ChallengeVertex{})

func TestChallengeVertex_ConfirmPsTimer(t *testing.T) {
	ctx := context.Background()
	tx := &activeTx{readWriteTx: true}
	height1 := uint64(6)
	height2 := uint64(7)
	a1, a2, challenge, chain1, _ := setupTopLevelFork(t, ctx, height1, height2)

	genesisAssertion, err := chain1.AssertionBySequenceNum(ctx, tx, 0)
	require.NoError(t, err)
	genesis := genesisAssertion.(*Assertion)

	genesisNode, err := genesis.fetchAssertionNode()
	require.NoError(t, err)
	a1Node, err := a1.fetchAssertionNode()
	require.NoError(t, err)
	a2Node, err := a2.fetchAssertionNode()
	require.NoError(t, err)
	// We add two leaves to the challenge.
	v1, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a1,
		util.HistoryCommitment{
			Height:        height1,
			Merkle:        common.BytesToHash([]byte("nyan")),
			FirstLeaf:     genesisNode.StateHash,
			LastLeaf:      a1Node.StateHash,
			LastLeafProof: []common.Hash{a1Node.StateHash},
		},
	)
	require.NoError(t, err)
	_, err = challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a2,
		util.HistoryCommitment{
			Height:        height2,
			Merkle:        common.BytesToHash([]byte("nyan2")),
			FirstLeaf:     genesisNode.StateHash,
			LastLeaf:      a2Node.StateHash,
			LastLeafProof: []common.Hash{a2Node.StateHash},
		},
	)
	require.NoError(t, err)

	t.Run("vertex ps timer has not exceeded challenge duration", func(t *testing.T) {
		require.ErrorIs(t, v1.ConfirmForPsTimer(ctx, tx), ErrPsTimerNotYet)
	})
	t.Run("vertex ps timer has exceeded challenge duration", func(t *testing.T) {
		backend, ok := chain1.backend.(*backends.SimulatedBackend)
		require.Equal(t, true, ok)
		for i := 0; i < 1000; i++ {
			backend.Commit()
		}
		require.NoError(t, v1.ConfirmForPsTimer(ctx, tx))
	})
}

func TestChallengeVertex_HasConfirmedSibling(t *testing.T) {
	ctx := context.Background()
	tx := &activeTx{readWriteTx: true}
	height1 := uint64(6)
	height2 := uint64(7)
	a1, a2, challenge, chain, _ := setupTopLevelFork(t, ctx, height1, height2)

	genesisAssertion, err := chain.AssertionBySequenceNum(ctx, tx, 0)
	require.NoError(t, err)
	genesis := genesisAssertion.(*Assertion)

	genesisNode, err := genesis.fetchAssertionNode()
	require.NoError(t, err)
	a1Node, err := a1.fetchAssertionNode()
	require.NoError(t, err)
	a2Node, err := a2.fetchAssertionNode()
	require.NoError(t, err)

	// We add two leaves to the challenge.
	v1, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a1,
		util.HistoryCommitment{
			Height:        height1,
			Merkle:        common.BytesToHash([]byte("nyan")),
			FirstLeaf:     genesisNode.StateHash,
			LastLeaf:      a1Node.StateHash,
			LastLeafProof: []common.Hash{a1Node.StateHash},
		},
	)
	require.NoError(t, err)
	v2, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a2,
		util.HistoryCommitment{
			Height:        height2,
			Merkle:        common.BytesToHash([]byte("nyan2")),
			FirstLeaf:     genesisNode.StateHash,
			LastLeaf:      a2Node.StateHash,
			LastLeafProof: []common.Hash{a2Node.StateHash},
		},
	)
	require.NoError(t, err)

	backend, ok := chain.backend.(*backends.SimulatedBackend)
	require.Equal(t, true, ok)
	for i := 0; i < 1000; i++ {
		backend.Commit()
	}
	require.NoError(t, v1.ConfirmForPsTimer(ctx, tx))

	ok, err = v2.HasConfirmedSibling(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, true, ok)
}

func TestChallengeVertex_IsPresumptiveSuccessor(t *testing.T) {
	ctx := context.Background()
	tx := &activeTx{readWriteTx: true}
	height1 := uint64(6)
	height2 := uint64(7)
	a1, a2, challenge, chain, _ := setupTopLevelFork(t, ctx, height1, height2)

	genesisAssertion, err := chain.AssertionBySequenceNum(ctx, tx, 0)
	require.NoError(t, err)
	genesis := genesisAssertion.(*Assertion)

	genesisNode, err := genesis.fetchAssertionNode()
	require.NoError(t, err)
	a1Node, err := a1.fetchAssertionNode()
	require.NoError(t, err)
	a2Node, err := a2.fetchAssertionNode()
	require.NoError(t, err)

	// We add two leaves to the challenge.
	v1, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a1,
		util.HistoryCommitment{
			Height:        height1,
			Merkle:        common.BytesToHash([]byte("nyan")),
			FirstLeaf:     genesisNode.StateHash,
			LastLeaf:      a1Node.StateHash,
			LastLeafProof: []common.Hash{a1Node.StateHash},
		},
	)
	require.NoError(t, err)
	v2, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a2,
		util.HistoryCommitment{
			Height:        height2,
			Merkle:        common.BytesToHash([]byte("nyan2")),
			FirstLeaf:     genesisNode.StateHash,
			LastLeaf:      a2Node.StateHash,
			LastLeafProof: []common.Hash{a2Node.StateHash},
		},
	)
	require.NoError(t, err)

	t.Run("first to act is now presumptive", func(t *testing.T) {
		isPs, err := v1.IsPresumptiveSuccessor(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, true, isPs)

		isPs, err = v2.IsPresumptiveSuccessor(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, false, isPs)
	})
	t.Run("the newly bisected vertex is now presumptive", func(t *testing.T) {
		wantCommit := common.BytesToHash([]byte("nyan2"))
		bisectedToV, err := v2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height:    4,
				Merkle:    wantCommit,
				FirstLeaf: genesisNode.StateHash,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		bisectedTo := bisectedToV.(*ChallengeVertex)

		bisectedToVertex, err := bisectedTo.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)

		require.Equal(t, uint64(4), bisectedToVertex.Height.Uint64())

		// V1 should no longer be presumptive.
		isPs, err := v1.IsPresumptiveSuccessor(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, false, isPs)

		// Bisected to should be presumptive.
		isPs, err = bisectedTo.IsPresumptiveSuccessor(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, true, isPs)
	})
}

func TestChallengeVertex_ChildrenAreAtOneStepFork(t *testing.T) {
	ctx := context.Background()
	tx := &activeTx{readWriteTx: true}
	t.Run("children are one step away", func(t *testing.T) {
		height1 := uint64(1)
		height2 := uint64(1)
		a1, a2, challenge, chain, _ := setupTopLevelFork(t, ctx, height1, height2)

		genesisAssertion, err := chain.AssertionBySequenceNum(ctx, tx, 0)
		require.NoError(t, err)
		genesis := genesisAssertion.(*Assertion)

		genesisNode, err := genesis.fetchAssertionNode()
		require.NoError(t, err)
		a1Node, err := a1.fetchAssertionNode()
		require.NoError(t, err)
		a2Node, err := a2.fetchAssertionNode()
		require.NoError(t, err)

		// We add two leaves to the challenge.
		_, err = challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a1,
			util.HistoryCommitment{
				Height:        height1,
				Merkle:        common.BytesToHash([]byte("nyan")),
				FirstLeaf:     genesisNode.StateHash,
				LastLeaf:      a1Node.StateHash,
				LastLeafProof: []common.Hash{a1Node.StateHash},
			},
		)
		require.NoError(t, err)
		_, err = challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a2,
			util.HistoryCommitment{
				Height:        height2,
				Merkle:        common.BytesToHash([]byte("nyan2")),
				FirstLeaf:     genesisNode.StateHash,
				LastLeaf:      a2Node.StateHash,
				LastLeafProof: []common.Hash{a2Node.StateHash},
			},
		)
		require.NoError(t, err)

		innerChallenge, err := challenge.fetchChallenge(ctx, tx)
		require.NoError(t, err)
		rootV := &ChallengeVertex{id: innerChallenge.RootId, assertionChain: chain}
		atOSF, err := rootV.ChildrenAreAtOneStepFork(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, true, atOSF)
	})
	t.Run("different heights", func(t *testing.T) {
		height1 := uint64(6)
		height2 := uint64(7)
		a1, a2, challenge, chain, _ := setupTopLevelFork(t, ctx, height1, height2)

		genesisAssertion, err := chain.AssertionBySequenceNum(ctx, tx, 0)
		require.NoError(t, err)
		genesis := genesisAssertion.(*Assertion)

		genesisNode, err := genesis.fetchAssertionNode()
		require.NoError(t, err)
		a1Node, err := a1.fetchAssertionNode()
		require.NoError(t, err)
		a2Node, err := a2.fetchAssertionNode()
		require.NoError(t, err)

		// We add two leaves to the challenge.
		_, err = challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a1,
			util.HistoryCommitment{
				Height:        height1,
				Merkle:        common.BytesToHash([]byte("nyan")),
				FirstLeaf:     genesisNode.StateHash,
				LastLeaf:      a1Node.StateHash,
				LastLeafProof: []common.Hash{a1Node.StateHash},
			},
		)
		require.NoError(t, err)
		_, err = challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a2,
			util.HistoryCommitment{
				Height:        height2,
				Merkle:        common.BytesToHash([]byte("nyan2")),
				FirstLeaf:     genesisNode.StateHash,
				LastLeaf:      a2Node.StateHash,
				LastLeafProof: []common.Hash{a2Node.StateHash},
			},
		)
		require.NoError(t, err)

		innerChallenge, err := challenge.fetchChallenge(ctx, tx)
		require.NoError(t, err)
		rootV := &ChallengeVertex{id: innerChallenge.RootId, assertionChain: chain}
		atOSF, err := rootV.ChildrenAreAtOneStepFork(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, false, atOSF)
	})
	t.Run("two bisection leading to one step fork", func(t *testing.T) {
		height1 := uint64(2)
		height2 := uint64(2)
		a1, a2, challenge, chain, _ := setupTopLevelFork(t, ctx, height1, height2)

		genesisAssertion, err := chain.AssertionBySequenceNum(ctx, tx, 0)
		require.NoError(t, err)
		genesis := genesisAssertion.(*Assertion)
		a1Node, err := a1.fetchAssertionNode()
		require.NoError(t, err)
		a2Node, err := a2.fetchAssertionNode()
		require.NoError(t, err)
		genesisNode, err := genesis.fetchAssertionNode()
		require.NoError(t, err)
		challengeInner, err := challenge.fetchChallenge(ctx, tx)
		require.NoError(t, err)

		// We add two leaves to the challenge.
		v1, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a1,
			util.HistoryCommitment{
				Height:        height1,
				Merkle:        common.BytesToHash([]byte("nyan")),
				FirstLeaf:     genesisNode.StateHash,
				LastLeaf:      a1Node.StateHash,
				LastLeafProof: []common.Hash{a1Node.StateHash},
			},
		)
		require.NoError(t, err)
		v2, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a2,
			util.HistoryCommitment{
				Height:        height2,
				Merkle:        common.BytesToHash([]byte("nyan2")),
				FirstLeaf:     genesisNode.StateHash,
				LastLeaf:      a2Node.StateHash,
				LastLeafProof: []common.Hash{a2Node.StateHash},
			},
		)
		require.NoError(t, err)

		rootV := &ChallengeVertex{id: challengeInner.RootId, assertionChain: chain}
		atOSF, err := rootV.ChildrenAreAtOneStepFork(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, false, atOSF)

		// We then bisect, and then the vertices we bisected to should
		// now be at one step forks, as they will be at height 1 while their
		// parent is at height 0.
		commit := common.BytesToHash([]byte("nyan2"))
		bisectedTo2V, err := v2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height:    1,
				Merkle:    commit,
				FirstLeaf: genesisNode.StateHash,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		bisectedTo2 := bisectedTo2V.(*ChallengeVertex)
		bisectedTo2Vertex, err := bisectedTo2.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(1), bisectedTo2Vertex.Height.Uint64())

		commit = common.BytesToHash([]byte("nyan2fork"))
		bisectedTo1V, err := v1.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height:    1,
				Merkle:    commit,
				FirstLeaf: genesisNode.StateHash,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		bisectedTo1 := bisectedTo1V.(*ChallengeVertex)
		bisectedTo1Vertex, err := bisectedTo1.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(1), bisectedTo1Vertex.Height.Uint64())

		rootV = &ChallengeVertex{id: challengeInner.RootId, assertionChain: chain}
		atOSF, err = rootV.ChildrenAreAtOneStepFork(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, true, atOSF)
	})
}

func TestChallengeVertex_Bisect(t *testing.T) {
	ctx := context.Background()
	tx := &activeTx{readWriteTx: true}
	height1 := uint64(6)
	height2 := uint64(7)
	a1, a2, challenge, chain1, chain2 := setupTopLevelFork(t, ctx, height1, height2)
	a1Node, err := a1.fetchAssertionNode()
	require.NoError(t, err)
	a2Node, err := a2.fetchAssertionNode()
	require.NoError(t, err)

	// We add two leaves to the challenge.
	challenge.assertionChain = chain1
	v1, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a1,
		util.HistoryCommitment{
			Height:        height1,
			Merkle:        common.BytesToHash([]byte("nyan")),
			LastLeaf:      a1Node.StateHash,
			LastLeafProof: []common.Hash{a1Node.StateHash},
		},
	)
	require.NoError(t, err)
	challenge.assertionChain = chain2
	v2, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a2,
		util.HistoryCommitment{
			Height:        height2,
			Merkle:        common.BytesToHash([]byte("nyan2")),
			LastLeaf:      a2Node.StateHash,
			LastLeafProof: []common.Hash{a2Node.StateHash},
		},
	)
	require.NoError(t, err)

	t.Run("vertex does not exist", func(t *testing.T) {
		vertex := &ChallengeVertex{
			id:             common.BytesToHash([]byte("junk")),
			assertionChain: challenge.assertionChain,
		}
		_, err = vertex.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: common.BytesToHash([]byte("nyan4")),
			},
			make([]common.Hash, 0),
		)
		require.ErrorContains(t, err, "does not exist")
	})
	t.Run("winner already declared", func(t *testing.T) {
		t.Skip("Need to add winner capabilities in order to test")
	})
	t.Run("cannot bisect presumptive successor", func(t *testing.T) {
		// V1 should be the presumptive successor here.
		_, err = v1.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: common.BytesToHash([]byte("nyan4")),
			},
			make([]common.Hash, 0),
		)
		require.ErrorContains(t, err, "Cannot bisect presumptive")
	})
	t.Run("presumptive successor already confirmable", func(t *testing.T) {
		manager, err := chain1.CurrentChallengeManager(ctx, tx)
		require.NoError(t, err)
		chalPeriod, err := manager.ChallengePeriodSeconds(ctx, tx)
		require.NoError(t, err)
		backend, ok := chain1.backend.(*backends.SimulatedBackend)
		require.Equal(t, true, ok)
		err = backend.AdjustTime(chalPeriod)
		require.NoError(t, err)

		// We make a challenge period pass.
		_, err = v2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: common.BytesToHash([]byte("nyan4")),
			},
			make([]common.Hash, 0),
		)
		require.ErrorContains(t, err, "cannot set lower ps")
	})
	t.Run("invalid prefix history", func(t *testing.T) {
		t.Skip("Need to add proof capabilities in solidity in order to test")
	})
	t.Run("OK", func(t *testing.T) {
		height1 := uint64(6)
		height2 := uint64(7)
		a1, a2, challenge, chain1, chain2 := setupTopLevelFork(t, ctx, height1, height2)
		a1Node, err := a1.fetchAssertionNode()
		require.NoError(t, err)
		a2Node, err := a2.fetchAssertionNode()
		require.NoError(t, err)

		// We add two leaves to the challenge.
		challenge.assertionChain = chain1
		v1, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a1,
			util.HistoryCommitment{
				Height:        height1,
				Merkle:        common.BytesToHash([]byte("nyan")),
				LastLeaf:      a1Node.StateHash,
				LastLeafProof: []common.Hash{a1Node.StateHash},
			},
		)
		require.NoError(t, err)

		challenge.assertionChain = chain2
		v2, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a2,
			util.HistoryCommitment{
				Height:        height2,
				Merkle:        common.BytesToHash([]byte("nyan2")),
				LastLeaf:      a2Node.StateHash,
				LastLeafProof: []common.Hash{a2Node.StateHash},
			},
		)
		require.NoError(t, err)

		wantCommit := common.BytesToHash([]byte("nyan4"))
		bisectedToV, err := v2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: wantCommit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		bisectedTo := bisectedToV.(*ChallengeVertex)
		bisectedToVertex, err := bisectedTo.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(4), bisectedToVertex.Height.Uint64())
		require.Equal(t, wantCommit[:], bisectedToVertex.HistoryRoot[:])

		_, err = v1.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: wantCommit,
			},
			make([]common.Hash, 0),
		)
		require.ErrorContains(t, err, "already exists")
	})
}

func TestChallengeVertex_Merge(t *testing.T) {
	ctx := context.Background()
	height1 := uint64(6)
	height2 := uint64(7)
	a1, a2, challenge, chain1, chain2 := setupTopLevelFork(t, ctx, height1, height2)
	a1Node, err := a1.fetchAssertionNode()
	require.NoError(t, err)
	a2Node, err := a2.fetchAssertionNode()
	require.NoError(t, err)
	tx := &activeTx{readWriteTx: true}

	// We add two leaves to the challenge.
	challenge.assertionChain = chain1
	v1, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a1,
		util.HistoryCommitment{
			Height:        height1,
			Merkle:        common.BytesToHash([]byte("nyan")),
			LastLeaf:      a1Node.StateHash,
			LastLeafProof: []common.Hash{a1Node.StateHash},
		},
	)
	require.NoError(t, err)

	challenge.assertionChain = chain2
	v2, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a2,
		util.HistoryCommitment{
			Height:        height2,
			Merkle:        common.BytesToHash([]byte("nyan2")),
			LastLeaf:      a2Node.StateHash,
			LastLeafProof: []common.Hash{a2Node.StateHash},
		},
	)
	require.NoError(t, err)

	t.Run("vertex does not exist", func(t *testing.T) {
		vertex := &ChallengeVertex{
			id:             common.BytesToHash([]byte("junk")),
			assertionChain: challenge.assertionChain,
		}
		_, err = vertex.Merge(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: common.BytesToHash([]byte("nyan4")),
			},
			make([]common.Hash, 0),
		)
		require.ErrorContains(t, err, "does not exist")
	})
	t.Run("winner already declared", func(t *testing.T) {
		t.Skip("Need to add winner capabilities in order to test")
	})
	t.Run("cannot merge presumptive successor", func(t *testing.T) {
		// V1 should be the presumptive successor here.
		_, err = v1.Merge(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: common.BytesToHash([]byte("nyan4")),
			},
			make([]common.Hash, 0),
		)
		require.ErrorContains(t, err, "Cannot bisect presumptive")
	})
	t.Run("presumptive successor already confirmable", func(t *testing.T) {
		backend, ok := chain1.backend.(*backends.SimulatedBackend)
		require.Equal(t, true, ok)

		wantCommit := common.BytesToHash([]byte("nyan4"))
		_, err = v2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: wantCommit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)

		for i := 0; i < 1000; i++ {
			backend.Commit()
		}

		_, err = v1.Merge(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: wantCommit,
			},
			make([]common.Hash, 0),
		)
		require.ErrorContains(t, err, "cannot set lower ps")
	})
	t.Run("invalid prefix history", func(t *testing.T) {
		t.Skip("Need to add proof capabilities in solidity in order to test")
	})
	t.Run("OK", func(t *testing.T) {
		height1 := uint64(6)
		height2 := uint64(7)
		a1, a2, challenge, chain1, chain2 := setupTopLevelFork(t, ctx, height1, height2)
		a1Node, err := a1.fetchAssertionNode()
		require.NoError(t, err)
		a2Node, err := a2.fetchAssertionNode()
		require.NoError(t, err)

		// We add two leaves to the challenge.
		challenge.assertionChain = chain1
		v1, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a1,
			util.HistoryCommitment{
				Height:        height1,
				Merkle:        common.BytesToHash([]byte("nyan")),
				LastLeaf:      a1Node.StateHash,
				LastLeafProof: []common.Hash{a1Node.StateHash},
			},
		)
		require.NoError(t, err)

		challenge.assertionChain = chain2
		v2, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a2,
			util.HistoryCommitment{
				Height:        height2,
				Merkle:        common.BytesToHash([]byte("nyan2")),
				LastLeaf:      a2Node.StateHash,
				LastLeafProof: []common.Hash{a2Node.StateHash},
			},
		)
		require.NoError(t, err)

		wantCommit := common.BytesToHash([]byte("nyan4"))
		bisectedToV, err := v2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: wantCommit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		bisectedTo := bisectedToV.(*ChallengeVertex)
		bisectedToVertex, err := bisectedTo.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(4), bisectedToVertex.Height.Uint64())
		require.Equal(t, wantCommit[:], bisectedToVertex.HistoryRoot[:])

		mergedToV, err := v1.Merge(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: wantCommit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)

		mergedTo := mergedToV.(*ChallengeVertex)
		mergedToVertex, err := mergedTo.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, bisectedToVertex.HistoryRoot, mergedToVertex.HistoryRoot)
	})
}

func TestChallengeVertex_CreateSubChallenge(t *testing.T) {
	ctx := context.Background()
	height1 := uint64(6)
	height2 := uint64(7)
	tx := &activeTx{readWriteTx: true}

	t.Run("Error: vertex does not exist", func(t *testing.T) {
		_, _, challenge, _, _ := setupTopLevelFork(t, ctx, height1, height2)

		vertex := &ChallengeVertex{
			id:             common.BytesToHash([]byte("junk")),
			assertionChain: challenge.assertionChain,
		}
		_, err := vertex.CreateSubChallenge(ctx, tx)
		require.ErrorContains(t, err, "execution reverted: Challenge does not exist")
	})
	t.Run("Error: leaf can never be a fork candidate", func(t *testing.T) {
		a1, _, challenge, _, _ := setupTopLevelFork(t, ctx, height1, height2)
		a1Node, err := a1.fetchAssertionNode()
		require.NoError(t, err)

		v1, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a1,
			util.HistoryCommitment{
				Height:        height1,
				Merkle:        common.BytesToHash([]byte("nyan")),
				LastLeaf:      a1Node.StateHash,
				LastLeafProof: []common.Hash{a1Node.StateHash},
			},
		)
		require.NoError(t, err)
		_, err = v1.CreateSubChallenge(ctx, tx)
		require.ErrorContains(t, err, "execution reverted: Leaf can never be a fork candidate")
	})
	t.Run("Error: lowest height not one above the current height", func(t *testing.T) {
		a1, a2, challenge, _, _ := setupTopLevelFork(t, ctx, height1, height2)
		a1Node, err := a1.fetchAssertionNode()
		require.NoError(t, err)
		a2Node, err := a2.fetchAssertionNode()
		require.NoError(t, err)

		// We add two leaves to the challenge.
		_, err = challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a1,
			util.HistoryCommitment{
				Height:        height1,
				Merkle:        common.BytesToHash([]byte("nyan")),
				LastLeaf:      a1Node.StateHash,
				LastLeafProof: []common.Hash{a1Node.StateHash},
			},
		)
		require.NoError(t, err)
		v2, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a2,
			util.HistoryCommitment{
				Height:        height2,
				Merkle:        common.BytesToHash([]byte("nyan2")),
				LastLeaf:      a2Node.StateHash,
				LastLeafProof: []common.Hash{a2Node.StateHash},
			},
		)
		require.NoError(t, err)
		wantCommit := common.BytesToHash([]byte("nyan2"))
		bisectedToV, err := v2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: wantCommit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		bisectedTo := bisectedToV.(*ChallengeVertex)
		bisectedToVertex, err := bisectedTo.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(4), bisectedToVertex.Height.Uint64())
		require.Equal(t, wantCommit[:], bisectedToVertex.HistoryRoot[:])
		// Vertex must be in the protocol.
		manager, err := challenge.assertionChain.CurrentChallengeManager(ctx, tx)
		require.NoError(t, err)
		caller, err := manager.GetCaller(ctx, tx)
		require.NoError(t, err)
		_, err = caller.GetVertex(challenge.assertionChain.callOpts, bisectedTo.id)
		require.NoError(t, err)
		_, err = bisectedTo.CreateSubChallenge(ctx, tx)
		require.ErrorContains(t, err, "execution reverted: Lowest height not one above the current height")
	})
	t.Run("Error: has presumptive successor", func(t *testing.T) {
		a1, a2, challenge, _, _ := setupTopLevelFork(t, ctx, height1, height2)
		a1Node, err := a1.fetchAssertionNode()
		require.NoError(t, err)
		a2Node, err := a2.fetchAssertionNode()
		require.NoError(t, err)

		// We add two leaves to the challenge.
		v1, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a1,
			util.HistoryCommitment{
				Height:        height1,
				Merkle:        common.BytesToHash([]byte("nyan")),
				LastLeaf:      a1Node.StateHash,
				LastLeafProof: []common.Hash{a1Node.StateHash},
			},
		)
		require.NoError(t, err)

		v2, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a2,
			util.HistoryCommitment{
				Height:        height2,
				Merkle:        common.BytesToHash([]byte("nyan2")),
				LastLeaf:      a2Node.StateHash,
				LastLeafProof: []common.Hash{a2Node.StateHash},
			},
		)
		require.NoError(t, err)

		v1Commit := common.BytesToHash([]byte("nyan"))
		v2Commit := common.BytesToHash([]byte("nyan2"))
		v2Height4V, err := v2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: v2Commit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)

		v2Height4 := v2Height4V.(*ChallengeVertex)
		v2Height4Inner, err := v2Height4.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(4), v2Height4Inner.Height.Uint64())
		require.Equal(t, v2Commit[:], v2Height4Inner.HistoryRoot[:])

		v1Height4V, err := v1.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: v1Commit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		v1Height4 := v1Height4V.(*ChallengeVertex)
		v1Height4Inner, err := v1Height4.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(4), v1Height4Inner.Height.Uint64())
		require.Equal(t, v1Commit[:], v1Height4Inner.HistoryRoot[:])

		v2Height2V, err := v2Height4.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 2,
				Merkle: v2Commit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		v2Height2 := v2Height2V.(*ChallengeVertex)
		v2Height2Inner, err := v2Height2.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(2), v2Height2Inner.Height.Uint64())
		require.Equal(t, v2Commit[:], v2Height2Inner.HistoryRoot[:])

		v1Height2V, err := v1Height4.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 2,
				Merkle: v1Commit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		v1Height2 := v1Height2V.(*ChallengeVertex)
		v1Height2Inner, err := v1Height2.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(2), v1Height2Inner.Height.Uint64())
		require.Equal(t, v1Commit[:], v1Height2Inner.HistoryRoot[:])

		v1Height1V, err := v1Height2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 1,
				Merkle: v1Commit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		v1Height1 := v1Height1V.(*ChallengeVertex)
		v1Height1Inner, err := v1Height1.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(1), v1Height1Inner.Height.Uint64())
		require.Equal(t, v1Commit[:], v1Height1Inner.HistoryRoot[:])

		_, err = v1Height1.CreateSubChallenge(ctx, tx)
		require.ErrorContains(t, err, "execution reverted: Has presumptive successor")
	})
	t.Run("Can create succession challenge", func(t *testing.T) {
		a1, a2, challenge, _, _ := setupTopLevelFork(t, ctx, height1, height2)
		a1Node, err := a1.fetchAssertionNode()
		require.NoError(t, err)
		a2Node, err := a2.fetchAssertionNode()
		require.NoError(t, err)

		// We add two leaves to the challenge.
		v1, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a1,
			util.HistoryCommitment{
				Height:        height1,
				Merkle:        common.BytesToHash([]byte("nyan")),
				LastLeaf:      a1Node.StateHash,
				LastLeafProof: []common.Hash{a1Node.StateHash},
			},
		)
		require.NoError(t, err)

		v2, err := challenge.AddBlockChallengeLeaf(
			ctx,
			tx,
			a2,
			util.HistoryCommitment{
				Height:        height2,
				Merkle:        common.BytesToHash([]byte("nyan2")),
				LastLeaf:      a2Node.StateHash,
				LastLeafProof: []common.Hash{a2Node.StateHash},
			},
		)
		require.NoError(t, err)

		v1Commit := common.BytesToHash([]byte("nyan"))
		v2Commit := common.BytesToHash([]byte("nyan2"))
		v2Height4V, err := v2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: v2Commit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		v2Height4 := v2Height4V.(*ChallengeVertex)
		v2Height4Inner, err := v2Height4.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(4), v2Height4Inner.Height.Uint64())
		require.Equal(t, v2Commit[:], v2Height4Inner.HistoryRoot[:])

		v1Height4V, err := v1.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 4,
				Merkle: v1Commit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		v1Height4 := v1Height4V.(*ChallengeVertex)
		v1Height4Inner, err := v1Height4.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(4), v1Height4Inner.Height.Uint64())
		require.Equal(t, v1Commit[:], v1Height4Inner.HistoryRoot[:])

		v2Height2V, err := v2Height4.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 2,
				Merkle: v2Commit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		v2Height2 := v2Height2V.(*ChallengeVertex)
		v2Height2Inner, err := v2Height2.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(2), v2Height2Inner.Height.Uint64())
		require.Equal(t, v2Commit[:], v2Height2Inner.HistoryRoot[:])

		v1Height2V, err := v1Height4.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 2,
				Merkle: v1Commit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		v1Height2 := v1Height2V.(*ChallengeVertex)
		v1Height2Inner, err := v1Height2.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(2), v1Height2Inner.Height.Uint64())
		require.Equal(t, v1Commit[:], v1Height2Inner.HistoryRoot[:])

		v1Height1V, err := v1Height2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 1,
				Merkle: v1Commit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		v1Height1 := v1Height1V.(*ChallengeVertex)
		v1Height1Inner, err := v1Height1.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(1), v1Height1Inner.Height.Uint64())
		require.Equal(t, v1Commit[:], v1Height1Inner.HistoryRoot[:])

		v2Height1V, err := v2Height2.Bisect(
			ctx,
			tx,
			util.HistoryCommitment{
				Height: 1,
				Merkle: v2Commit,
			},
			make([]common.Hash, 0),
		)
		require.NoError(t, err)
		v2Height1 := v2Height1V.(*ChallengeVertex)
		v2Height1Inner, err := v2Height1.fetchChallengeVertex(ctx, tx)
		require.NoError(t, err)
		require.Equal(t, uint64(1), v2Height1Inner.Height.Uint64())
		require.Equal(t, v2Commit[:], v2Height1Inner.HistoryRoot[:])

		genesis := &ChallengeVertex{
			id:             v2Height1Inner.PredecessorId,
			assertionChain: challenge.assertionChain,
		}
		_, err = genesis.CreateSubChallenge(context.Background(), tx)
		require.NoError(t, err)
	})
}

func TestChallengeVertex_AddSubChallengeLeaf(t *testing.T) {
	ctx := context.Background()
	tx := &activeTx{readWriteTx: true}
	height1 := uint64(6)
	height2 := uint64(7)
	a1, a2, challenge, _, _ := setupTopLevelFork(t, ctx, height1, height2)
	a1Node, err := a1.fetchAssertionNode()
	require.NoError(t, err)
	a2Node, err := a2.fetchAssertionNode()
	require.NoError(t, err)

	v1, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a1,
		util.HistoryCommitment{
			Height:        height1,
			Merkle:        common.BytesToHash([]byte("nyan")),
			LastLeaf:      a1Node.StateHash,
			LastLeafProof: []common.Hash{a1Node.StateHash},
		},
	)
	require.NoError(t, err)

	v2, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a2,
		util.HistoryCommitment{
			Height:        height2,
			Merkle:        common.BytesToHash([]byte("nyan2")),
			LastLeaf:      a2Node.StateHash,
			LastLeafProof: []common.Hash{a2Node.StateHash},
		},
	)
	require.NoError(t, err)

	v1Commit := common.BytesToHash([]byte("nyan"))
	v2Commit := common.BytesToHash([]byte("nyan2"))
	v2Height4V, err := v2.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 4,
			Merkle: v2Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v2Height4 := v2Height4V.(*ChallengeVertex)
	v2Height4Inner, err := v2Height4.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), v2Height4Inner.Height.Uint64())
	require.Equal(t, v2Commit[:], v2Height4Inner.HistoryRoot[:])

	v1Height4V, err := v1.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 4,
			Merkle: v1Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v1Height4 := v1Height4V.(*ChallengeVertex)
	v1Height4Inner, err := v1Height4.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), v1Height4Inner.Height.Uint64())
	require.Equal(t, v1Commit[:], v1Height4Inner.HistoryRoot[:])

	v2Height2V, err := v2Height4.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 2,
			Merkle: v2Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v2Height2 := v2Height2V.(*ChallengeVertex)
	v2Height2Inner, err := v2Height2.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), v2Height2Inner.Height.Uint64())
	require.Equal(t, v2Commit[:], v2Height2Inner.HistoryRoot[:])

	v1Height2V, err := v1Height4.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 2,
			Merkle: v1Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v1Height2 := v1Height2V.(*ChallengeVertex)
	v1Height2Inner, err := v1Height2.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), v1Height2Inner.Height.Uint64())
	require.Equal(t, v1Commit[:], v1Height2Inner.HistoryRoot[:])

	v1Height1V, err := v1Height2.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 1,
			Merkle: v1Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v1Height1 := v1Height1V.(*ChallengeVertex)
	v1Height1Inner, err := v1Height1.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), v1Height1Inner.Height.Uint64())
	require.Equal(t, v1Commit[:], v1Height1Inner.HistoryRoot[:])

	v2Height1V, err := v2Height2.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 1,
			Merkle: v2Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v2Height1 := v2Height1V.(*ChallengeVertex)
	v2Height1Inner, err := v2Height1.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), v2Height1Inner.Height.Uint64())
	require.Equal(t, v2Commit[:], v2Height1Inner.HistoryRoot[:])

	genesis := &ChallengeVertex{
		id:             v2Height1Inner.PredecessorId,
		assertionChain: challenge.assertionChain,
	}
	bigStepChal, err := genesis.CreateSubChallenge(context.Background(), tx)
	require.NoError(t, err)

	t.Run("empty history root", func(t *testing.T) {
		_, err = bigStepChal.AddSubChallengeLeaf(ctx, tx, v1Height1, util.HistoryCommitment{})
		require.ErrorContains(t, err, "execution reverted: Empty historyRoot")
	})
	t.Run("vertex does not exist", func(t *testing.T) {
		_, err = bigStepChal.AddSubChallengeLeaf(ctx, tx, &ChallengeVertex{
			id:             [32]byte{},
			assertionChain: challenge.assertionChain,
		}, util.HistoryCommitment{
			Height: 2,
			Merkle: v1Commit,
		})
		require.ErrorContains(t, err, "execution reverted: Vertex does not exist")
	})
	t.Run("claim has invalid succession challenge", func(t *testing.T) {
		_, err = bigStepChal.AddSubChallengeLeaf(ctx, tx, v1Height2, util.HistoryCommitment{
			Height: 2,
			Merkle: v1Commit,
		})
		require.ErrorContains(t, err, "execution reverted: Claim has invalid succession challenge")
	})
	t.Run("create sub challenge leaf rival 1", func(t *testing.T) {
		v, err := bigStepChal.AddSubChallengeLeaf(ctx, tx, v1Height1, util.HistoryCommitment{
			Height: 1,
			Merkle: v1Commit,
		})
		require.NoError(t, err)
		require.False(t, v.Id() == [32]byte{}) // Should have a non-empty ID
	})
	t.Run("create sub challenge leaf rival 2", func(t *testing.T) {
		v, err := bigStepChal.AddSubChallengeLeaf(ctx, tx, v2Height1, util.HistoryCommitment{
			Height: 1,
			Merkle: v2Commit,
		})
		require.NoError(t, err)
		require.False(t, v.Id() == [32]byte{}) // Should have a non-empty ID
	})
}

func TestChallengeVertex_CanConfirmSubChallenge(t *testing.T) {
	ctx := context.Background()
	tx := &activeTx{readWriteTx: true}
	height1 := uint64(6)
	height2 := uint64(7)
	a1, a2, challenge, chain, _ := setupTopLevelFork(t, ctx, height1, height2)

	a1Node, err := a1.fetchAssertionNode()
	require.NoError(t, err)
	a2Node, err := a2.fetchAssertionNode()
	require.NoError(t, err)

	v1, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a1,
		util.HistoryCommitment{
			Height:        height1,
			Merkle:        common.BytesToHash([]byte("nyan")),
			LastLeaf:      a1Node.StateHash,
			LastLeafProof: []common.Hash{a1Node.StateHash},
		},
	)
	require.NoError(t, err)

	v2, err := challenge.AddBlockChallengeLeaf(
		ctx,
		tx,
		a2,
		util.HistoryCommitment{
			Height:        height2,
			Merkle:        common.BytesToHash([]byte("nyan2")),
			LastLeaf:      a2Node.StateHash,
			LastLeafProof: []common.Hash{a2Node.StateHash},
		},
	)
	require.NoError(t, err)

	v1Commit := common.BytesToHash([]byte("nyan"))
	v2Commit := common.BytesToHash([]byte("nyan2"))
	v2Height4V, err := v2.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 4,
			Merkle: v2Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v2Height4 := v2Height4V.(*ChallengeVertex)
	v2Height4Inner, err := v2Height4.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), v2Height4Inner.Height.Uint64())
	require.Equal(t, v2Commit[:], v2Height4Inner.HistoryRoot[:])

	v1Height4V, err := v1.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 4,
			Merkle: v1Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v1Height4 := v1Height4V.(*ChallengeVertex)
	v1Height4Inner, err := v1Height4.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), v1Height4Inner.Height.Uint64())
	require.Equal(t, v1Commit[:], v1Height4Inner.HistoryRoot[:])

	v2Height2V, err := v2Height4.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 2,
			Merkle: v2Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v2Height2 := v2Height2V.(*ChallengeVertex)
	v2Height2Inner, err := v2Height2.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), v2Height2Inner.Height.Uint64())
	require.Equal(t, v2Commit[:], v2Height2Inner.HistoryRoot[:])

	v1Height2V, err := v1Height4.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 2,
			Merkle: v1Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v1Height2 := v1Height2V.(*ChallengeVertex)
	v1Height2Inner, err := v1Height2.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), v1Height2Inner.Height.Uint64())
	require.Equal(t, v1Commit[:], v1Height2Inner.HistoryRoot[:])

	v1Height1V, err := v1Height2.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 1,
			Merkle: v1Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v1Height1 := v1Height1V.(*ChallengeVertex)
	v1Height1Inner, err := v1Height1.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), v1Height1Inner.Height.Uint64())
	require.Equal(t, v1Commit[:], v1Height1Inner.HistoryRoot[:])

	v2Height1V, err := v2Height2.Bisect(
		ctx,
		tx,
		util.HistoryCommitment{
			Height: 1,
			Merkle: v2Commit,
		},
		make([]common.Hash, 0),
	)
	require.NoError(t, err)
	v2Height1 := v2Height1V.(*ChallengeVertex)
	v2Height1Inner, err := v2Height1.fetchChallengeVertex(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), v2Height1Inner.Height.Uint64())
	require.Equal(t, v2Commit[:], v2Height1Inner.HistoryRoot[:])

	require.NoError(t, err)
	genesis := &ChallengeVertex{
		assertionChain: challenge.assertionChain,
		id:             v2Height1Inner.PredecessorId,
	}
	bigStepChal, err := genesis.CreateSubChallenge(context.Background(), tx)
	require.NoError(t, err)

	v, err := bigStepChal.AddSubChallengeLeaf(ctx, tx, v1Height1, util.HistoryCommitment{
		Height: 1,
		Merkle: v1Commit,
	})
	require.NoError(t, err)

	t.Run("can't confirm sub challenge", func(t *testing.T) {
		require.ErrorContains(t, v.ConfirmForPsTimer(ctx, tx), "ps timer has not exceeded challenge period")
	})
	t.Run("can confirm sub challenge", func(t *testing.T) {
		backend, ok := chain.backend.(*backends.SimulatedBackend)
		require.Equal(t, true, ok)
		for i := 0; i < 1000; i++ {
			backend.Commit()
		}
		require.NoError(t, v.ConfirmForPsTimer(ctx, tx))
	})
}
