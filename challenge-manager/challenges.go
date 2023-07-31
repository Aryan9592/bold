// Copyright 2023, Offchain Labs, Inc.
// For license information, see https://github.com/offchainlabs/bold/blob/main/LICENSE

package challengemanager

import (
	"context"
	"fmt"

	protocol "github.com/OffchainLabs/bold/chain-abstraction"
	edgetracker "github.com/OffchainLabs/bold/challenge-manager/edge-tracker"
	"github.com/OffchainLabs/bold/containers"
	"github.com/ethereum/go-ethereum/log"
	"github.com/pkg/errors"
)

// (start, end) = edge
// bisect(edge) -> (lower, upper)

// blocks, state root
// (0=0xab, 2048=0xe7) = honest, level zero edge
// (0=0xab, 1024=0xcc), (1024=0xcc, 2048=0xe7) = honest

// (0=0xab, 2048=0xf8) = evil, level zero edge
// (0=0xab, 1024=0xcc), (1024=0xcc, 2048=0xf8) = evil

// alice = (1024=0xcc, 1025=0xdd)
// bob = (1024=0xcc, 1025=0xee)

// -> open subchallenge
// alice = asm opcode commitments (0=0x33, 2394029304929342=0x44) level zero edge
// 	subchal_edge_alice = what edge was it opened on? opened on 1024,1025 one layer above
// bob = asm opcode commitments (0=0x33, 2394029304929342=0x55) level zero edge
// 	subchal_edge_bob = what edge was it opened on? opened on 1024,1025 one layer above

// 2^43 max wasm opcodes per block

// ChallengeAssertion initiates a challenge on an assertion added to the protocol by finding its parent assertion
// and starting a challenge transaction. If the challenge creation is successful, we add a leaf
// with an associated history commitment to it and spawn a challenge tracker in the background.
func (m *Manager) ChallengeAssertion(ctx context.Context, id protocol.AssertionHash) error {
	assertion, err := m.chain.GetAssertion(ctx, id)
	if err != nil {
		return errors.Wrapf(err, "could not get assertion to challenge with id %#x", id)
	}

	// We then add a level zero edge to initiate a challenge.
	levelZeroEdge, creationInfo, err := m.addBlockChallengeLevelZeroEdge(ctx, assertion)
	if err != nil {
		return fmt.Errorf("could not add block challenge level zero edge %v: %w", m.name, err)
	}
	if !creationInfo.InboxMaxCount.IsUint64() {
		return errors.New("assertion creation info inbox max count was not a uint64")
	}
	// Start tracking the challenge.
	tracker, err := edgetracker.New(
		ctx,
		levelZeroEdge,
		m.chain,
		m.stateManager,
		m.watcher,
		m,
		edgetracker.HeightConfig{
			StartBlockHeight:           0,
			TopLevelClaimEndBatchCount: creationInfo.InboxMaxCount.Uint64(),
		},
		edgetracker.WithActInterval(m.edgeTrackerWakeInterval),
		edgetracker.WithTimeReference(m.timeRef),
		edgetracker.WithValidatorName(m.name),
	)
	if err != nil {
		return err
	}
	go tracker.Spawn(ctx)

	srvlog.Info("Successfully created level zero edge for block challenge", log.Ctx{
		"name":          m.name,
		"assertionHash": containers.Trunc(id.Bytes()),
	})
	return nil
}

func (m *Manager) addBlockChallengeLevelZeroEdge(
	ctx context.Context,
	assertion protocol.Assertion,
) (protocol.SpecEdge, *protocol.AssertionCreatedInfo, error) {
	creationInfo, err := m.chain.ReadAssertionCreationInfo(ctx, assertion.Id())
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not get assertion creation info")
	}
	if !creationInfo.InboxMaxCount.IsUint64() {
		return nil, nil, errors.New("creation info inbox max count was not a uint64")
	}
	parentAssertionInfo, err := m.chain.ReadAssertionCreationInfo(ctx, protocol.AssertionHash{Hash: creationInfo.ParentAssertionHash})
	if err != nil {
		return nil, nil, err
	}
	parentAssertionAfterState := protocol.GoExecutionStateFromSolidity(parentAssertionInfo.AfterState)
	startCommit, err := m.stateManager.HistoryCommitmentAtMessage(ctx, parentAssertionAfterState.GlobalState.Batch)
	if err != nil {
		return nil, nil, err
	}
	manager, err := m.chain.SpecChallengeManager(ctx)
	if err != nil {
		return nil, nil, err
	}
	levelZeroBlockEdgeHeight, err := manager.LevelZeroBlockEdgeHeight(ctx)
	if err != nil {
		return nil, nil, err
	}
	endCommit, err := m.stateManager.HistoryCommitmentUpToBatch(
		ctx,
		parentAssertionAfterState.GlobalState.Batch,
		parentAssertionAfterState.GlobalState.Batch+levelZeroBlockEdgeHeight,
		creationInfo.InboxMaxCount.Uint64()-1,
	)
	if err != nil {
		return nil, nil, err
	}
	startEndPrefixProof, err := m.stateManager.PrefixProofUpToBatch(
		ctx,
		parentAssertionAfterState.GlobalState.Batch,
		parentAssertionAfterState.GlobalState.Batch,
		parentAssertionAfterState.GlobalState.Batch+levelZeroBlockEdgeHeight,
		creationInfo.InboxMaxCount.Uint64()-1,
	)
	if err != nil {
		return nil, nil, err
	}
	edge, err := manager.AddBlockChallengeLevelZeroEdge(ctx, assertion, startCommit, endCommit, startEndPrefixProof)
	if err != nil {
		return nil, nil, err
	}
	return edge, creationInfo, nil
}
