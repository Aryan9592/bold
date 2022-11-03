package protocol

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/OffchainLabs/new-rollup-exploration/util"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"sync"
)

var (
	ErrWrongChain            = errors.New("wrong chain")
	ErrInvalid               = errors.New("invalid operation")
	ErrInvalidHeight         = errors.New("invalid block height")
	ErrVertexAlreadyExists   = errors.New("vertex already exists")
	ErrWrongState            = errors.New("vertex state does not allow this operation")
	ErrWrongPredecessorState = errors.New("predecessor state does not allow this operation")
	ErrNotYet                = errors.New("deadline has not yet passed")
	ErrNoWinnerYet           = errors.New("challenges does not yet have a winner")
	ErrPastDeadline          = errors.New("deadline has passed")
	ErrNotImplemented        = errors.New("not yet implemented")
)

type StateCommitment struct {
	height uint64
	state  common.Hash
}

func (comm *StateCommitment) Hash() common.Hash {
	return crypto.Keccak256Hash(binary.BigEndian.AppendUint64([]byte{}, comm.height), comm.state.Bytes())
}

type AssertionChain struct {
	mutex           sync.RWMutex
	timeReference   TimeReference
	challengePeriod SecondsDuration
	confirmedLatest uint64
	assertions      []*Assertion
	dedupe          map[common.Hash]bool
	feed            *EventFeed[AssertionChainEvent]
}

func (chain *AssertionChain) Tx(clo func(*AssertionChain) error) error {
	chain.mutex.Lock()
	defer chain.mutex.Unlock()
	return clo(chain)
}

func (chain *AssertionChain) Call(clo func(*AssertionChain) error) error {
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
	sequenceNum             uint64
	stateCommitment         StateCommitment
	prev                    util.Option[*Assertion]
	isFirstChild            bool
	firstChildCreationTime  util.Option[SecondsDuration]
	secondChildCreationTime util.Option[SecondsDuration]
	challenge               util.Option[*Challenge]
	staker                  util.Option[common.Address]
}

func NewAssertionChain(ctx context.Context, timeRef TimeReference, challengePeriod SecondsDuration) *AssertionChain {
	genesis := &Assertion{
		chain:       nil,
		status:      ConfirmedAssertionState,
		sequenceNum: 0,
		stateCommitment: StateCommitment{
			height: 0,
			state:  common.Hash{},
		},
		prev:                    util.EmptyOption[*Assertion](),
		isFirstChild:            false,
		firstChildCreationTime:  util.EmptyOption[SecondsDuration](),
		secondChildCreationTime: util.EmptyOption[SecondsDuration](),
		challenge:               util.EmptyOption[*Challenge](),
		staker:                  util.EmptyOption[common.Address](),
	}
	chain := &AssertionChain{
		mutex:           sync.RWMutex{},
		timeReference:   timeRef,
		challengePeriod: challengePeriod,
		confirmedLatest: 0,
		assertions:      []*Assertion{genesis},
		dedupe:          make(map[common.Hash]bool), // no need to insert genesis assertion here
		feed:            NewEventFeed[AssertionChainEvent](ctx),
	}
	genesis.chain = chain
	return chain
}

func (chain *AssertionChain) LatestConfirmed() *Assertion {
	return chain.assertions[chain.confirmedLatest]
}

func (chain *AssertionChain) Subscribe(ctx context.Context) <-chan AssertionChainEvent {
	return chain.feed.StartListener(ctx)
}

func (chain *AssertionChain) CreateLeaf(prev *Assertion, commitment StateCommitment, staker common.Address) (*Assertion, error) {
	if prev.chain != chain {
		return nil, ErrWrongChain
	}
	if prev.stateCommitment.height >= commitment.height {
		return nil, ErrInvalid
	}
	dedupeCode := crypto.Keccak256Hash(binary.BigEndian.AppendUint64(commitment.Hash().Bytes(), prev.sequenceNum))
	if chain.dedupe[dedupeCode] {
		return nil, ErrVertexAlreadyExists
	}
	leaf := &Assertion{
		chain:                   chain,
		status:                  PendingAssertionState,
		sequenceNum:             uint64(len(chain.assertions)),
		stateCommitment:         commitment,
		prev:                    util.FullOption[*Assertion](prev),
		isFirstChild:            prev.firstChildCreationTime.IsEmpty(),
		firstChildCreationTime:  util.EmptyOption[SecondsDuration](),
		secondChildCreationTime: util.EmptyOption[SecondsDuration](),
		challenge:               util.EmptyOption[*Challenge](),
		staker:                  util.FullOption[common.Address](staker),
	}
	if prev.firstChildCreationTime.IsEmpty() {
		prev.firstChildCreationTime = util.FullOption[SecondsDuration](chain.timeReference.Get())
	} else if prev.secondChildCreationTime.IsEmpty() {
		prev.secondChildCreationTime = util.FullOption[SecondsDuration](chain.timeReference.Get())
	}
	prev.staker = util.EmptyOption[common.Address]()
	chain.assertions = append(chain.assertions, leaf)
	chain.dedupe[dedupeCode] = true
	chain.feed.Append(&CreateLeafEvent{
		prevSeqNum: prev.sequenceNum,
		seqNum:     leaf.sequenceNum,
		commitment: leaf.stateCommitment,
		staker:     staker,
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
		seqNum: a.sequenceNum,
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
		seqNum: a.sequenceNum,
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
	if a.chain.timeReference.Get() <= prev.firstChildCreationTime.OpenKnownFull().SaturatingAdd(a.chain.challengePeriod) {
		return ErrNotYet
	}
	a.status = ConfirmedAssertionState
	a.chain.confirmedLatest = a.sequenceNum
	a.chain.feed.Append(&ConfirmEvent{
		seqNum: a.sequenceNum,
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
	a.chain.confirmedLatest = a.sequenceNum
	a.chain.feed.Append(&ConfirmEvent{
		seqNum: a.sequenceNum,
	})
	return nil
}

type Challenge struct {
	parent            *Assertion
	winner            *Assertion
	root              *ChallengeVertex
	latestConfirmed   *ChallengeVertex
	creationTime      SecondsDuration
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
		psTimer:              newCountUpTimer(parent.chain.timeReference),
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
		parentSeqNum: parent.sequenceNum,
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

	timer := newCountUpTimer(chain.timeReference)
	if assertion.isFirstChild {
		delta, err := prev.secondChildCreationTime.OpenKnownFull().Sub(prev.firstChildCreationTime.OpenKnownFull())
		if err != nil {
			return nil, err
		}
		timer.set(delta)
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
		sequenceNum:       leaf.sequenceNum,
		winnerIfConfirmed: assertion.sequenceNum,
		history:           history,
		becomesPS:         leaf.prev.presumptiveSuccessor == leaf,
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
	psTimer              *countUpTimer
	subChallenge         *SubChallenge
	winnerIfConfirmed    *Assertion
}

func (vertex *ChallengeVertex) eligibleForNewSuccessor() bool {
	return vertex.presumptiveSuccessor == nil || vertex.presumptiveSuccessor.psTimer.get() <= vertex.challenge.parent.chain.challengePeriod
}

func (vertex *ChallengeVertex) maybeNewPresumptiveSuccessor(succ *ChallengeVertex) {
	if vertex.presumptiveSuccessor != nil && succ.commitment.Height < vertex.presumptiveSuccessor.commitment.Height {
		vertex.presumptiveSuccessor.psTimer.stop()
		vertex.presumptiveSuccessor = nil
	}
	if vertex.presumptiveSuccessor == nil {
		vertex.presumptiveSuccessor = succ
		succ.psTimer.start()
	}
}

func (vertex *ChallengeVertex) isPresumptiveSuccessor() bool {
	return vertex.prev == nil || vertex.prev.presumptiveSuccessor == vertex
}

func (vertex *ChallengeVertex) requiredBisectionHeight() (uint64, error) {
	return util.BisectionPoint(vertex.prev.commitment.Height, vertex.commitment.Height)
}

func (vertex *ChallengeVertex) bisect(history util.HistoryCommitment, proof []common.Hash) error {
	if vertex.isPresumptiveSuccessor() {
		return ErrWrongState
	}
	if !vertex.prev.eligibleForNewSuccessor() {
		return ErrPastDeadline
	}
	if vertex.challenge.includedHistories[history.Hash()] {
		return ErrVertexAlreadyExists
	}
	bisectionHeight, err := vertex.requiredBisectionHeight()
	if err != nil {
		return err
	}
	if bisectionHeight != history.Height {
		return ErrInvalidHeight
	}
	if err := util.VerifyPrefixProof(history, vertex.commitment, proof); err != nil {
		return err
	}

	vertex.psTimer.stop()
	newVertex := &ChallengeVertex{
		challenge:            vertex.challenge,
		sequenceNum:          vertex.challenge.nextSequenceNum,
		isLeaf:               false,
		commitment:           history,
		prev:                 vertex.prev,
		presumptiveSuccessor: nil,
		psTimer:              vertex.psTimer.clone(),
	}
	newVertex.challenge.nextSequenceNum++
	newVertex.maybeNewPresumptiveSuccessor(vertex)
	newVertex.prev.maybeNewPresumptiveSuccessor(vertex)
	newVertex.challenge.includedHistories[history.Hash()] = true
	newVertex.challenge.feed.Append(&ChallengeBisectEvent{
		fromSequenceNum: vertex.sequenceNum,
		sequenceNum:     newVertex.sequenceNum,
		history:         newVertex.commitment,
		becomesPS:       newVertex.prev.presumptiveSuccessor == newVertex,
	})
	return nil
}

func (vertex *ChallengeVertex) merge(newPrev *ChallengeVertex, proof []common.Hash) error {
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
	newPrev.psTimer.add(vertex.psTimer.get())
	newPrev.maybeNewPresumptiveSuccessor(vertex)
	vertex.challenge.feed.Append(&ChallengeMergeEvent{
		deeperSequenceNum:    vertex.sequenceNum,
		shallowerSequenceNum: newPrev.sequenceNum,
		becomesPS:            newPrev.presumptiveSuccessor == vertex,
	})
	return nil
}

func (vertex *ChallengeVertex) confirmForSubChallengeWin() error {
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

func (vertex *ChallengeVertex) confirmForPsTimer() error {
	if vertex.status != PendingAssertionState {
		return ErrWrongState
	}
	if vertex.prev.status != ConfirmedAssertionState {
		return ErrWrongPredecessorState
	}
	if vertex.psTimer.get() <= vertex.challenge.parent.chain.challengePeriod {
		return ErrNotYet
	}
	vertex._confirm()
	return nil
}

func (vertex *ChallengeVertex) confirmForChallengeDeadline() error {
	if vertex.status != PendingAssertionState {
		return ErrWrongState
	}
	if vertex.prev.status != ConfirmedAssertionState {
		return ErrWrongPredecessorState
	}
	chain := vertex.challenge.parent.chain
	chalPeriod := chain.challengePeriod
	if chain.timeReference.Get() <= vertex.challenge.creationTime.SaturatingAdd(2*chalPeriod) {
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

func (vertex *ChallengeVertex) createSubChallenge() error {
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

func (sc *SubChallenge) setWinner(winner *ChallengeVertex) error {
	if sc.winner != nil {
		return ErrInvalid
	}
	if winner.prev != sc.parent {
		return ErrInvalid
	}
	sc.winner = winner
	return nil
}
