package validator

import (
	"context"
	"fmt"
	"time"

	"github.com/OffchainLabs/new-rollup-exploration/protocol"
	statemanager "github.com/OffchainLabs/new-rollup-exploration/state-manager"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithField("prefix", "validator")

type Opt = func(val *Validator)

type Validator struct {
	protocol               protocol.OnChainProtocol
	stateManager           statemanager.Manager
	assertionEvents        <-chan protocol.AssertionChainEvent
	stateUpdateEvents      <-chan *statemanager.StateAdvancedEvent
	address                common.Address
	createLeafInterval     time.Duration
	maliciousProbability   float64
	chaosMonkeyProbability float64
}

func WithMaliciousProbability(p float64) Opt {
	return func(val *Validator) {
		val.maliciousProbability = p
	}
}

func WithAddress(addr common.Address) Opt {
	return func(val *Validator) {
		val.address = addr
	}
}

func WithCreateLeafEvery(d time.Duration) Opt {
	return func(val *Validator) {
		val.createLeafInterval = d
	}
}

func New(
	ctx context.Context,
	onChainProtocol protocol.OnChainProtocol,
	stateManager statemanager.Manager,
	opts ...Opt,
) (*Validator, error) {
	v := &Validator{
		protocol:           onChainProtocol,
		stateManager:       stateManager,
		address:            common.Address{},
		createLeafInterval: 5 * time.Second,
	}
	for _, o := range opts {
		o(v)
	}
	// TODO: Prefer an API where the caller provides the channel and we can subscribe to all challenge and
	// assertion chain events. Provide the ability to specify the type of the subscription.
	v.assertionEvents = v.protocol.Subscribe(ctx)
	v.stateUpdateEvents = v.stateManager.SubscribeStateEvents(ctx)
	return v, nil
}

func (v *Validator) Start(ctx context.Context) {
	go v.listenForAssertionEvents(ctx)
	go v.submitLeafCreationPeriodically(ctx)
}

// TODO: Simulate posting leaf events with some jitter delay, validators will have
// latency in posting created leaves to the protocol.
func (v *Validator) submitLeafCreationPeriodically(ctx context.Context) {
	ticker := time.NewTicker(v.createLeafInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			prevAssertion := v.protocol.LatestConfirmed()
			currentCommit := v.stateManager.LatestHistoryCommitment(ctx)
			commit := protocol.StateCommitment{
				Height: currentCommit.Height,
				State:  currentCommit.Merkle,
			}
			if _, err := v.protocol.CreateLeaf(prevAssertion, commit, v.address); err != nil {
				panic(err)
			}
			log.WithFields(logrus.Fields{
				"latestConfirmedHeight": fmt.Sprintf("%+v", prevAssertion.SequenceNum),
				"leafHeight":            commit.Height,
				"leafCommitment":        fmt.Sprintf("%#x", commit.Hash()),
				"staker":                fmt.Sprintf("%#x", v.address),
			}).Info("Submitted leaf creation")
		case <-ctx.Done():
			return
		}
	}
}

func (v *Validator) listenForAssertionEvents(ctx context.Context) {
	for {
		select {
		case genericEvent := <-v.assertionEvents:
			switch ev := genericEvent.(type) {
			case *protocol.CreateLeafEvent:
				if v.isCorrectLeaf(ctx, ev) {
					log.WithFields(logrus.Fields{
						"height":     ev.Commitment.Height,
						"commitment": fmt.Sprintf("%#x", ev.Commitment.Hash()),
						"staker":     fmt.Sprintf("%#x", v.address),
					}).Info("Leaf creation matches local state")
					v.defendLeaf(ev)
				} else {
					log.WithFields(logrus.Fields{
						"height":     ev.Commitment.Height,
						"commitment": fmt.Sprintf("%#x", ev.Commitment.Hash()),
						"staker":     fmt.Sprintf("%#x", v.address),
					}).Warn("Leaf creation DOES NOT MATCH local state")
					v.challengeLeaf(ev)
				}
			case *protocol.StartChallengeEvent:
				v.processChallengeStart(ctx, ev)
			default:
				panic("not a recognized assertion chain event")
			}
		case <-ctx.Done():
			return
		}
	}
}

func (v *Validator) isCorrectLeaf(ctx context.Context, ev *protocol.CreateLeafEvent) bool {
	localCommitment, err := v.stateManager.HistoryCommitmentAtHeight(ctx, ev.Commitment.Height)
	if err != nil {
		panic(err)
	}
	return localCommitment != ev.Commitment.Hash()
}

func (v *Validator) defendLeaf(ev *protocol.CreateLeafEvent) {
}

func (v *Validator) challengeLeaf(ev *protocol.CreateLeafEvent) {
}

func (v *Validator) processChallengeStart(ctx context.Context, ev *protocol.StartChallengeEvent) {

}
