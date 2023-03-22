package validator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	solimpl "github.com/OffchainLabs/challenge-protocol-v2/protocol/sol-implementation"
	"github.com/OffchainLabs/challenge-protocol-v2/solgen/go/challengeV2gen"
	"github.com/OffchainLabs/challenge-protocol-v2/solgen/go/rollupgen"
	statemanager "github.com/OffchainLabs/challenge-protocol-v2/state-manager"
	"github.com/OffchainLabs/challenge-protocol-v2/util"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const defaultCreateLeafInterval = time.Second * 5

var log = logrus.WithField("prefix", "validator")

type Opt = func(val *Validator)

// Validator defines a validator client instances in the assertion protocol, which will be
// an active participant in interacting with the on-chain contracts.
type Validator struct {
	chain                                  protocol.Protocol
	chalManagerAddr                        common.Address
	rollupAddr                             common.Address
	rollup                                 *rollupgen.RollupCore
	rollupFilterer                         *rollupgen.RollupCoreFilterer
	chalManager                            *challengeV2gen.ChallengeManagerImplFilterer
	backend                                bind.ContractBackend
	stateManager                           statemanager.Manager
	address                                common.Address
	name                                   string
	knownValidatorNames                    map[common.Address]string
	createdAssertions                      map[common.Hash]protocol.Assertion
	assertionsLock                         sync.RWMutex
	sequenceNumbersByParentStateCommitment map[common.Hash][]protocol.AssertionSequenceNumber
	assertions                             map[protocol.AssertionSequenceNumber]protocol.Assertion
	leavesLock                             sync.RWMutex
	createLeafInterval                     time.Duration
	disableLeafCreation                    bool
	timeRef                                util.TimeReference
	challengeVertexWakeInterval            time.Duration
	newAssertionCheckInterval              time.Duration
}

// WithName is a human-readable identifier for this validator client for logging purposes.
func WithName(name string) Opt {
	return func(val *Validator) {
		val.name = name
	}
}

// WithAddress gives a staker address to the validator.
func WithAddress(addr common.Address) Opt {
	return func(val *Validator) {
		val.address = addr
	}
}

// WithTimeReference adds a time reference interface to the validator.
func WithTimeReference(ref util.TimeReference) Opt {
	return func(val *Validator) {
		val.timeRef = ref
	}
}

// WithChallengeVertexWakeInterval specifies how often each challenge vertex goroutine will
// act on its responsibilites.
func WithChallengeVertexWakeInterval(d time.Duration) Opt {
	return func(val *Validator) {
		val.challengeVertexWakeInterval = d
	}
}

// WithNewAssertionCheckInterval specifies how often handle assertions goroutine will
// act on its responsibilites.
func WithNewAssertionCheckInterval(d time.Duration) Opt {
	return func(val *Validator) {
		val.newAssertionCheckInterval = d
	}
}

// WithDisableLeafCreation disables scheduled, background submission of assertions to the protocol in the validator.
// Useful for testing.
func WithDisableLeafCreation() Opt {
	return func(val *Validator) {
		val.disableLeafCreation = true
	}
}

// New sets up a validator client instances provided a protocol, state manager,
// and additional options.
func New(
	ctx context.Context,
	chain protocol.Protocol,
	backend bind.ContractBackend,
	stateManager statemanager.Manager,
	rollupAddr common.Address,
	opts ...Opt,
) (*Validator, error) {
	v := &Validator{
		backend:                                backend,
		chain:                                  chain,
		stateManager:                           stateManager,
		address:                                common.Address{},
		createLeafInterval:                     defaultCreateLeafInterval,
		createdAssertions:                      make(map[common.Hash]protocol.Assertion),
		sequenceNumbersByParentStateCommitment: make(map[common.Hash][]protocol.AssertionSequenceNumber),
		assertions:                             make(map[protocol.AssertionSequenceNumber]protocol.Assertion),
		timeRef:                                util.NewRealTimeReference(),
		rollupAddr:                             rollupAddr,
		challengeVertexWakeInterval:            time.Millisecond * 100,
		newAssertionCheckInterval:              time.Second,
	}
	for _, o := range opts {
		o(v)
	}
	genesisAssertion, err := v.chain.AssertionBySequenceNum(ctx, 0)
	if err != nil {
		return nil, err
	}
	chalManager, err := v.chain.CurrentChallengeManager(ctx)
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
	chalManagerFilterer, err := challengeV2gen.NewChallengeManagerImplFilterer(chalManagerAddr, backend)
	if err != nil {
		return nil, err
	}
	v.rollup = rollup
	v.rollupFilterer = rollupFilterer
	v.assertions[0] = genesisAssertion
	v.chalManagerAddr = chalManagerAddr
	v.chalManager = chalManagerFilterer
	return v, nil
}

func (v *Validator) Start(ctx context.Context) {
	go v.handleChallengeEvents(ctx)
	go v.pollForAssertions(ctx)
	if !v.disableLeafCreation {
		go v.prepareLeafCreationPeriodically(ctx)
	}
	log.WithField(
		"address",
		v.address.Hex(),
	).Info("Started validator client")
}

func (v *Validator) prepareLeafCreationPeriodically(ctx context.Context) {
	ticker := time.NewTicker(v.createLeafInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			leaf, err := v.SubmitLeafCreation(ctx)
			if err != nil {
				log.WithError(err).Error("Could not submit leaf to protocol")
				continue
			}
			go v.confirmLeafAfterChallengePeriod(ctx, leaf)
		case <-ctx.Done():
			return
		}
	}
}

// SubmitLeafCreation submits a leaf creation to the protocol.
// TODO: Include leaf creation validity conditions which are more complex than this.
// For example, a validator must include messages from the inbox that were not included
// by the last validator in the last leaf's creation.
func (v *Validator) SubmitLeafCreation(ctx context.Context) (protocol.Assertion, error) {
	// Ensure that we only build on a valid parent from this validator's perspective.
	// the validator should also have ready access to historical commitments to make sure it can select
	// the valid parent based on its commitment state root.
	parentAssertionSeq, err := v.findLatestValidAssertion(ctx)
	if err != nil {
		return nil, err
	}
	parentAssertion, err := v.chain.AssertionBySequenceNum(ctx, parentAssertionSeq)
	if err != nil {
		return nil, err
	}
	parentAssertionHeight, err := parentAssertion.Height()
	if err != nil {
		return nil, err
	}
	assertionToCreate, err := v.stateManager.LatestAssertionCreationData(ctx, parentAssertionHeight)
	if err != nil {
		return nil, err
	}
	leaf, err := v.chain.CreateAssertion(ctx, assertionToCreate.Height, parentAssertionSeq, assertionToCreate.PreState, assertionToCreate.PostState, assertionToCreate.InboxMaxCount)
	switch {
	case errors.Is(err, solimpl.ErrAlreadyExists):
		return nil, errors.Wrap(err, "assertion already exists, unable to create new leaf")
	case err != nil:
		return nil, err
	}
	parentAssertionStateHash, err := parentAssertion.StateHash()
	if err != nil {
		return nil, err
	}
	leafStateHash, err := leaf.StateHash()
	if err != nil {
		return nil, err
	}
	leafHeight, err := leaf.Height()
	if err != nil {
		return nil, err
	}
	logFields := logrus.Fields{
		"name":               v.name,
		"parentHeight":       fmt.Sprintf("%+v", parentAssertionHeight),
		"parentStateHash":    fmt.Sprintf("%#x", parentAssertionStateHash),
		"assertionHeight":    leafHeight,
		"assertionStateHash": fmt.Sprintf("%#x", leafStateHash),
	}
	log.WithFields(logFields).Info("Submitted assertion")

	// Keep track of the created assertion locally.
	// TODO: Get the event from the chain instead, by using logs from the receipt.
	v.assertionsLock.Lock()
	// TODO: Store a more minimal struct, with only what we need.
	v.assertions[leaf.SeqNum()] = leaf
	v.sequenceNumbersByParentStateCommitment[parentAssertionStateHash] = append(
		v.sequenceNumbersByParentStateCommitment[parentAssertionStateHash],
		leaf.SeqNum(),
	)
	v.assertionsLock.Unlock()

	v.leavesLock.Lock()
	v.createdAssertions[leafStateHash] = leaf
	v.leavesLock.Unlock()
	return leaf, nil
}

// Finds the latest valid assertion sequence num a validator should build their new leaves upon. This walks
// down from the number of assertions in the protocol down until it finds
// an assertion that we have a state commitment for.
func (v *Validator) findLatestValidAssertion(ctx context.Context) (protocol.AssertionSequenceNumber, error) {
	numAssertions, err := v.chain.NumAssertions(ctx)
	if err != nil {
		return 0, err
	}
	latestConfirmedFetched, err := v.chain.LatestConfirmed(ctx)
	if err != nil {
		return 0, err
	}
	latestConfirmed := latestConfirmedFetched.SeqNum()
	v.assertionsLock.RLock()
	defer v.assertionsLock.RUnlock()
	for s := protocol.AssertionSequenceNumber(numAssertions); s > latestConfirmed; s-- {
		a, ok := v.assertions[s]
		if !ok {
			continue
		}
		height, err := a.Height()
		if err != nil {
			return 0, err
		}
		stateHash, err := a.StateHash()
		if err != nil {
			return 0, err
		}
		if v.stateManager.HasStateCommitment(ctx, util.StateCommitment{
			Height:    height,
			StateRoot: stateHash,
		}) {
			return a.SeqNum(), nil
		}
	}
	return latestConfirmed, nil
}

// For a leaf created by a validator, we confirm the leaf has no rival after the challenge deadline has passed.
// This function is meant to be ran as a goroutine for each leaf created by the validator.
func (v *Validator) confirmLeafAfterChallengePeriod(ctx context.Context, leaf protocol.Assertion) {
	manager, err := v.chain.CurrentChallengeManager(ctx)
	if err != nil {
		panic(err) // TODO: handle error instead of panic.
	}
	challengePeriodLength, err := manager.ChallengePeriodSeconds(ctx)
	if err != nil {
		panic(err) // TODO: handle error instead of panic.
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(challengePeriodLength))
	defer cancel()

	// TODO: Handle validator process dying here.
	<-ctx.Done()
	leafHeight, err := leaf.Height()
	if err != nil {
		panic(err)
	}
	logFields := logrus.Fields{
		"height":      leafHeight,
		"sequenceNum": leaf.SeqNum(),
	}
	if err := v.chain.Confirm(ctx, common.Hash{}, common.Hash{}); err != nil {
		log.WithError(err).WithFields(logFields).Warn("Could not confirm that created leaf had no rival")
		return
	}
	log.WithFields(logFields).Info("Confirmed leaf passed challenge period successfully on-chain")
}

// Processes new leaf creation events from the protocol that were not initiated by self.
func (v *Validator) onLeafCreated(
	ctx context.Context,
	assertion protocol.Assertion,
) error {
	assertionStateHash, err := assertion.StateHash()
	if err != nil {
		return err
	}
	assertionHeight, err := assertion.Height()
	if err != nil {
		return err
	}
	log.WithFields(logrus.Fields{
		"name":      v.name,
		"stateHash": fmt.Sprintf("%#x", assertionStateHash),
		"height":    assertionHeight,
	}).Info("New assertion appended to protocol")
	// Detect if there is a fork, then decide if we want to challenge.
	// We check if the parent assertion has > 1 child.
	v.assertionsLock.Lock()
	// Keep track of the created assertion locally.
	v.assertions[assertion.SeqNum()] = assertion
	v.assertionsLock.Unlock()

	// Keep track of assertions by parent state root to more easily detect forks.
	assertionPrevSeqNum, err := assertion.PrevSeqNum()
	if err != nil {
		return err
	}
	prevAssertion, err := v.chain.AssertionBySequenceNum(ctx, assertionPrevSeqNum)
	if err != nil {
		return err
	}

	v.assertionsLock.Lock()
	key, err := prevAssertion.StateHash()
	if err != nil {
		return err
	}
	v.sequenceNumbersByParentStateCommitment[key] = append(
		v.sequenceNumbersByParentStateCommitment[key],
		assertion.SeqNum(),
	)
	hasForked := len(v.sequenceNumbersByParentStateCommitment[key]) > 1
	v.assertionsLock.Unlock()

	// If this leaf's creation has not triggered fork, we have nothing else to do.
	if !hasForked {
		log.Info("No fork detected in assertion tree upon leaf creation")
		return nil
	}

	return v.challengeAssertion(ctx, assertion)
}

func isFromSelf(self, staker common.Address) bool {
	return self == staker
}
