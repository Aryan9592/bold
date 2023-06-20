package challengemanager

import (
	"context"
	"fmt"
	"time"

	protocol "github.com/OffchainLabs/challenge-protocol-v2/chain-abstraction"
	watcher "github.com/OffchainLabs/challenge-protocol-v2/challenge-manager/chain-watcher"
	edgetracker "github.com/OffchainLabs/challenge-protocol-v2/challenge-manager/edge-tracker"
	"github.com/OffchainLabs/challenge-protocol-v2/containers/threadsafe"
	l2stateprovider "github.com/OffchainLabs/challenge-protocol-v2/layer2-state-provider"
	retry "github.com/OffchainLabs/challenge-protocol-v2/runtime"
	"github.com/OffchainLabs/challenge-protocol-v2/solgen/go/challengeV2gen"
	"github.com/OffchainLabs/challenge-protocol-v2/solgen/go/rollupgen"
	utilTime "github.com/OffchainLabs/challenge-protocol-v2/time"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithField("prefix", "challenge-manager")

type Opt = func(val *Manager)

// Manager defines an offchain, challenge manager, which will be
// an active participant in interacting with the on-chain contracts.
type Manager struct {
	chain                   protocol.Protocol
	chalManagerAddr         common.Address
	rollupAddr              common.Address
	rollup                  *rollupgen.RollupCore
	rollupFilterer          *rollupgen.RollupCoreFilterer
	chalManager             *challengeV2gen.EdgeChallengeManagerFilterer
	backend                 bind.ContractBackend
	stateManager            l2stateprovider.Provider
	address                 common.Address
	name                    string
	timeRef                 utilTime.Reference
	edgeTrackerWakeInterval time.Duration
	chainWatcherInterval    time.Duration
	watcher                 *watcher.Watcher
	trackedEdgeIds          *threadsafe.Set[protocol.EdgeId]
	assertionIdCache        *threadsafe.Map[protocol.AssertionId, [2]uint64]
}

// WithName is a human-readable identifier for this challenge manager for logging purposes.
func WithName(name string) Opt {
	return func(val *Manager) {
		val.name = name
	}
}

// WithAddress gives a staker address to the validator.
func WithAddress(addr common.Address) Opt {
	return func(val *Manager) {
		val.address = addr
	}
}

// WithEdgeTrackerWakeInterval specifies how often each edge tracker goroutine will
// act on its responsibilities.
func WithEdgeTrackerWakeInterval(d time.Duration) Opt {
	return func(val *Manager) {
		val.edgeTrackerWakeInterval = d
	}
}

// New sets up a challenge manager instance provided a protocol, state manager, and additional options.
func New(
	ctx context.Context,
	chain protocol.Protocol,
	backend bind.ContractBackend,
	stateManager l2stateprovider.Provider,
	rollupAddr common.Address,
	opts ...Opt,
) (*Manager, error) {
	m := &Manager{
		backend:                 backend,
		chain:                   chain,
		stateManager:            stateManager,
		address:                 common.Address{},
		timeRef:                 utilTime.NewRealTimeReference(),
		rollupAddr:              rollupAddr,
		edgeTrackerWakeInterval: time.Millisecond * 100,
		chainWatcherInterval:    time.Millisecond * 500,
		trackedEdgeIds:          threadsafe.NewSet[protocol.EdgeId](),
		assertionIdCache:        threadsafe.NewMap[protocol.AssertionId, [2]uint64](),
	}
	for _, o := range opts {
		o(m)
	}
	chalManager, err := m.chain.SpecChallengeManager(ctx)
	if err != nil {
		return nil, err
	}
	chalManagerAddr := chalManager.Address()

	rollup, err := rollupgen.NewRollupCore(rollupAddr, backend)
	if err != nil {
		return nil, err
	}
	rollupFilterer, err := rollupgen.NewRollupCoreFilterer(rollupAddr, backend)
	if err != nil {
		return nil, err
	}
	chalManagerFilterer, err := challengeV2gen.NewEdgeChallengeManagerFilterer(chalManagerAddr, backend)
	if err != nil {
		return nil, err
	}
	m.rollup = rollup
	m.rollupFilterer = rollupFilterer
	m.chalManagerAddr = chalManagerAddr
	m.chalManager = chalManagerFilterer
	m.watcher = watcher.New(m.chain, m, m.stateManager, backend, m.chainWatcherInterval, m.name)
	return m, nil
}

func (m *Manager) IsTrackingEdge(edgeId protocol.EdgeId) bool {
	return m.trackedEdgeIds.Has(edgeId)
}

func (m *Manager) MarkTrackedEdge(edgeId protocol.EdgeId) {
	m.trackedEdgeIds.Insert(edgeId)
}

func (m *Manager) TrackEdge(ctx context.Context, edge protocol.SpecEdge) error {
	trk, err := m.getTrackerForEdge(ctx, edge)
	if err != nil {
		return err
	}
	go trk.Spawn(ctx)
	return nil
}

func (m *Manager) getTrackerForEdge(ctx context.Context, edge protocol.SpecEdge) (*edgetracker.Tracker, error) {
	// Retry until you get the previous assertion ID.
	assertionId, err := retry.UntilSucceeds(ctx, func() (protocol.AssertionId, error) {
		return edge.AssertionId(ctx)
	})
	if err != nil {
		return nil, err
	}

	// Smart caching to avoid querying the same assertion number and creation info multiple times.
	// Edges in the same challenge should have the same creation info.
	cachedHeightAndInboxMsgCount, ok := m.assertionIdCache.TryGet(assertionId)
	var assertionHeight uint64
	var inboxMsgCount uint64
	if !ok {
		// Retry until you get the assertion creation info.
		assertionCreationInfo, creationErr := retry.UntilSucceeds(ctx, func() (*protocol.AssertionCreatedInfo, error) {
			return m.chain.ReadAssertionCreationInfo(ctx, assertionId)
		})
		if creationErr != nil {
			return nil, creationErr
		}

		// Retry until you get the execution state block height.
		height, heightErr := retry.UntilSucceeds(ctx, func() (uint64, error) {
			return m.getExecutionStateBlockHeight(ctx, assertionCreationInfo.AfterState)
		})
		if heightErr != nil {
			return nil, heightErr
		}
		assertionHeight = height
		inboxMsgCount = assertionCreationInfo.InboxMaxCount.Uint64()
		m.assertionIdCache.Put(assertionId, [2]uint64{assertionHeight, inboxMsgCount})
	} else {
		assertionHeight, inboxMsgCount = cachedHeightAndInboxMsgCount[0], cachedHeightAndInboxMsgCount[1]
	}
	return retry.UntilSucceeds(ctx, func() (*edgetracker.Tracker, error) {
		return edgetracker.New(
			edge,
			m.chain,
			m.stateManager,
			m.watcher,
			m,
			edgetracker.HeightConfig{
				StartBlockHeight:           assertionHeight,
				TopLevelClaimEndBatchCount: inboxMsgCount,
			},
			edgetracker.WithActInterval(m.edgeTrackerWakeInterval),
			edgetracker.WithTimeReference(m.timeRef),
			edgetracker.WithValidatorAddress(m.address),
			edgetracker.WithValidatorName(m.name),
		)
	})
}

func (m *Manager) Start(ctx context.Context) {
	log.WithField(
		"address",
		m.address.Hex(),
	).Info("Started challenge manager")

	// Start watching for ongoing chain events in the background.
	go m.watcher.Watch(ctx)
}

func (m *Manager) getExecutionStateBlockHeight(ctx context.Context, st rollupgen.ExecutionState) (uint64, error) {
	height, ok := m.stateManager.ExecutionStateBlockHeight(ctx, protocol.GoExecutionStateFromSolidity(st))
	if !ok {
		return 0, fmt.Errorf("missing previous assertion after execution %+v in local state manager", st)
	}
	return height, nil
}
