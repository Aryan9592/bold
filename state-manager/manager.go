package statemanager

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/OffchainLabs/challenge-protocol-v2/execution"
	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	"github.com/OffchainLabs/challenge-protocol-v2/util"
	prefixproofs "github.com/OffchainLabs/challenge-protocol-v2/util/prefix-proofs"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Defines the ABI encoding structure for submission of prefix proofs to the protocol contracts
var (
	b32Arr, _ = abi.NewType("bytes32[]", "", nil)
	// ProofArgs for submission to the protocol.
	ProofArgs = abi.Arguments{
		{Type: b32Arr, Name: "prefixExpansion"},
		{Type: b32Arr, Name: "prefixProof"},
	}
)

// AssertionToCreate defines a struct that can provide local state data and historical
// Merkle commitments to L2 state for the validator.
type AssertionToCreate struct {
	PreState      *protocol.ExecutionState
	PostState     *protocol.ExecutionState
	InboxMaxCount *big.Int
	Height        uint64
}

type Manager interface {
	// Produces the latest assertion data to post to L1 from the local state manager's
	// perspective based on a parent assertion height.
	LatestAssertionCreationData(ctx context.Context, prevHeight uint64) (*AssertionToCreate, error)
	// Checks if a state commitment corresponds to data the state manager has locally.
	HasStateCommitment(ctx context.Context, blockChallengeCommitment util.StateCommitment) bool
	// Produces a block challenge history commitment up to and including a certain height.
	HistoryCommitmentUpTo(ctx context.Context, blockChallengeHeight uint64) (util.HistoryCommitment, error)
	// Produces a big step history commitment for all big steps within block
	// challenge heights H to H+1.
	BigStepLeafCommitment(
		ctx context.Context,
		fromBlockChallengeHeight,
		toBlockChallengeHeight uint64,
	) (util.HistoryCommitment, error)
	// Produces a big step history commitment from big step 0 to N within block
	// challenge heights A and B where B = A + 1.
	BigStepCommitmentUpTo(
		ctx context.Context,
		fromBlockChallengeHeight,
		toBlockChallengeHeight,
		toBigStep uint64,
	) (util.HistoryCommitment, error)
	// Produces a small step history commitment for all small steps between
	// big steps S to S+1 within block challenge heights H to H+1.
	SmallStepLeafCommitment(
		ctx context.Context,
		fromBlockChallengeHeight,
		toBlockChallengeHeight,
		fromBigStep,
		toBigStep uint64,
	) (util.HistoryCommitment, error)
	// Produces a small step history commitment from small step 0 to N between
	// big steps S to S+1 within block challenge heights H to H+1.
	SmallStepCommitmentUpTo(
		ctx context.Context,
		fromBlockChallengeHeight,
		toBlockChallengeHeight,
		fromBigStep,
		toBigStep,
		toSmallStep uint64,
	) (util.HistoryCommitment, error)
	// Produces a prefix proof in a block challenge from height A to B.
	PrefixProof(
		ctx context.Context,
		fromBlockChallengeHeight,
		toBlockChallengeHeight uint64,
	) ([]byte, error)
	// Produces a big step prefix proof from height A to B for heights H to H+1
	// within a block challenge.
	BigStepPrefixProof(
		ctx context.Context,
		fromBlockChallengeHeight,
		toBlockChallengeHeight,
		fromBigStep,
		toBigStep uint64,
	) ([]byte, error)
	// Produces a small step prefix proof from height A to B for big step S to S+1 and
	// block challenge height heights H to H+1.
	SmallStepPrefixProof(
		ctx context.Context,
		fromAssertionHeight,
		toAssertionHeight,
		fromBigStep,
		toBigStep,
		fromSmallStep,
		toSmallStep uint64,
	) ([]byte, error)
}

// Simulated defines a very naive state manager that is initialized from a list of predetermined
// state roots. It can produce state and history commitments from those roots.
type Simulated struct {
	stateRoots                []common.Hash
	executionStates           []*protocol.ExecutionState
	inboxMaxCounts            []*big.Int
	maxWavmOpcodes            uint64
	numOpcodesPerBigStep      uint64
	bigStepDivergenceHeight   uint64
	smallStepDivergenceHeight uint64
	malicious                 bool
}

// New simulated manager from a list of predefined state roots, useful for tests and simulations.
func New(stateRoots []common.Hash, opts ...Opt) (*Simulated, error) {
	if len(stateRoots) == 0 {
		return nil, errors.New("no state roots provided")
	}
	s := &Simulated{stateRoots: stateRoots}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

type Opt func(*Simulated)

func WithMaxWavmOpcodesPerBlock(maxOpcodes uint64) Opt {
	return func(s *Simulated) {
		s.maxWavmOpcodes = maxOpcodes
	}
}

func WithNumOpcodesPerBigStep(numOpcodes uint64) Opt {
	return func(s *Simulated) {
		s.numOpcodesPerBigStep = numOpcodes
	}
}

func WithBigStepStateDivergenceHeight(divergenceHeight uint64) Opt {
	return func(s *Simulated) {
		s.bigStepDivergenceHeight = divergenceHeight
	}
}

func WithSmallStepStateDivergenceHeight(divergenceHeight uint64) Opt {
	return func(s *Simulated) {
		s.smallStepDivergenceHeight = divergenceHeight
	}
}

func WithMaliciousIntent() Opt {
	return func(s *Simulated) {
		s.malicious = true
	}
}

// NewWithAssertionStates creates a simulated state manager from a list of predefined state roots for
// the top-level assertion chain, useful for tests and simulation purposes in block challenges.
// This also allows for specifying the honest states for big and small step subchallenges along
// with the point at which the state manager should diverge from the honest computation.
func NewWithAssertionStates(
	assertionChainExecutionStates []*protocol.ExecutionState,
	inboxMaxCounts []*big.Int,
	opts ...Opt,
) (*Simulated, error) {
	if len(assertionChainExecutionStates) == 0 {
		return nil, errors.New("must have execution states")
	}
	if len(assertionChainExecutionStates) != len(inboxMaxCounts) {
		return nil, fmt.Errorf(
			"number of exec states %d must match number of inbox max counts %d",
			len(assertionChainExecutionStates),
			len(inboxMaxCounts),
		)
	}
	stateRoots := make([]common.Hash, len(assertionChainExecutionStates))
	for i := 0; i < len(stateRoots); i++ {
		stateRoots[i] = protocol.ComputeStateHash(assertionChainExecutionStates[i], big.NewInt(2))
	}
	s := &Simulated{
		stateRoots:      stateRoots,
		executionStates: assertionChainExecutionStates,
		inboxMaxCounts:  inboxMaxCounts,
	}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// LatestAssertionCreationData gets the state commitment corresponding to the last, local state root the manager has
// and a pre-state based on a height of the previous assertion the validator should build upon.
func (s *Simulated) LatestAssertionCreationData(
	ctx context.Context,
	prevHeight uint64,
) (*AssertionToCreate, error) {
	if len(s.executionStates) == 0 {
		return nil, errors.New("no local execution states")
	}
	if prevHeight >= uint64(len(s.stateRoots)) {
		return nil, fmt.Errorf(
			"prev height %d cannot be >= %d state roots",
			prevHeight,
			len(s.stateRoots),
		)
	}
	lastState := s.executionStates[len(s.executionStates)-1]
	return &AssertionToCreate{
		PreState:      s.executionStates[prevHeight],
		PostState:     lastState,
		InboxMaxCount: big.NewInt(1),
		Height:        uint64(len(s.stateRoots)) - 1,
	}, nil
}

// HasStateCommitment checks if a state commitment is found in our local list of state roots.
func (s *Simulated) HasStateCommitment(ctx context.Context, commitment util.StateCommitment) bool {
	if commitment.Height >= uint64(len(s.stateRoots)) {
		return false
	}
	return s.stateRoots[commitment.Height] == commitment.StateRoot
}

func (s *Simulated) HistoryCommitmentUpTo(ctx context.Context, blockChallengeHeight uint64) (util.HistoryCommitment, error) {
	// The size is the number of elements being committed to. For example, if the height is 7, there will
	// be 8 elements being committed to from [0, 7] inclusive.
	size := blockChallengeHeight + 1
	return util.NewHistoryCommitment(
		blockChallengeHeight,
		s.stateRoots[:size],
	)
}

func (s *Simulated) BigStepLeafCommitment(
	ctx context.Context,
	fromAssertionHeight,
	toAssertionHeight uint64,
) (util.HistoryCommitment, error) {
	// Number of big steps between assertion heights A and B will be
	// fixed in this simulated state manager. It is simply the max number of opcodes
	// per block divided by the size of a big step.
	numBigSteps := s.maxWavmOpcodes / s.numOpcodesPerBigStep
	return s.BigStepCommitmentUpTo(
		ctx,
		fromAssertionHeight,
		toAssertionHeight,
		numBigSteps,
	)
}

func (s *Simulated) BigStepCommitmentUpTo(
	ctx context.Context,
	fromAssertionHeight,
	toAssertionHeight,
	toBigStep uint64,
) (util.HistoryCommitment, error) {
	if fromAssertionHeight+1 != toAssertionHeight {
		return util.HistoryCommitment{}, fmt.Errorf(
			"from height %d is not one-step away from to height %d",
			fromAssertionHeight,
			toAssertionHeight,
		)
	}
	engine, err := s.setupEngine(fromAssertionHeight, toAssertionHeight)
	if err != nil {
		return util.HistoryCommitment{}, err
	}
	if engine.NumBigSteps() < toBigStep {
		return util.HistoryCommitment{}, errors.New("not enough big steps")
	}
	leaves, err := s.intermediateBigStepLeaves(
		fromAssertionHeight,
		toAssertionHeight,
		0, // from big step.
		toBigStep,
		engine,
	)
	if err != nil {
		return util.HistoryCommitment{}, err
	}
	return util.NewHistoryCommitment(toBigStep, leaves)
}

func (s *Simulated) intermediateBigStepLeaves(
	fromBlockChallengeHeight,
	toBlockChallengeHeight,
	fromBigStep,
	toBigStep uint64,
	engine execution.EngineAtBlock,
) ([]common.Hash, error) {
	leaves := make([]common.Hash, 0)
	leaves = append(leaves, engine.FirstMachineState().Hash())
	// Up to and including the specified step.
	for i := fromBigStep; i < toBigStep; i++ {
		start, err := engine.StateAfterBigSteps(i)
		if err != nil {
			return nil, err
		}
		intermediateState, err := start.NextMachineState()
		if err != nil {
			return nil, err
		}
		var hash common.Hash

		// For testing purposes, if we want to diverge from the honest
		// hashes starting at a specified hash.
		if s.bigStepDivergenceHeight == 0 || i+1 < s.bigStepDivergenceHeight {
			hash = intermediateState.Hash()
		} else {
			hash = crypto.Keccak256Hash([]byte(fmt.Sprintf("%d:%d:%d:%d", i, fromBlockChallengeHeight, toBlockChallengeHeight, protocol.BigStepChallengeEdge)))
		}
		leaves = append(leaves, hash)
	}
	return leaves, nil
}

func (s *Simulated) SmallStepLeafCommitment(
	ctx context.Context,
	fromAssertionHeight,
	toAssertionHeight,
	fromBigStep,
	toBigStep uint64,
) (util.HistoryCommitment, error) {
	return s.SmallStepCommitmentUpTo(
		ctx,
		fromAssertionHeight,
		toAssertionHeight,
		fromBigStep,
		toBigStep,
		s.numOpcodesPerBigStep-1,
	)
}

func (s *Simulated) SmallStepCommitmentUpTo(
	ctx context.Context,
	fromBlockChallengeHeight,
	toBlockChallengeHeight,
	fromBigStep,
	toBigStep,
	toSmallStep uint64,
) (util.HistoryCommitment, error) {
	if fromBlockChallengeHeight+1 != toBlockChallengeHeight {
		return util.HistoryCommitment{}, fmt.Errorf(
			"from height %d is not one-step away from to height %d",
			fromBlockChallengeHeight,
			toBlockChallengeHeight,
		)
	}
	if fromBigStep+1 != toBigStep {
		return util.HistoryCommitment{}, fmt.Errorf(
			"from height %d is not one-step away from to height %d",
			fromBigStep,
			toBigStep,
		)
	}
	engine, err := s.setupEngine(fromBlockChallengeHeight, toBlockChallengeHeight)
	if err != nil {
		return util.HistoryCommitment{}, err
	}
	if engine.NumOpcodes() < toSmallStep {
		return util.HistoryCommitment{}, errors.New("not enough small steps")
	}

	fromSmall := (fromBigStep * s.numOpcodesPerBigStep)
	toSmall := fromSmall + toSmallStep
	leaves, err := s.intermediateSmallStepLeaves(
		fromBlockChallengeHeight,
		toBlockChallengeHeight,
		fromSmall,
		toSmall,
		engine,
	)
	if err != nil {
		return util.HistoryCommitment{}, err
	}
	return util.NewHistoryCommitment(toSmallStep, leaves)
}

func (s *Simulated) intermediateSmallStepLeaves(
	fromBlockChallengeHeight,
	toBlockChallengeHeight,
	fromSmallStep,
	toSmallStep uint64,
	engine execution.EngineAtBlock,
) ([]common.Hash, error) {
	leaves := make([]common.Hash, 0)
	leaves = append(leaves, engine.FirstMachineState().Hash())
	// Up to and including the specified step.
	divergingAt := fromSmallStep + s.smallStepDivergenceHeight
	for i := fromSmallStep; i < toSmallStep; i++ {
		start, err := engine.StateAfterSmallSteps(i)
		if err != nil {
			return nil, err
		}
		intermediateState, err := start.NextMachineState()
		if err != nil {
			return nil, err
		}
		var hash common.Hash

		// For testing purposes, if we want to diverge from the honest
		// hashes starting at a specified hash.
		if s.smallStepDivergenceHeight == 0 || i+1 < divergingAt {
			hash = intermediateState.Hash()
		} else {
			hash = crypto.Keccak256Hash([]byte(fmt.Sprintf("%d:%d:%d:%d", i, fromBlockChallengeHeight, toBlockChallengeHeight, protocol.SmallStepChallengeEdge)))
		}
		leaves = append(leaves, hash)
	}
	return leaves, nil
}

func (s *Simulated) PrefixProof(_ context.Context, lo, hi uint64) ([]byte, error) {
	loSize := lo + 1
	hiSize := hi + 1
	prefixExpansion, err := prefixproofs.ExpansionFromLeaves(s.stateRoots[:loSize])
	if err != nil {
		return nil, err
	}
	prefixProof, err := prefixproofs.GeneratePrefixProof(
		loSize,
		prefixExpansion,
		s.stateRoots[loSize:hiSize],
		prefixproofs.RootFetcherFromExpansion,
	)
	if err != nil {
		return nil, err
	}
	_, numRead := prefixproofs.MerkleExpansionFromCompact(prefixProof, loSize)
	onlyProof := prefixProof[numRead:]
	return ProofArgs.Pack(&prefixExpansion, &onlyProof)
}

func (s *Simulated) BigStepPrefixProof(
	_ context.Context,
	fromBlockChallengeHeight,
	toBlockChallengeHeight,
	fromBigStep,
	toBigStep uint64,
) ([]byte, error) {
	if fromBlockChallengeHeight+1 != toBlockChallengeHeight {
		return nil, fmt.Errorf(
			"fromAssertionHeight=%d is not 1 height apart from toAssertionHeight=%d",
			fromBlockChallengeHeight,
			toBlockChallengeHeight,
		)
	}
	engine, err := s.setupEngine(fromBlockChallengeHeight, toBlockChallengeHeight)
	if err != nil {
		return nil, err
	}
	if engine.NumBigSteps() < toBigStep {
		return nil, errors.New("wrong number of big steps")
	}
	return s.bigStepPrefixProofCalculation(
		fromBlockChallengeHeight,
		toBlockChallengeHeight,
		fromBigStep,
		toBigStep,
		engine,
	)
}

func (s *Simulated) bigStepPrefixProofCalculation(
	fromBlockChallengeHeight,
	toBlockChallengeHeight,
	fromBigStep,
	toBigStep uint64,
	engine execution.EngineAtBlock,
) ([]byte, error) {
	loSize := fromBigStep + 1
	hiSize := toBigStep + 1
	prefixLeaves, err := s.intermediateBigStepLeaves(
		fromBlockChallengeHeight,
		toBlockChallengeHeight,
		0,
		toBigStep,
		engine,
	)
	if err != nil {
		return nil, err
	}
	prefixExpansion, err := prefixproofs.ExpansionFromLeaves(prefixLeaves[:loSize])
	if err != nil {
		return nil, err
	}
	prefixProof, err := prefixproofs.GeneratePrefixProof(
		loSize,
		prefixExpansion,
		prefixLeaves[loSize:hiSize],
		prefixproofs.RootFetcherFromExpansion,
	)
	if err != nil {
		return nil, err
	}
	_, numRead := prefixproofs.MerkleExpansionFromCompact(prefixProof, loSize)
	onlyProof := prefixProof[numRead:]
	return ProofArgs.Pack(&prefixExpansion, &onlyProof)
}

func (s *Simulated) SmallStepPrefixProof(
	_ context.Context,
	fromBlockChallengeHeight,
	toBlockChallengeHeight,
	fromBigStep,
	toBigStep,
	fromSmallStep,
	toSmallStep uint64,
) ([]byte, error) {
	if fromBlockChallengeHeight+1 != toBlockChallengeHeight {
		return nil, fmt.Errorf(
			"fromAssertionHeight=%d is not 1 height apart from toAssertionHeight=%d",
			fromBlockChallengeHeight,
			toBlockChallengeHeight,
		)
	}
	if fromBigStep+1 != toBigStep {
		return nil, fmt.Errorf(
			"fromBigStep=%d is not 1 height apart from toBigStep=%d",
			fromBigStep,
			toBigStep,
		)
	}
	engine, err := s.setupEngine(fromBlockChallengeHeight, toBlockChallengeHeight)
	if err != nil {
		return nil, err
	}
	if engine.NumOpcodes() < toSmallStep {
		return nil, errors.New("wrong number of opcodes")
	}
	return s.smallStepPrefixProofCalculation(
		fromBlockChallengeHeight,
		toBlockChallengeHeight,
		fromBigStep,
		fromSmallStep,
		toSmallStep,
		engine,
	)
}

func (s *Simulated) setupEngine(fromHeight, toHeight uint64) (*execution.Engine, error) {
	machineCfg := execution.DefaultMachineConfig()
	if s.maxWavmOpcodes > 0 {
		machineCfg.MaxInstructionsPerBlock = s.maxWavmOpcodes
	}
	if s.numOpcodesPerBigStep > 0 {
		machineCfg.BigStepSize = s.numOpcodesPerBigStep
	}
	return execution.NewExecutionEngine(
		machineCfg,
		s.stateRoots[fromHeight],
		s.stateRoots[fromHeight+1],
	)
}

func (s *Simulated) smallStepPrefixProofCalculation(
	fromBlockChallengeHeight,
	toBlockChallengeHeight,
	fromBigStep,
	fromSmallStep,
	toSmallStep uint64,
	engine execution.EngineAtBlock,
) ([]byte, error) {
	fromSmall := (fromBigStep * s.numOpcodesPerBigStep)
	toSmall := fromSmall + toSmallStep
	prefixLeaves, err := s.intermediateSmallStepLeaves(
		fromBlockChallengeHeight,
		toBlockChallengeHeight,
		fromSmall,
		toSmall,
		engine,
	)
	if err != nil {
		return nil, err
	}
	loSize := fromSmallStep + 1
	hiSize := toSmallStep + 1
	prefixExpansion, err := prefixproofs.ExpansionFromLeaves(prefixLeaves[:loSize])
	if err != nil {
		return nil, err
	}
	prefixProof, err := prefixproofs.GeneratePrefixProof(
		loSize,
		prefixExpansion,
		prefixLeaves[loSize:hiSize],
		prefixproofs.RootFetcherFromExpansion,
	)
	if err != nil {
		return nil, err
	}
	_, numRead := prefixproofs.MerkleExpansionFromCompact(prefixProof, loSize)
	onlyProof := prefixProof[numRead:]
	return ProofArgs.Pack(&prefixExpansion, &onlyProof)
}
