package challengetree

import (
	"fmt"
	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	"github.com/OffchainLabs/challenge-protocol-v2/util"
	"github.com/OffchainLabs/challenge-protocol-v2/util/threadsafe"
)

func (ht *HonestChallengeTree) PathTimer(e protocol.EdgeSnapshot, blockNum uint64) (uint64, error) {
	if blockNum < e.CreatedAtBlock() {
		return 0, nil
	}
	return 0, nil
}

// Gets the local timer of an edge at time T. If T is earlier than the edge's creation,
// this function will return 0.
func (ht *HonestChallengeTree) localTimer(e protocol.EdgeSnapshot, t uint64) (uint64, error) {
	if t < e.CreatedAtBlock() {
		return 0, nil
	}
	// If no rival at time t, then the local timer is defined
	// as t - t_creation(e).
	unrivaled, err := ht.unrivaledAtTime(e, t)
	if err != nil {
		return 0, err
	}
	if unrivaled {
		return t - e.CreatedAtBlock(), nil
	}
	// Else we return the earliest created rival's time: t_rival - t_creation(e).
	// This unwrap is safe because the edge has rivals at this point due to the check above.
	earliest := ht.earliestCreatedRivalTimestamp(e)
	tRival := earliest.Unwrap()
	if e.CreatedAtBlock() >= tRival {
		return 0, nil
	}
	return tRival - e.CreatedAtBlock(), nil
}

// Gets the minimum creation timestamp across all of an edge's rivals. If an edge
// has no rivals, this minimum is undefined.
func (ht *HonestChallengeTree) earliestCreatedRivalTimestamp(e protocol.EdgeSnapshot) util.Option[uint64] {
	rivals := ht.rivalsWithCreationTimes(e)
	creationBlocks := make([]uint64, len(rivals))
	for i, r := range rivals {
		creationBlocks[i] = uint64(r.createdAtBlock)
	}
	return util.Min(creationBlocks)
}

// Determines if an edge was unrivaled at timestamp T. If any rival existed
// for the edge at T, this function will return false.
func (ht *HonestChallengeTree) unrivaledAtTime(e protocol.EdgeSnapshot, t uint64) (bool, error) {
	if t < e.CreatedAtBlock() {
		return false, fmt.Errorf(
			"edge creation block %d less than specified %d",
			e.CreatedAtBlock(),
			t,
		)
	}
	rivals := ht.rivalsWithCreationTimes(e)
	if len(rivals) == 0 {
		return true, nil
	}
	for _, r := range rivals {
		// If a rival existed before or at the time of the edge's
		// creation, we then return false.
		if uint64(r.createdAtBlock) <= t {
			return false, nil
		}
	}
	return true, nil
}

// Contains a rival edge's id and its creation time.
type rival struct {
	id             protocol.EdgeId
	createdAtBlock creationTime
}

// Computes the set of rivals with their creation timestamp for an edge being tracked
// by the challenge tree. We do this by computing the mutual id of the edge and fetching
// all edge ids that share the same one from a set the challenge tree keeps track of.
// We exclude the specified edge from the returned list of rivals.
func (ht *HonestChallengeTree) rivalsWithCreationTimes(eg protocol.EdgeSnapshot) []*rival {
	rivals := make([]*rival, 0)
	mutualId := eg.MutualId()
	mutuals := ht.mutualIds.Get(mutualId)
	if mutuals == nil {
		ht.mutualIds.Put(mutualId, threadsafe.NewMap[protocol.EdgeId, creationTime]())
		return rivals
	}
	_ = mutuals.ForEach(func(rivalId protocol.EdgeId, t creationTime) error {
		if rivalId == eg.Id() {
			return nil
		}
		rivals = append(rivals, &rival{
			id:             rivalId,
			createdAtBlock: t,
		})
		return nil
	})
	return rivals
}
