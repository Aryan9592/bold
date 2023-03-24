package validator

import (
	"context"
	"time"

	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	"github.com/OffchainLabs/challenge-protocol-v2/solgen/go/challengeV2gen"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
)

// Subscribes to events fired by the rollup contracts in order to listen to
// challenge start events from the protocol.
// TODO: Brittle - should be based on querying the chain instead.
func (v *Validator) handleChallengeEvents(ctx context.Context) {
	challengeCreatedChan := make(chan *challengeV2gen.ChallengeManagerImplChallengeCreated, 1)
	chalSub, err := v.chalManager.WatchChallengeCreated(&bind.WatchOpts{}, challengeCreatedChan)
	if err != nil {
		log.Error(err)
		return
	}
	defer chalSub.Unsubscribe()

	for {
		select {
		case err := <-chalSub.Err():
			log.Fatal(err)
		case chalCreated := <-challengeCreatedChan:
			manager, err := v.chain.CurrentChallengeManager(ctx)
			if err != nil {
				log.WithError(err).Error("Failed to get current challenge manager")
				continue
			}
			retrieved, err := manager.GetChallenge(ctx, chalCreated.ChallengeId)
			if err != nil {
				log.WithError(err).Error("Failed to get challenge")
				continue
			}
			if retrieved.IsNone() {
				log.Errorf("no challenge with id %#x", chalCreated.ChallengeId)
				continue
			}
			challenge := retrieved.Unwrap()
			// Ignore challenges from self.
			challenger := challenge.Challenger()
			if isFromSelf(v.address, challenger) {
				continue
			}
			if err := v.onChallengeStarted(ctx, challenge); err != nil {
				log.Error(err)
			}
		}
	}
}

func (v *Validator) pollForAssertions(ctx context.Context) {
	ticker := time.NewTicker(v.newAssertionCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			numberOfAssertions, err := v.chain.NumAssertions(ctx)
			if err != nil {
				log.Error(err)
				continue
			}
			latestConfirmedAssertion, err := v.chain.LatestConfirmed(ctx)
			if err != nil {
				log.Error(err)
				continue
			}

			for i := uint64(latestConfirmedAssertion.SeqNum()); i < numberOfAssertions; i++ {
				v.assertionsLock.RLock()
				_, ok := v.assertions[protocol.AssertionSequenceNumber(i)]
				v.assertionsLock.RUnlock()
				if ok {
					continue
				}
				assertion, err := v.chain.AssertionBySequenceNum(ctx, protocol.AssertionSequenceNumber(i))
				if err != nil {
					log.Error(err)
					continue
				}
				v.assertions[assertion.SeqNum()] = assertion
				selfStakedAssertion, err := v.rollup.AssertionHasStaker(&bind.CallOpts{}, i, v.address)
				if err != nil {
					log.Error(err)
					continue
				}
				// Ignore assertions from self.
				if selfStakedAssertion {
					continue
				}
				if err := v.onLeafCreated(ctx, assertion); err != nil {
					log.Error(err)
				}
			}
		case <-ctx.Done():
			return
		}
	}
}
