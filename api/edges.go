package api

import (
	"context"
	"errors"
	"fmt"

	protocol "github.com/OffchainLabs/bold/chain-abstraction"
	challengetree "github.com/OffchainLabs/bold/challenge-manager/challenge-tree"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/sync/errgroup"
)

type Edge struct {
	ID                  common.Hash             `json:"id"`
	Type                string                  `json:"type"`
	StartCommitment     *Commitment             `json:"startCommitment"`
	EndCommitment       *Commitment             `json:"endCommitment"`
	CreatedAtBlock      uint64                  `json:"createdAtBlock"`
	MutualID            common.Hash             `json:"mutualId"`
	OriginID            common.Hash             `json:"originId"`
	ClaimID             common.Hash             `json:"claimId"`
	HasChildren         bool                    `json:"hasChildren"`
	LowerChildID        common.Hash             `json:"lowerChildId"`
	UpperChildID        common.Hash             `json:"upperChildId"`
	MiniStaker          common.Address          `json:"miniStaker"`
	AssertionHash       common.Hash             `json:"assertionHash"`
	TimeUnrivaled       uint64                  `json:"timeUnrivaled"`
	HasRival            bool                    `json:"hasRival"`
	Status              string                  `json:"status"`
	HasLengthOneRival   bool                    `json:"hasLengthOneRival"`
	TopLevelClaimHeight *protocol.OriginHeights `json:"topLevelClaimHeight"`
	CumulativePathTimer uint64                  `json:"cumulativePathTimer"`

	// Validator's point of view
	// IsHonest bool `json:"isHonest"`
	// AgreesWithStartCommitment `json:"agreesWithStartCommitment"`
}

type StakeInfo struct {
	StakerAddresses       []common.Address `json:"stakerAddresses"`
	NumberOfMinistakes    uint64           `json:"numberOfMinistakes"`
	StartCommitmentHeight uint64           `json:"startCommitmentHeight"`
	EndCommitmentHeight   uint64           `json:"endCommitmentHeight"`
}

type Ministakes struct {
	AssertionHash common.Hash `json:"assertionHash"`
	Level         string      `json:"level"`
	StakeInfo     *StakeInfo  `json:"stakeInfo"`
}

func (e *Edge) IsRootChallenge() bool {
	return e.Type == protocol.NewBlockChallengeLevel().String() && e.ClaimID == common.Hash{}
}

type Commitment struct {
	Height uint64      `json:"height"`
	Hash   common.Hash `json:"hash"`
}

func convertSpecEdgeEdgesToEdges(ctx context.Context, e []protocol.SpecEdge, edgesProvider EdgesProvider) ([]*Edge, error) {
	// Convert concurrently as some of the underlying methods are API calls.
	eg, ctx := errgroup.WithContext(ctx)

	edges := make([]*Edge, len(e))
	for i, edge := range e {
		index := i
		ee := edge

		eg.Go(func() (err error) {
			edges[index], err = convertSpecEdgeEdgeToEdge(ctx, ee, edgesProvider)
			return
		})
	}
	return edges, eg.Wait()
}

func convertSpecEdgeEdgeToEdge(ctx context.Context, e protocol.SpecEdge, edgesProvider EdgesProvider) (*Edge, error) {
	challengeLevel := e.GetChallengeLevel()
	edge := &Edge{
		ID:              e.Id().Hash,
		Type:            challengeLevel.String(),
		StartCommitment: toCommitment(e.StartCommitment),
		EndCommitment:   toCommitment(e.EndCommitment),
		MutualID:        common.Hash(e.MutualId()),
		OriginID:        common.Hash(e.OriginId()),
		ClaimID: func() common.Hash {
			if !e.ClaimId().IsNone() {
				return common.Hash(e.ClaimId().Unwrap())
			}
			return common.Hash{}
		}(),
		MiniStaker: func() common.Address {
			if !e.MiniStaker().IsNone() {
				return common.Address(e.MiniStaker().Unwrap())
			}
			return common.Address{}
		}(),
		CreatedAtBlock: func() uint64 {
			cab, err := e.CreatedAtBlock()
			if err != nil {
				return 0
			}
			return cab
		}(),
	}

	// The following methods include calls to the backend, so we run them concurrently.
	// Note: No rate limiting currently in place.
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		hasChildren, err := e.HasChildren(ctx)
		if err != nil {
			return fmt.Errorf("could not get edge children: %w", err)
		}
		edge.HasChildren = hasChildren
		return nil
	})

	eg.Go(func() error {
		lowerChild, err := e.LowerChild(ctx)
		if err != nil {
			return fmt.Errorf("could not get edge lower child: %w", err)
		}
		if !lowerChild.IsNone() {
			edge.LowerChildID = lowerChild.Unwrap().Hash
		}
		return nil
	})

	eg.Go(func() error {
		upperChild, err := e.UpperChild(ctx)
		if err != nil {
			return fmt.Errorf("could not get edge upper child: %w", err)
		}
		if !upperChild.IsNone() {
			edge.UpperChildID = upperChild.Unwrap().Hash
		}
		return nil
	})

	eg.Go(func() error {
		ah, err := e.AssertionHash(ctx)
		if err != nil {
			return fmt.Errorf("could not get edge assertion hash: %w", err)
		}
		edge.AssertionHash = ah.Hash

		cumulativePathTimer, _, _, err := edgesProvider.ComputeHonestPathTimer(ctx, ah, e.Id())
		if err != nil {
			if errors.Is(err, challengetree.ErrNoLowerChildYet) {
				return nil
			}
			return fmt.Errorf("failed to get edge cumulative path timer: %w", err)
		}
		edge.CumulativePathTimer = uint64(cumulativePathTimer)
		return nil
	})

	eg.Go(func() error {
		timeUnrivaled, err := e.TimeUnrivaled(ctx)
		if err != nil {
			return fmt.Errorf("could not get edge time unrivaled: %w", err)
		}
		edge.TimeUnrivaled = timeUnrivaled
		return nil
	})

	eg.Go(func() error {
		hasRival, err := e.HasRival(ctx)
		if err != nil {
			return fmt.Errorf("could not get edge has rival: %w", err)
		}
		edge.HasRival = hasRival
		return nil
	})

	eg.Go(func() error {
		status, err := e.Status(ctx)
		if err != nil {
			return fmt.Errorf("could not get edge status: %w", err)
		}
		edge.Status = status.String()
		return nil
	})

	eg.Go(func() error {
		hasLengthOneRival, err := e.HasLengthOneRival(ctx)
		if err != nil {
			return fmt.Errorf("could not get edge has length one rival: %w", err)
		}
		edge.HasLengthOneRival = hasLengthOneRival
		return nil
	})

	eg.Go(func() error {
		topLevelClaimHeight, err := e.TopLevelClaimHeight(ctx)
		if err != nil {
			return fmt.Errorf("could not get edge top level claim height: %w", err)
		}
		edge.TopLevelClaimHeight = &topLevelClaimHeight
		return nil
	})

	return edge, eg.Wait()
}

func toCommitment(fn func() (protocol.Height, common.Hash)) *Commitment {
	h, hs := fn()
	return &Commitment{
		Height: uint64(h),
		Hash:   hs,
	}
}
