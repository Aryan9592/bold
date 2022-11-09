package statemanager

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/OffchainLabs/new-rollup-exploration/protocol"
	"github.com/OffchainLabs/new-rollup-exploration/util"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithField("prefix", "state-manager")

type Manager interface {
	LatestHistoryCommitment(ctx context.Context) util.HistoryCommitment
	HasStateRoot(ctx context.Context, stateRoot common.Hash) bool
	HistoryCommitmentAtHeight(ctx context.Context, height uint64) (util.HistoryCommitment, error)
	SubscribeStateEvents(ctx context.Context, ch chan<- *StateAdvancedEvent)
}

type Simulated struct {
	currentHeight   *atomic.Uint64
	maxHeight       uint64
	lock            sync.RWMutex
	leaves          []common.Hash
	knownStateRoots map[common.Hash]bool
	stateTree       util.MerkleExpansion
	l2BlockTimes    time.Duration
	feed            *protocol.EventFeed[*StateAdvancedEvent]
}

type StateAdvancedEvent struct {
	HistoryCommitment *util.HistoryCommitment
}

type Opt func(*Simulated)

func WithL2BlockTimes(d time.Duration) Opt {
	return func(s *Simulated) {
		s.l2BlockTimes = d
	}
}

func NewSimulatedManager(ctx context.Context, maxHeight uint64, leaves []common.Hash, opts ...Opt) *Simulated {
	s := &Simulated{
		maxHeight:       maxHeight,
		currentHeight:   &atomic.Uint64{},
		leaves:          leaves,
		stateTree:       util.ExpansionFromLeaves(leaves[:1]),
		l2BlockTimes:    time.Second,
		feed:            protocol.NewEventFeed[*StateAdvancedEvent](ctx),
		knownStateRoots: make(map[common.Hash]bool),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *Simulated) SubscribeStateEvents(ctx context.Context, ch chan<- *StateAdvancedEvent) {
	s.feed.Subscribe(ctx, ch)
}

func (s *Simulated) HasStateRoot(ctx context.Context, stateRoot common.Hash) bool {
	// TODO: State commit is not the same as history commit! They are treated as the same for now
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.knownStateRoots[stateRoot]
}

// LatestHistoryCommitment --
func (s *Simulated) LatestHistoryCommitment(_ context.Context) util.HistoryCommitment {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return util.HistoryCommitment{
		Height: s.currentHeight.Load(),
		Merkle: s.stateTree.Root(),
	}
}

// HistoryCommitmentAtHeight --
// TODO: Match up with the existing state manager methods to rewind state, for example, for
// easier integration into the Nitro codebase.
func (s *Simulated) HistoryCommitmentAtHeight(_ context.Context, height uint64) (util.HistoryCommitment, error) {
	s.lock.RLock()
	if height >= uint64(len(s.leaves)) {
		return util.HistoryCommitment{Height: 0, Merkle: common.Hash{}}, fmt.Errorf("height %d exceeds available states %d", height, len(s.leaves))
	}
	treeAtHeight := util.ExpansionFromLeaves(s.leaves[:height+1])
	s.lock.RUnlock()
	return util.HistoryCommitment{
		Height: height,
		Merkle: treeAtHeight.Root(),
	}, nil
}

func (s *Simulated) AdvanceL2Chain(ctx context.Context) {
	tick := time.NewTicker(s.l2BlockTimes)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			s.currentHeight.Add(1)
			height := s.currentHeight.Load()
			s.lock.Lock()
			s.stateTree = s.stateTree.AppendLeaf(s.leaves[height])
			s.feed.Append(&StateAdvancedEvent{
				HistoryCommitment: &util.HistoryCommitment{
					Height: height,
					Merkle: s.stateTree.Root(),
				},
			})
			s.knownStateRoots[s.stateTree.Root()] = true
			log.WithFields(logrus.Fields{
				"newHeight": height,
				"merkle":    util.FormatHash(s.stateTree.Root()),
			}).Debug("Advancing L2 chain state")
			s.lock.Unlock()
		case <-ctx.Done():
			return
		}
	}
}
