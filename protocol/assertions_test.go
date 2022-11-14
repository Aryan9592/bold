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

	chain := NewAssertionChain(ctx, timeRef, testChallengePeriod, make(map[common.Address]string))
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

	chain := NewAssertionChain(ctx, timeRef, testChallengePeriod, make(map[common.Address]string))

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
	_ = bisection

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
