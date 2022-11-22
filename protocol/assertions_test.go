package protocol

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/OffchainLabs/new-rollup-exploration/util"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var _ = OnChainProtocol(&AssertionChain{})

const testChallengePeriod = 100 * time.Second

func TestAssertionChain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	timeRef := util.NewArtificialTimeReference()
	correctBlockHashes := correctBlockHashesForTest(200)
	wrongBlockHashes := wrongBlockHashesForTest(200)
	staker1 := common.BytesToAddress([]byte{1})
	staker2 := common.BytesToAddress([]byte{2})

	chain := NewAssertionChain(ctx, timeRef, testChallengePeriod)
	require.Equal(t, 1, len(chain.assertions))
	require.Equal(t, uint64(0), chain.confirmedLatest)
	genesis := chain.LatestConfirmed()
	require.Equal(t, StateCommitment{
		Height:    0,
		StateRoot: common.Hash{},
	}, genesis.StateCommitment)

	bigBalance := new(big.Int).Mul(AssertionStakeWei, big.NewInt(1000))
	chain.SetBalance(staker1, bigBalance)
	chain.SetBalance(staker2, bigBalance)

	eventChan := make(chan AssertionChainEvent)
	chain.feed.SubscribeWithFilter(ctx, eventChan, func(ev AssertionChainEvent) bool {
		switch ev.(type) {
		case *SetBalanceEvent:
			return false
		default:
			return true
		}
	})

	// add an assertion, then confirm it
	comm := StateCommitment{Height: 1, StateRoot: correctBlockHashes[99]}
	newAssertion, err := chain.CreateLeaf(genesis, comm, staker1)
	require.NoError(t, err)
	require.Equal(t, 2, len(chain.assertions))
	require.Equal(t, genesis, chain.LatestConfirmed())
	verifyCreateLeafEventInFeed(t, eventChan, 1, 0, staker1, comm)
	require.True(t, new(big.Int).Add(chain.GetBalance(staker1), AssertionStakeWei).Cmp(bigBalance) == 0)

	err = newAssertion.ConfirmNoRival()
	require.ErrorIs(t, err, ErrNotYet)
	timeRef.Add(testChallengePeriod + time.Second)
	require.NoError(t, newAssertion.ConfirmNoRival())
	require.True(t, chain.GetBalance(staker1).Cmp(bigBalance) == 0)

	require.Equal(t, newAssertion, chain.LatestConfirmed())
	require.Equal(t, ConfirmedAssertionState, int(newAssertion.status))
	verifyConfirmEventInFeed(t, eventChan, 1)

	// try to create a duplicate assertion
	_, err = chain.CreateLeaf(genesis, StateCommitment{1, correctBlockHashes[99]}, staker1)
	require.ErrorIs(t, err, ErrVertexAlreadyExists)

	// create a fork, let first branch win by timeout
	comm = StateCommitment{2, correctBlockHashes[199]}
	branch1, err := chain.CreateLeaf(newAssertion, comm, staker1)
	require.NoError(t, err)
	timeRef.Add(5 * time.Second)
	verifyCreateLeafEventInFeed(t, eventChan, 2, 1, staker1, comm)
	comm = StateCommitment{2, wrongBlockHashes[199]}
	branch2, err := chain.CreateLeaf(newAssertion, comm, staker2)
	require.NoError(t, err)
	verifyCreateLeafEventInFeed(t, eventChan, 3, 1, staker2, comm)
	challenge, err := newAssertion.CreateChallenge(ctx)
	require.NoError(t, err)
	verifyStartChallengeEventInFeed(t, eventChan, newAssertion.SequenceNum)
	chal1, err := challenge.AddLeaf(branch1, util.HistoryCommitment{Height: 100, Merkle: util.ExpansionFromLeaves(correctBlockHashes[99:200]).Root()})
	require.NoError(t, err)
	_, err = challenge.AddLeaf(branch2, util.HistoryCommitment{Height: 100, Merkle: util.ExpansionFromLeaves(wrongBlockHashes[99:200]).Root()})
	require.NoError(t, err)
	err = chal1.ConfirmForPsTimer()
	require.ErrorIs(t, err, ErrNotYet)

	timeRef.Add(testChallengePeriod)
	require.NoError(t, chal1.ConfirmForPsTimer())
	require.NoError(t, branch1.ConfirmForWin())
	require.Equal(t, branch1, chain.LatestConfirmed())

	verifyConfirmEventInFeed(t, eventChan, 2)
	require.NoError(t, branch2.RejectForLoss())
	verifyRejectEventInFeed(t, eventChan, 3)

	// verify that feed is empty
	time.Sleep(500 * time.Millisecond)
	select {
	case ev := <-eventChan:
		t.Fatal(ev)
	default:
	}
}

func TestAssertionChain_LeafCreationThroughDiffStakers(t *testing.T) {
	ctx := context.Background()
	chain := NewAssertionChain(ctx, util.NewArtificialTimeReference(), testChallengePeriod)

	oldStaker := common.BytesToAddress([]byte{1})
	staker := common.BytesToAddress([]byte{2})
	require.Equal(t, chain.GetBalance(oldStaker), big.NewInt(0)) // Old staker has 0 because it's already staked.
	chain.SetBalance(staker, AssertionStakeWei)
	require.Equal(t, chain.GetBalance(staker), AssertionStakeWei) // New staker has full balance because it's not yet staked.

	lc := chain.LatestConfirmed()
	lc.staker = util.FullOption[common.Address](oldStaker)
	_, err := chain.CreateLeaf(lc, StateCommitment{Height: 1, StateRoot: common.Hash{}}, staker)
	require.NoError(t, err)

	require.Equal(t, chain.GetBalance(staker), big.NewInt(0))        // New staker has 0 balance after staking.
	require.Equal(t, chain.GetBalance(oldStaker), AssertionStakeWei) // Old staker has full balance after unstaking.
}

func TestAssertionChain_LeafCreationsInsufficientStakes(t *testing.T) {
	ctx := context.Background()
	chain := NewAssertionChain(ctx, util.NewArtificialTimeReference(), testChallengePeriod)
	lc := chain.LatestConfirmed()

	staker := common.BytesToAddress([]byte{1})
	lc.staker = util.EmptyOption[common.Address]()
	_, err := chain.CreateLeaf(lc, StateCommitment{Height: 1, StateRoot: common.Hash{}}, staker)
	require.ErrorIs(t, err, ErrInsufficientBalance)

	diffStaker := common.BytesToAddress([]byte{2})
	lc.staker = util.FullOption[common.Address](diffStaker)
	_, err = chain.CreateLeaf(lc, StateCommitment{Height: 1, StateRoot: common.Hash{}}, staker)
	require.ErrorIs(t, err, ErrInsufficientBalance)
}

func verifyCreateLeafEventInFeed(t *testing.T, c <-chan AssertionChainEvent, seqNum, prevSeqNum uint64, staker common.Address, comm StateCommitment) {
	t.Helper()
	ev := <-c
	switch e := ev.(type) {
	case *CreateLeafEvent:
		if e.SeqNum != seqNum || e.PrevSeqNum != prevSeqNum || e.Staker != staker || e.StateCommitment != comm {
			t.Fatal(e)
		}
	default:
		t.Fatal(e)
	}
}

func verifyConfirmEventInFeed(t *testing.T, c <-chan AssertionChainEvent, seqNum uint64) {
	t.Helper()
	ev := <-c
	switch e := ev.(type) {
	case *ConfirmEvent:
		require.Equal(t, seqNum, e.SeqNum)
	default:
		t.Fatal()
	}
}

func verifyRejectEventInFeed(t *testing.T, c <-chan AssertionChainEvent, seqNum uint64) {
	t.Helper()
	ev := <-c
	switch e := ev.(type) {
	case *RejectEvent:
		require.Equal(t, seqNum, e.SeqNum)
	default:
		t.Fatal()
	}
}

func verifyStartChallengeEventInFeed(t *testing.T, c <-chan AssertionChainEvent, parentSeqNum uint64) {
	t.Helper()
	ev := <-c
	switch e := ev.(type) {
	case *StartChallengeEvent:
		require.Equal(t, parentSeqNum, e.ParentSeqNum)
	default:
		t.Fatal()
	}
}

func TestBisectionChallengeGame(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	timeRef := util.NewArtificialTimeReference()
	correctBlockHashes := correctBlockHashesForTest(10)
	wrongBlockHashes := wrongBlockHashesForTest(10)
	staker1 := common.BytesToAddress([]byte{1})
	staker2 := common.BytesToAddress([]byte{2})

	chain := NewAssertionChain(ctx, timeRef, testChallengePeriod)

	bigBalance := new(big.Int).Mul(AssertionStakeWei, big.NewInt(1000))
	chain.SetBalance(staker1, bigBalance)
	chain.SetBalance(staker2, bigBalance)

	// We create a fork with genesis as the parent, where one branch is a higher depth than the other.
	lowerHeight := uint64(6)
	higherHeight := uint64(7)
	genesis := chain.LatestConfirmed()
	wrongLeaf, err := chain.CreateLeaf(
		genesis, StateCommitment{
			Height:    lowerHeight,
			StateRoot: wrongBlockHashes[lowerHeight],
		}, staker1,
	)
	require.NoError(t, err)
	correctLeaf, err := chain.CreateLeaf(
		genesis,
		StateCommitment{
			Height:    higherHeight,
			StateRoot: correctBlockHashes[higherHeight],
		},
		staker2,
	)
	require.NoError(t, err)

	challenge, err := genesis.CreateChallenge(ctx)
	require.NoError(t, err)

	// Add the relevant leaves to the challenge along with
	// their historical state commitments.
	expectedBisectionHeight := uint64(4)

	lowerLeaf, err := challenge.AddLeaf(
		wrongLeaf,
		util.HistoryCommitment{
			Height: lowerHeight,
			Merkle: util.ExpansionFromLeaves(wrongBlockHashes[:lowerHeight]).Root(),
		},
	)
	require.NoError(t, err)
	higherLeaf, err := challenge.AddLeaf(
		correctLeaf,
		util.HistoryCommitment{
			Height: higherHeight,
			Merkle: util.ExpansionFromLeaves(correctBlockHashes[:higherHeight]).Root(),
		},
	)
	require.NoError(t, err)

	// Ensure the lower height challenge vertex is the ps.
	require.Equal(t, true, lowerLeaf.isPresumptiveSuccessor())
	require.Equal(t, false, higherLeaf.isPresumptiveSuccessor())

	// Next, only the vertex that is not the presumptive successor can start a bisection move.
	bisectionHeight, err := higherLeaf.requiredBisectionHeight()
	require.NoError(t, err)
	require.Equal(t, expectedBisectionHeight, bisectionHeight)

	// Expect a lower leaf to be disallowed from bisecting, despite correct proof.
	bisectionExpansion := util.ExpansionFromLeaves(wrongBlockHashes[:bisectionHeight])
	proof := util.GeneratePrefixProof(
		expectedBisectionHeight,
		util.ExpansionFromLeaves(wrongBlockHashes[:bisectionHeight]),
		wrongBlockHashes[bisectionHeight:lowerHeight],
	)
	_, err = lowerLeaf.Bisect(
		util.HistoryCommitment{
			Height: bisectionHeight,
			Merkle: bisectionExpansion.Root(),
		},
		proof,
	)
	require.ErrorIs(t, err, ErrWrongState)

	// Generate a prefix proof for the associated history commitments from the bisection
	// height up to the height of the state commitment for the non-presumptive challenge leaf.
	bisectionExpansion = util.ExpansionFromLeaves(correctBlockHashes[:bisectionHeight])
	proof = util.GeneratePrefixProof(
		bisectionHeight,
		bisectionExpansion,
		correctBlockHashes[bisectionHeight:higherHeight],
	)
	bisection, err := higherLeaf.Bisect(
		util.HistoryCommitment{
			Height: bisectionHeight,
			Merkle: bisectionExpansion.Root(),
		},
		proof,
	)
	require.NoError(t, err)

	// Expect the ps of the root to change after we bisect. It should be the new challenge
	// vertex created by bisecting the highest leaf.
	require.Equal(t, true, bisection.isPresumptiveSuccessor())
}

func correctBlockHashesForTest(numBlocks uint64) []common.Hash {
	ret := []common.Hash{}
	for i := uint64(0); i < numBlocks; i++ {
		ret = append(ret, util.HashForUint(i))
	}
	return ret
}

func wrongBlockHashesForTest(numBlocks uint64) []common.Hash {
	ret := []common.Hash{}
	for i := uint64(0); i < numBlocks; i++ {
		ret = append(ret, util.HashForUint(71285937102384-i))
	}
	return ret
}

func TestAssertionChain_StakerInsufficientBalance(t *testing.T) {
	ctx := context.Background()
	chain := NewAssertionChain(ctx, util.NewArtificialTimeReference(), testChallengePeriod)
	require.Equal(t, chain.DeductFromBalance(common.BytesToAddress([]byte{1}), AssertionStakeWei), ErrInsufficientBalance)
}

func TestAssertionChain_ChallengePeriodLength(t *testing.T) {
	ctx := context.Background()
	cp := 123 * time.Second
	chain := NewAssertionChain(ctx, util.NewArtificialTimeReference(), cp)
	require.Equal(t, chain.ChallengePeriodLength(), cp)
}

func TestAssertionChain_LeafCreationErrors(t *testing.T) {
	ctx := context.Background()
	chain := NewAssertionChain(ctx, util.NewArtificialTimeReference(), testChallengePeriod)
	badChain := NewAssertionChain(ctx, util.NewArtificialTimeReference(), testChallengePeriod+1)
	lc := chain.LatestConfirmed()
	_, err := badChain.CreateLeaf(lc, StateCommitment{}, common.BytesToAddress([]byte{}))
	require.ErrorIs(t, err, ErrWrongChain)
	_, err = chain.CreateLeaf(lc, StateCommitment{}, common.BytesToAddress([]byte{}))
	require.ErrorIs(t, err, ErrInvalid)
}

func TestAssertion_ErrWrongState(t *testing.T) {
	ctx := context.Background()
	chain := NewAssertionChain(ctx, util.NewArtificialTimeReference(), testChallengePeriod)
	a := chain.LatestConfirmed()
	require.ErrorIs(t, a.RejectForPrev(), ErrWrongState)
	require.ErrorIs(t, a.RejectForLoss(), ErrWrongState)
	require.ErrorIs(t, a.ConfirmForWin(), ErrWrongState)
}

func TestAssertion_ErrWrongPredecessorState(t *testing.T) {
	ctx := context.Background()
	chain := NewAssertionChain(ctx, util.NewArtificialTimeReference(), testChallengePeriod)
	staker := common.BytesToAddress([]byte{1})
	bigBalance := new(big.Int).Mul(AssertionStakeWei, big.NewInt(1000))
	chain.SetBalance(staker, bigBalance)
	newA, err := chain.CreateLeaf(chain.LatestConfirmed(), StateCommitment{Height: 1}, staker)
	require.NoError(t, err)
	require.ErrorIs(t, newA.RejectForPrev(), ErrWrongPredecessorState)
	require.ErrorIs(t, newA.ConfirmForWin(), ErrWrongPredecessorState)
}

func TestAssertion_ErrNotYet(t *testing.T) {
	ctx := context.Background()
	chain := NewAssertionChain(ctx, util.NewArtificialTimeReference(), testChallengePeriod)
	staker := common.BytesToAddress([]byte{1})
	bigBalance := new(big.Int).Mul(AssertionStakeWei, big.NewInt(1000))
	chain.SetBalance(staker, bigBalance)
	newA, err := chain.CreateLeaf(chain.LatestConfirmed(), StateCommitment{Height: 1}, staker)
	require.NoError(t, err)
	require.ErrorIs(t, newA.ConfirmNoRival(), ErrNotYet)
}
