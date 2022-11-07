package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/OffchainLabs/new-rollup-exploration/protocol"
	statemanager "github.com/OffchainLabs/new-rollup-exploration/state-manager"
	"github.com/OffchainLabs/new-rollup-exploration/util"
	"github.com/OffchainLabs/new-rollup-exploration/validator"
	"github.com/OffchainLabs/new-rollup-exploration/visualization"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

type simConfig struct {
	challengePeriod      time.Duration
	numSimulationHashes  uint64
	l2BlockTimes         time.Duration
	leafCreationInterval time.Duration
}

var (
	defaultSimConfig = &simConfig{
		challengePeriod: 100 * time.Second,
		// For the purposes of our simulation, we initialize 1000 blocks worth of "correct" hashes.
		numSimulationHashes: 1000,
		// For the simulation, we have 1 second block times in L2.
		l2BlockTimes: time.Second,
		// Honest validators submit leaf creation events every 5 seconds.
		leafCreationInterval: 5 * time.Second,
	}
	log = logrus.WithField("prefix", "main")
)

func init() {
	formatter := new(prefixed.TextFormatter)
	formatter.TimestampFormat = "2006-01-02 15:04:05"
	formatter.FullTimestamp = true
	log.Level = logrus.DebugLevel
	logrus.SetFormatter(formatter)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	log.WithField(
		"defaults",
		fmt.Sprintf("%+v", defaultSimConfig),
	).Info("Starting assertion protocol simulation")
	log.Info("View assertion chain live at http://localhost:3000")

	timeRef := util.NewRealTimeReference()
	chain := protocol.NewAssertionChain(ctx, timeRef, defaultSimConfig.challengePeriod)
	correctLeaves := prepareCorrectHashes(defaultSimConfig.numSimulationHashes)
	latestHeight := defaultSimConfig.numSimulationHashes - 1

	manager := statemanager.NewSimulatedManager(
		ctx,
		latestHeight,
		correctLeaves,
		statemanager.WithL2BlockTimes(defaultSimConfig.l2BlockTimes),
	)

	// We start our simulation with a single, honest validator.
	val, err := validator.New(
		ctx,
		chain,
		manager,
		validator.WithCreateLeafEvery(defaultSimConfig.leafCreationInterval),
		validator.WithMaliciousProbability(0), // Not a malicious validator for now...
	)
	if err != nil {
		panic(err)
	}

	vis := visualization.New(chain)
	go vis.Start(ctx)

	// Begin the validator process in the background.
	// The validator will not only be responsible for listening to new leaf and challenge creation events,
	// but will also participate in defending challenges it agrees with and challenge assertions it
	// disagrees with. Honest validators will also be responsible for creating new leaves in the assertion
	// tree based on the local state advancing, controlled by the state manager.
	//
	// All honest validators will have the same Merkle root as the state transition advances the chain,
	// and therefore all leaves posted by honest validators should have the same commitment. We give all honest
	// validators the same state reader. State reader is also advancing its state in the background:
	//
	// TODO: For the purposes of simulation, we plan to create several validators that can either be malicious, honest,
	// or chaos monkeys with some probability and we want to observe the behavior of the system.
	//
	// TODO: Create either a metrics collector that will gather information about the challenge games being
	// played and create an API that can extract a graphviz of the current assertion chain to visualize
	// the actual tree of assertions. Bonus: add detail such as presumptive status, chess clocks, etc.
	//
	// Observe:
	//  1. All honest validators
	//  2. Malicious validators issuing challenges on honestly-created leaves
	//  3. Honest validators issuing challenges on maliciously-created leaves
	//  4. Chaos monkey validators operating alongside honest ones.
	//
	val.Start(ctx)

	// Advance an L2 chain, and each time state is updated, an event will be sent over a feed
	// and honest validators that has access to the state manager will attempt to submit leaf creation
	// events to the contracts.
	go manager.AdvanceL2Chain(ctx)

	// Await a shutdown signal, which will trigger context cancellation across the program.
	exit := make(chan os.Signal, 1)
	signal.Notify(exit, syscall.SIGINT, syscall.SIGTERM)
	<-exit
	cancel()
}

func prepareCorrectHashes(numBlocks uint64) []common.Hash {
	ret := make([]common.Hash, numBlocks)
	for i := uint64(0); i < numBlocks; i++ {
		ret[i] = util.HashForUint(i)
	}
	return ret
}
