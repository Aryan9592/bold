package protocol

import (
	"context"
	"math/big"
	"time"

	"github.com/OffchainLabs/challenge-protocol-v2/util"
	"github.com/ethereum/go-ethereum/common"
)

// AssertionSequenceNumber is a monotonically increasing ID
// for each assertion in the chain.
type AssertionSequenceNumber uint64

// VertexSequenceNumber is a monotonically increasing ID
// for each vertex in the chain.
type VertexSequenceNumber uint64

// AssertionHash represents a unique identifier for an assertion
// constructed as a keccak256 hash of some of its internals.
type AssertionHash common.Hash

// ChallengeHash represents a unique identifier for a challenge
// constructed as a keccak256 hash of some of its internals.
type ChallengeHash common.Hash

// VertexHash represents a unique identifier for a challenge vertex
// constructed as a keccak256 hash of some of its internals.
type VertexHash common.Hash

// Protocol --
type Protocol interface {
	AssertionChain
}

// AssertionChain can manage assertions in the protocol and retrieve
// information about them. It also has an associated challenge manager
// which is used for all challenges in the protocol.
type AssertionChain interface {
	// Read-only methods.
	NumAssertions(ctx context.Context) (uint64, error)
	AssertionBySequenceNum(ctx context.Context, seqNum AssertionSequenceNumber) (Assertion, error)
	LatestConfirmed(ctx context.Context) (Assertion, error)
	GetAssertionId(ctx context.Context, seqNum AssertionSequenceNumber) (AssertionHash, error)
	GetAssertionNum(ctx context.Context, assertionHash AssertionHash) (AssertionSequenceNumber, error)
	BlockChallenge(ctx context.Context, assertionSeqNum AssertionSequenceNumber) (Challenge, error)

	// Mutating methods.
	CreateAssertion(
		ctx context.Context,
		height uint64,
		prevSeqNum AssertionSequenceNumber,
		prevAssertionState *ExecutionState,
		postState *ExecutionState,
		prevInboxMaxCount *big.Int,
	) (Assertion, error)
	// TODO: Remove.
	CreateSuccessionChallenge(ctx context.Context, seqNum AssertionSequenceNumber) (Challenge, error)
	Confirm(ctx context.Context, blockHash, sendRoot common.Hash) error

	// Spec-based implementation methods.
	SpecChallengeManager(ctx context.Context) (SpecChallengeManager, error)
}

// ChallengeManager allows for retrieving details of challenges such
// as challenges themselves, vertices, or constants such as the challenge period seconds.
type ChallengeManager interface {
	Address() common.Address
	ChallengePeriodSeconds(ctx context.Context) (time.Duration, error)
	CalculateChallengeHash(ctx context.Context, itemId common.Hash, challengeType ChallengeType) (ChallengeHash, error)
	CalculateChallengeVertexId(ctx context.Context, challengeId ChallengeHash, history util.HistoryCommitment) (VertexHash, error)
	GetVertex(ctx context.Context, vertexId VertexHash) (util.Option[ChallengeVertex], error)
	GetChallenge(ctx context.Context, challengeId ChallengeHash) (util.Option[Challenge], error)
}

// Assertion represents a top-level claim in the protocol about the
// chain state created by a validator that stakes on their claim.
// Assertions can be challenged.
type Assertion interface {
	Height() (uint64, error)
	SeqNum() AssertionSequenceNumber
	PrevSeqNum() (AssertionSequenceNumber, error)
	StateHash() (common.Hash, error)
}

// ChallengeType represents the enum with the same name
// in the protocol smart contracts.
type ChallengeType uint8

const (
	BlockChallenge ChallengeType = iota
	BigStepChallenge
	SmallStepChallenge
)

func (ct ChallengeType) String() string {
	switch ct {
	case BlockChallenge:
		return "block"
	case BigStepChallenge:
		return "big_step"
	case SmallStepChallenge:
		return "small_step"
	default:
		return "unknown"
	}
}

// IsSubChallenge returns true if the challenge type is either big or small step.
func (ct ChallengeType) IsSubChallenge() bool {
	return ct == BigStepChallenge || ct == SmallStepChallenge
}

// AssertionState represents the enum with the same name
// in the protocol smart contracts.
type AssertionState uint8

const (
	AssertionPending AssertionState = iota
	AssertionConfirmed
	AssertionRejected
)

// Challenge represents a challenge instance in the protocol, with associated
// methods about its lifecycle, getters of its internals, and methods that
// make mutating calls to the chain for adding leaves and confirming / rejecting
// a challenge.
type Challenge interface {
	// Getters.
	Id() ChallengeHash
	TopLevelClaimVertex(ctx context.Context) (ChallengeVertex, error)
	GetType() ChallengeType
	WinningClaim(ctx context.Context) (util.Option[AssertionHash], error)
	RootAssertion(ctx context.Context) (Assertion, error)
	RootVertex(ctx context.Context) (ChallengeVertex, error)
	WinnerVertex(ctx context.Context) (util.Option[ChallengeVertex], error)
	Completed(ctx context.Context) (bool, error)
	Challenger() common.Address

	// Mutating calls.
	AddBlockChallengeLeaf(
		ctx context.Context,
		assertion Assertion,
		history util.HistoryCommitment,
	) (ChallengeVertex, error)
	AddSubChallengeLeaf(
		ctx context.Context,
		vertex ChallengeVertex,
		history util.HistoryCommitment,
	) (ChallengeVertex, error)
}

// ChallengeVertex represents a challenge vertex instance in the protocol, with associated
// methods about its lifecycle, getters of its internals, and methods that
// make mutating calls to the chain for making challenge moves.
type ChallengeVertex interface {
	// Getters.
	Id() [32]byte
	HistoryCommitment() util.HistoryCommitment
	Status(ctx context.Context) (AssertionState, error)
	MiniStaker(ctx context.Context) (common.Address, error)
	Prev(ctx context.Context) (util.Option[ChallengeVertex], error)
	GetSubChallenge(ctx context.Context) (util.Option[Challenge], error)
	HasConfirmedSibling(ctx context.Context) (bool, error)

	// Presumptive status / timer readers.
	IsPresumptiveSuccessor(ctx context.Context) (bool, error)
	PsTimer(ctx context.Context) (uint64, error)
	ChildrenAreAtOneStepFork(ctx context.Context) (bool, error)

	// Mutating calls for challenge moves.
	CreateSubChallenge(ctx context.Context) (Challenge, error)
	Bisect(ctx context.Context, history util.HistoryCommitment, proof []byte) (ChallengeVertex, error)

	// Mutating calls for confirmations.
	ConfirmForPsTimer(ctx context.Context) error
	ConfirmForSubChallengeWin(ctx context.Context) error
}
