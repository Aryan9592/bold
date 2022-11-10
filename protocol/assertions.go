package protocol

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/OffchainLabs/new-rollup-exploration/util"
	"github.com/emicklei/dot"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	ErrWrongChain            = errors.New("wrong chain")
	ErrInvalid               = errors.New("invalid operation")
	ErrInvalidHeight         = errors.New("invalid block Height")
	ErrVertexAlreadyExists   = errors.New("vertex already exists")
	ErrWrongState            = errors.New("vertex State does not allow this operation")
	ErrWrongPredecessorState = errors.New("predecessor State does not allow this operation")
	ErrNotYet                = errors.New("deadline has not yet passed")
	ErrNoWinnerYet           = errors.New("challenges does not yet have a winner")
	ErrPastDeadline          = errors.New("deadline has passed")
	ErrNotImplemented        = errors.New("not yet implemented")
)

// OnChainProtocol defines an interface for interacting with the smart contract implementation
// of the assertion protocol, with methods to issue mutating transactions, make eth calls, create
// leafs in the protocol, issue challenges, and subscribe to chain events wrapped in simple abstractions.
type OnChainProtocol interface {
	ChainReader
	ChainWriter
	EventsProvider
	AssertionManager
	ChainVisualizer
}

// ChainReader can make non-mutating calls to the on-chain protocol.
type ChainReader interface {
	ChallengePeriodLength() time.Duration
	Call(clo func(*AssertionChain) error) error
}

// ChainWriter can make mutating calls to the on-chain protocol.
type ChainWriter interface {
	Tx(clo func(*AssertionChain) error) error
}

// EventsProvider allows subscribing to chain events for the on-chain protocol.
type EventsProvider interface {
	SubscribeChainEvents(ctx context.Context, ch chan<- AssertionChainEvent)
}

// AssertionManager allows the creation of new leaves for a Staker with a State Commitment
// and a previous assertion.
type AssertionManager interface {
	NumAssertions() uint64
	Genesis() *Assertion
	LatestConfirmed() *Assertion
	LatestAssertion() *Assertion
	AssertionBySequenceNumber(sequenceNum uint64) (*Assertion, error)
	CreateLeaf(prev *Assertion, commitment StateCommitment, staker common.Address) (*Assertion, error)
}

type ChainVisualizer interface {
	Visualize() string
}

type AssertionChain struct {
	mutex               sync.RWMutex
	timeReference       util.TimeReference
	challengePeriod     time.Duration
	confirmedLatest     uint64
	assertions          []*Assertion
	dedupe              map[common.Hash]bool
	feed                *EventFeed[AssertionChainEvent]
	knownValidatorNames map[common.Address]string
}

func (chain *AssertionChain) Tx(clo func(chain *AssertionChain) error) error {
	chain.mutex.Lock()
	defer chain.mutex.Unlock()
	return clo(chain)
}

func (chain *AssertionChain) Call(clo func(chain *AssertionChain) error) error {
	chain.mutex.RLock()
	defer chain.mutex.RUnlock()
	return clo(chain)
}

const (
	PendingAssertionState = iota
	ConfirmedAssertionState
	RejectedAssertionState
)

type AssertionState int

type Assertion struct {
	chain                   *AssertionChain
	status                  AssertionState
	SequenceNum             uint64
	StateCommitment         StateCommitment
	prev                    util.Option[*Assertion]
	isFirstChild            bool
	firstChildCreationTime  util.Option[time.Time]
	secondChildCreationTime util.Option[time.Time]
	challenge               util.Option[*Challenge]
	staker                  util.Option[common.Address]
	creatorName             string
}

type StateCommitment struct {
	Height    uint64
	StateRoot common.Hash
}

func (comm *StateCommitment) Hash() common.Hash {
	return crypto.Keccak256Hash(binary.BigEndian.AppendUint64([]byte{}, comm.Height), comm.StateRoot.Bytes())
}

func NewAssertionChain(
	ctx context.Context,
	timeRef util.TimeReference,
	challengePeriod time.Duration,
	knownValidatorNames map[common.Address]string,
) *AssertionChain {
	genesis := &Assertion{
		chain:       nil,
		status:      ConfirmedAssertionState,
		SequenceNum: 0,
		StateCommitment: StateCommitment{
			Height:    0,
			StateRoot: common.Hash{},
		},
		prev:                    util.EmptyOption[*Assertion](),
		isFirstChild:            false,
		firstChildCreationTime:  util.EmptyOption[time.Time](),
		secondChildCreationTime: util.EmptyOption[time.Time](),
		challenge:               util.EmptyOption[*Challenge](),
		staker:                  util.EmptyOption[common.Address](),
	}
	chain := &AssertionChain{
		mutex:               sync.RWMutex{},
		timeReference:       timeRef,
		challengePeriod:     challengePeriod,
		confirmedLatest:     0,
		assertions:          []*Assertion{genesis},
		dedupe:              make(map[common.Hash]bool), // no need to insert genesis assertion here
		feed:                NewEventFeed[AssertionChainEvent](ctx),
		knownValidatorNames: knownValidatorNames,
	}
	genesis.chain = chain
	return chain
}

func (chain *AssertionChain) ChallengePeriodLength() time.Duration {
	return chain.challengePeriod
}

func (chain *AssertionChain) LatestConfirmed() *Assertion {
	chain.mutex.RLock()
	defer chain.mutex.RUnlock()
	return chain.assertions[chain.confirmedLatest]
}

func (chain *AssertionChain) Genesis() *Assertion {
	chain.mutex.RLock()
	defer chain.mutex.RUnlock()
	return chain.assertions[0]
}

func (chain *AssertionChain) NumAssertions() uint64 {
	chain.mutex.RLock()
	defer chain.mutex.RUnlock()
	return uint64(len(chain.assertions))
}

func (chain *AssertionChain) AssertionBySequenceNumber(seqNum uint64) (*Assertion, error) {
	chain.mutex.RLock()
	defer chain.mutex.RUnlock()
	if seqNum >= uint64(len(chain.assertions)) {
		return nil, fmt.Errorf(
			"assertion with sequence number %d exceeds num assertions, %d",
			seqNum,
			len(chain.assertions),
		)
	}
	return chain.assertions[seqNum], nil
}

func (chain *AssertionChain) LatestAssertion() *Assertion {
	chain.mutex.RLock()
	defer chain.mutex.RUnlock()
	return chain.assertions[len(chain.assertions)-1]
}

func (chain *AssertionChain) SubscribeChainEvents(ctx context.Context, ch chan<- AssertionChainEvent) {
	chain.feed.Subscribe(ctx, ch)
}

func (chain *AssertionChain) CreateLeaf(prev *Assertion, commitment StateCommitment, staker common.Address) (*Assertion, error) {
	chain.mutex.Lock()
	defer chain.mutex.Unlock()
	if prev.chain != chain {
		return nil, ErrWrongChain
	}
	if prev.StateCommitment.Height >= commitment.Height {
		return nil, ErrInvalid
	}
	dedupeCode := crypto.Keccak256Hash(binary.BigEndian.AppendUint64(commitment.Hash().Bytes(), prev.SequenceNum))
	if chain.dedupe[dedupeCode] {
		return nil, ErrVertexAlreadyExists
	}
	creatorName := "unknown"
	if name, ok := chain.knownValidatorNames[staker]; ok {
		creatorName = name
	}
	leaf := &Assertion{
		chain:                   chain,
		status:                  PendingAssertionState,
		SequenceNum:             uint64(len(chain.assertions)),
		StateCommitment:         commitment,
		prev:                    util.FullOption[*Assertion](prev),
		isFirstChild:            prev.firstChildCreationTime.IsEmpty(),
		firstChildCreationTime:  util.EmptyOption[time.Time](),
		secondChildCreationTime: util.EmptyOption[time.Time](),
		challenge:               util.EmptyOption[*Challenge](),
		staker:                  util.FullOption[common.Address](staker),
		creatorName:             creatorName,
	}
	if prev.firstChildCreationTime.IsEmpty() {
		prev.firstChildCreationTime = util.FullOption[time.Time](chain.timeReference.Get())
	} else if prev.secondChildCreationTime.IsEmpty() {
		prev.secondChildCreationTime = util.FullOption[time.Time](chain.timeReference.Get())
	}
	prev.staker = util.EmptyOption[common.Address]()
	chain.assertions = append(chain.assertions, leaf)
	chain.dedupe[dedupeCode] = true
	chain.feed.Append(&CreateLeafEvent{
		PrevSeqNum:      prev.SequenceNum,
		SeqNum:          leaf.SequenceNum,
		StateCommitment: leaf.StateCommitment,
		Staker:          staker,
	})
	return leaf, nil
}

func (a *Assertion) RejectForPrev() error {
	if a.status != PendingAssertionState {
		return ErrWrongState
	}
	if a.prev.IsEmpty() {
		return ErrInvalid
	}
	if a.prev.OpenKnownFull().status != RejectedAssertionState {
		return ErrWrongPredecessorState
	}
	a.status = RejectedAssertionState
	a.chain.feed.Append(&RejectEvent{
		SeqNum: a.SequenceNum,
	})
	return nil
}

func (a *Assertion) RejectForLoss() error {
	if a.status != PendingAssertionState {
		return ErrWrongState
	}
	if a.prev.IsEmpty() {
		return ErrInvalid
	}
	chal := a.prev.OpenKnownFull().challenge
	if chal.IsEmpty() {
		return util.ErrOptionIsEmpty
	}
	winner, err := chal.OpenKnownFull().Winner()
	if err != nil {
		return err
	}
	if winner == a {
		return ErrInvalid
	}
	a.status = RejectedAssertionState
	a.chain.feed.Append(&RejectEvent{
		SeqNum: a.SequenceNum,
	})
	return nil
}

func (a *Assertion) ConfirmNoRival() error {
	if a.status != PendingAssertionState {
		return ErrWrongState
	}
	if a.prev.IsEmpty() {
		return ErrInvalid
	}
	prev := a.prev.OpenKnownFull()
	if prev.status != ConfirmedAssertionState {
		return ErrWrongPredecessorState
	}
	if !prev.secondChildCreationTime.IsEmpty() {
		return ErrInvalid
	}
	if !a.chain.timeReference.Get().After(prev.firstChildCreationTime.OpenKnownFull().Add(a.chain.challengePeriod)) {
		return ErrNotYet
	}
	a.status = ConfirmedAssertionState
	a.chain.confirmedLatest = a.SequenceNum
	a.chain.feed.Append(&ConfirmEvent{
		SeqNum: a.SequenceNum,
	})
	return nil
}

func (a *Assertion) ConfirmForWin() error {
	if a.status != PendingAssertionState {
		return ErrWrongState
	}
	if a.prev.IsEmpty() {
		return ErrInvalid
	}
	prev := a.prev.OpenKnownFull()
	if prev.status != ConfirmedAssertionState {
		return ErrWrongPredecessorState
	}
	if prev.challenge.IsEmpty() {
		return ErrWrongPredecessorState
	}
	winner, err := prev.challenge.OpenKnownFull().Winner()
	if err != nil {
		return err
	}
	if winner != a {
		return ErrInvalid
	}
	a.status = ConfirmedAssertionState
	a.chain.confirmedLatest = a.SequenceNum
	a.chain.feed.Append(&ConfirmEvent{
		SeqNum: a.SequenceNum,
	})
	return nil
}

func (a *Assertion) Prev() util.Option[*Assertion] {
	return a.prev
}

type Challenge struct {
	parent            *Assertion
	winner            *Assertion
	root              *ChallengeVertex
	latestConfirmed   *ChallengeVertex
	creationTime      time.Time
	includedHistories map[common.Hash]bool
	nextSequenceNum   uint64
	feed              *EventFeed[ChallengeEvent]
}

func (parent *Assertion) CreateChallenge(ctx context.Context) (*Challenge, error) {
	if parent.status != PendingAssertionState && parent.chain.LatestConfirmed() != parent {
		return nil, ErrWrongState
	}
	if !parent.challenge.IsEmpty() {
		return nil, ErrInvalid
	}
	if parent.secondChildCreationTime.IsEmpty() {
		return nil, ErrInvalid
	}
	root := &ChallengeVertex{
		challenge:   nil,
		sequenceNum: 0,
		isLeaf:      false,
		status:      ConfirmedAssertionState,
		commitment: util.HistoryCommitment{
			Height: 0,
			Merkle: common.Hash{},
		},
		prev:                 nil,
		presumptiveSuccessor: nil,
		psTimer:              util.NewCountUpTimer(parent.chain.timeReference),
		subChallenge:         nil,
	}
	ret := &Challenge{
		parent:            parent,
		winner:            nil,
		root:              root,
		latestConfirmed:   root,
		creationTime:      parent.chain.timeReference.Get(),
		includedHistories: make(map[common.Hash]bool),
		nextSequenceNum:   1,
		feed:              NewEventFeed[ChallengeEvent](ctx),
	}
	root.challenge = ret
	ret.includedHistories[root.commitment.Hash()] = true
	parent.challenge = util.FullOption[*Challenge](ret)
	parent.chain.feed.Append(&StartChallengeEvent{
		ParentSeqNum: parent.SequenceNum,
	})
	return ret, nil
}

func (chal *Challenge) AddLeaf(assertion *Assertion, history util.HistoryCommitment) (*ChallengeVertex, error) {
	if assertion.prev.IsEmpty() {
		return nil, ErrInvalid
	}
	prev := assertion.prev.OpenKnownFull()
	if prev != chal.parent {
		return nil, ErrInvalid
	}
	if chal.Completed() {
		return nil, ErrWrongState
	}
	chain := assertion.chain
	if !chal.root.eligibleForNewSuccessor() {
		return nil, ErrPastDeadline
	}

	timer := util.NewCountUpTimer(chain.timeReference)
	if assertion.isFirstChild {
		delta := prev.secondChildCreationTime.OpenKnownFull().Sub(prev.firstChildCreationTime.OpenKnownFull())
		timer.Set(delta)
	}
	leaf := &ChallengeVertex{
		challenge:            chal,
		sequenceNum:          chal.nextSequenceNum,
		isLeaf:               true,
		status:               PendingAssertionState,
		commitment:           history,
		prev:                 chal.root,
		presumptiveSuccessor: nil,
		psTimer:              timer,
		subChallenge:         nil,
		winnerIfConfirmed:    assertion,
	}
	chal.nextSequenceNum++
	chal.root.maybeNewPresumptiveSuccessor(leaf)
	chal.feed.Append(&ChallengeLeafEvent{
		SequenceNum:       leaf.sequenceNum,
		WinnerIfConfirmed: assertion.SequenceNum,
		History:           history,
		BecomesPS:         leaf.prev.presumptiveSuccessor == leaf,
	})
	return leaf, nil
}

func (chal *Challenge) Completed() bool {
	return chal.winner != nil
}

func (chal *Challenge) Winner() (*Assertion, error) {
	if chal.winner == nil {
		return nil, ErrNoWinnerYet
	}
	return chal.winner, nil
}

type ChallengeVertex struct {
	challenge            *Challenge
	sequenceNum          uint64 // unique within the challenge
	isLeaf               bool
	status               AssertionState
	commitment           util.HistoryCommitment
	prev                 *ChallengeVertex
	presumptiveSuccessor *ChallengeVertex
	psTimer              *util.CountUpTimer
	subChallenge         *SubChallenge
	winnerIfConfirmed    *Assertion
}

func (vertex *ChallengeVertex) eligibleForNewSuccessor() bool {
	return vertex.presumptiveSuccessor == nil || vertex.presumptiveSuccessor.psTimer.Get() <= vertex.challenge.parent.chain.challengePeriod
}

func (vertex *ChallengeVertex) maybeNewPresumptiveSuccessor(succ *ChallengeVertex) {
	if vertex.presumptiveSuccessor != nil && succ.commitment.Height < vertex.presumptiveSuccessor.commitment.Height {
		vertex.presumptiveSuccessor.psTimer.Stop()
		vertex.presumptiveSuccessor = nil
	}
	if vertex.presumptiveSuccessor == nil {
		vertex.presumptiveSuccessor = succ
		succ.psTimer.Start()
	}
}

func (vertex *ChallengeVertex) isPresumptiveSuccessor() bool {
	return vertex.prev == nil || vertex.prev.presumptiveSuccessor == vertex
}

func (vertex *ChallengeVertex) requiredBisectionHeight() (uint64, error) {
	return util.BisectionPoint(vertex.prev.commitment.Height, vertex.commitment.Height)
}

func (vertex *ChallengeVertex) Bisect(history util.HistoryCommitment, proof []common.Hash) (*ChallengeVertex, error) {
	if vertex.isPresumptiveSuccessor() {
		return nil, ErrWrongState
	}
	if !vertex.prev.eligibleForNewSuccessor() {
		return nil, ErrPastDeadline
	}
	if vertex.challenge.includedHistories[history.Hash()] {
		return nil, ErrVertexAlreadyExists
	}
	bisectionHeight, err := vertex.requiredBisectionHeight()
	if err != nil {
		return nil, err
	}
	if bisectionHeight != history.Height {
		return nil, ErrInvalidHeight
	}
	if err := util.VerifyPrefixProof(history, vertex.commitment, proof); err != nil {
		return nil, err
	}

	vertex.psTimer.Stop()
	newVertex := &ChallengeVertex{
		challenge:            vertex.challenge,
		sequenceNum:          vertex.challenge.nextSequenceNum,
		isLeaf:               false,
		commitment:           history,
		prev:                 vertex.prev,
		presumptiveSuccessor: nil,
		psTimer:              vertex.psTimer.Clone(),
	}
	newVertex.challenge.nextSequenceNum++
	newVertex.maybeNewPresumptiveSuccessor(vertex)
	newVertex.prev.maybeNewPresumptiveSuccessor(vertex)
	newVertex.challenge.includedHistories[history.Hash()] = true
	newVertex.challenge.feed.Append(&ChallengeBisectEvent{
		FromSequenceNum: vertex.sequenceNum,
		SequenceNum:     newVertex.sequenceNum,
		History:         newVertex.commitment,
		BecomesPS:       newVertex.prev.presumptiveSuccessor == newVertex,
	})
	return newVertex, nil
}

func (vertex *ChallengeVertex) Merge(newPrev *ChallengeVertex, proof []common.Hash) error {
	if !newPrev.eligibleForNewSuccessor() {
		return ErrPastDeadline
	}
	if vertex.prev != newPrev.prev {
		return ErrInvalid
	}
	if vertex.commitment.Height <= newPrev.commitment.Height {
		return ErrInvalidHeight
	}
	if err := util.VerifyPrefixProof(newPrev.commitment, vertex.commitment, proof); err != nil {
		return err
	}

	vertex.prev = newPrev
	newPrev.psTimer.Add(vertex.psTimer.Get())
	newPrev.maybeNewPresumptiveSuccessor(vertex)
	vertex.challenge.feed.Append(&ChallengeMergeEvent{
		DeeperSequenceNum:    vertex.sequenceNum,
		ShallowerSequenceNum: newPrev.sequenceNum,
		BecomesPS:            newPrev.presumptiveSuccessor == vertex,
	})
	return nil
}

func (vertex *ChallengeVertex) ConfirmForSubChallengeWin() error {
	if vertex.status != PendingAssertionState {
		return ErrWrongState
	}
	if vertex.prev.status != ConfirmedAssertionState {
		return ErrWrongPredecessorState
	}
	subChal := vertex.prev.subChallenge
	if subChal == nil || subChal.winner != vertex {
		return ErrInvalid
	}
	vertex._confirm()
	return nil
}

func (vertex *ChallengeVertex) ConfirmForPsTimer() error {
	if vertex.status != PendingAssertionState {
		return ErrWrongState
	}
	if vertex.prev.status != ConfirmedAssertionState {
		return ErrWrongPredecessorState
	}
	if vertex.psTimer.Get() <= vertex.challenge.parent.chain.challengePeriod {
		return ErrNotYet
	}
	vertex._confirm()
	return nil
}

func (vertex *ChallengeVertex) ConfirmForChallengeDeadline() error {
	if vertex.status != PendingAssertionState {
		return ErrWrongState
	}
	if vertex.prev.status != ConfirmedAssertionState {
		return ErrWrongPredecessorState
	}
	chain := vertex.challenge.parent.chain
	chalPeriod := chain.challengePeriod
	if !chain.timeReference.Get().After(vertex.challenge.creationTime.Add(2 * chalPeriod)) {
		return ErrNotYet
	}
	vertex._confirm()
	return nil
}

func (vertex *ChallengeVertex) _confirm() {
	vertex.status = ConfirmedAssertionState
	if vertex.isLeaf {
		vertex.challenge.winner = vertex.winnerIfConfirmed
	}
}

func (vertex *ChallengeVertex) CreateSubChallenge() error {
	if vertex.subChallenge != nil {
		return ErrVertexAlreadyExists
	}
	if vertex.status == ConfirmedAssertionState {
		return ErrWrongState
	}
	vertex.subChallenge = &SubChallenge{
		parent: vertex,
		winner: nil,
	}
	return nil
}

type SubChallenge struct {
	parent *ChallengeVertex
	winner *ChallengeVertex
}

func (sc *SubChallenge) SetWinner(winner *ChallengeVertex) error {
	if sc.winner != nil {
		return ErrInvalid
	}
	if winner.prev != sc.parent {
		return ErrInvalid
	}
	sc.winner = winner
	return nil
}

type vizNode struct {
	isConfirmed bool
	parent      util.Option[*Assertion]
	assertion   *Assertion
	dotNode     dot.Node
}

// Visualize returns a graphviz string for the current assertion chain tree.
func (chain *AssertionChain) Visualize() string {
	graph := dot.NewGraph(dot.Directed)
	graph.Attr("rankdir", "BT")
	graph.Attr("labeljust", "l")

	assertions := chain.assertions
	latestConfirmed := chain.LatestConfirmed()
	// Construct nodes
	m := make(map[[32]byte]*vizNode)
	for i := 0; i < len(assertions); i++ {
		a := assertions[i]
		commit := a.StateCommitment
		// Construct label of each node.
		rStr := hex.EncodeToString(commit.Hash().Bytes())
		isConfirmed := a.SequenceNum <= latestConfirmed.SequenceNum
		label := fmt.Sprintf(
			"height: %d\n commitment: %#x\n Staker: %s\n Confirmed: %v",
			commit.Height,
			util.FormatHash(commit.Hash()),
			a.creatorName,
			isConfirmed,
		)

		dotN := graph.Node(rStr).Box().Attr("label", label)
		if isConfirmed {
			dotN.Attr("fillcolor", "#39C141").Attr("style", "filled")
		}
		m[commit.Hash()] = &vizNode{
			isConfirmed: isConfirmed,
			parent:      a.prev,
			assertion:   a,
			dotNode:     dotN,
		}
	}

	// Construct an edge only if block's parent exist in the tree.
	for _, n := range m {
		if !n.parent.IsEmpty() {
			parentHash := n.parent.OpenKnownFull().StateCommitment.Hash()
			if _, ok := m[parentHash]; ok {
				graph.Edge(n.dotNode, m[parentHash].dotNode)
			}
		}
	}
	return graph.String()
}
