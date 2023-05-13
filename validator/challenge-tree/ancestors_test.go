package challengetree

import (
	"testing"

	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	"github.com/OffchainLabs/challenge-protocol-v2/util"
	"github.com/OffchainLabs/challenge-protocol-v2/util/threadsafe"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// Tests the following tree, all the way down to the small
// step subchallenge level.
//
//		Block challenge:
//
//			      /--5---6-----8-----------16 = Alice
//			0-----4
//			      \--5'--6'----8'----------16' = Bob
//
//		Big step challenge:
//
//			      /--5---6-----8-----------16 = Alice (claim_id = id(5, 6) in the level above)
//			0-----4
//			      \--5'--6'----8'----------16' = Bob
//
//	 Small step challenge:
//
//			      /--5---6-----8-----------16 = Alice (claim_id = id(5, 6) in the level above)
//			0-----4
//			      \--5'--6'----8'----------16' = Bob
//
// From here, the list of ancestors can be determined all the way to the top.
func TestAncestors_AllChallengeLevels(t *testing.T) {
	tree := &HonestChallengeTree{
		edges:     threadsafe.NewMap[protocol.EdgeId, protocol.EdgeSnapshot](),
		mutualIds: threadsafe.NewMap[protocol.MutualId, *threadsafe.Map[protocol.EdgeId, creationTime]](),
	}
	// Edge ids that belong to block challenges are prefixed with "blk".
	// For big step, prefixed with "big", and small step, prefixed with "smol".
	setupBlockChallengeTreeSnapshot(t, tree)
	tree.honestBlockChalLevelZeroEdge = util.Some(tree.edges.Get(id("blk-0.a-16.a")))
	claimId := "blk-4.a-5.a"
	setupBigStepChallengeSnapshot(t, tree, claimId)
	tree.honestBigStepChalLevelZeroEdge = util.Some(tree.edges.Get(id("big-0.a-16.a")))
	claimId = "big-4.a-5.a"
	setupSmallStepChallengeSnapshot(t, tree, claimId)
	tree.honestSmallStepChalLevelZeroEdge = util.Some(tree.edges.Get(id("smol-0.a-16.a")))

	t.Run("junk edge fails", func(t *testing.T) {
		// We start by querying for ancestors for a block edge id.
		_, err := tree.AncestorsForHonestEdge(id("foo"))
		require.ErrorContains(t, err, "not found in honest challenge tree")
	})
	t.Run("dishonest edge lookup fails", func(t *testing.T) {
		_, err := tree.AncestorsForHonestEdge(id("blk-0.a-16.b"))
		require.ErrorContains(t, err, "not found in honest challenge tree")
	})
	t.Run("block challenge: level zero edge has no ancestors", func(t *testing.T) {
		ancestors, err := tree.AncestorsForHonestEdge(id("blk-0.a-16.a"))
		require.NoError(t, err)
		require.Equal(t, 0, len(ancestors))
	})
	t.Run("block challenge: single ancestor", func(t *testing.T) {
		ancestors, err := tree.AncestorsForHonestEdge(id("blk-0.a-8.a"))
		require.NoError(t, err)
		require.Equal(t, []protocol.EdgeId{id("blk-0.a-16.a")}, ancestors)
		ancestors, err = tree.AncestorsForHonestEdge(id("blk-8.a-16.a"))
		require.NoError(t, err)
		require.Equal(t, []protocol.EdgeId{id("blk-0.a-16.a")}, ancestors)
	})
	t.Run("block challenge: many ancestors", func(t *testing.T) {
		ancestors, err := tree.AncestorsForHonestEdge(id("blk-4.a-5.a"))
		require.NoError(t, err)
		wanted := []protocol.EdgeId{
			id("blk-4.a-6.a"),
			id("blk-4.a-8.a"),
			id("blk-0.a-8.a"),
			id("blk-0.a-16.a"),
		}
		require.Equal(t, wanted, ancestors)
	})
	t.Run("big step challenge: level zero edge has ancestors from block challenge", func(t *testing.T) {
		ancestors, err := tree.AncestorsForHonestEdge(id("big-0.a-16.a"))
		require.NoError(t, err)
		wanted := []protocol.EdgeId{
			id("blk-4.a-5.a"),
			id("blk-4.a-6.a"),
			id("blk-4.a-8.a"),
			id("blk-0.a-8.a"),
			id("blk-0.a-16.a"),
		}
		require.Equal(t, wanted, ancestors)
	})
	t.Run("big step challenge: many ancestors plus block challenge ancestors", func(t *testing.T) {
		ancestors, err := tree.AncestorsForHonestEdge(id("big-5.a-6.a"))
		require.NoError(t, err)
		wanted := []protocol.EdgeId{
			// Big step chal.
			id("big-4.a-6.a"),
			id("big-4.a-8.a"),
			id("big-0.a-8.a"),
			id("big-0.a-16.a"),
			// Block chal.
			id("blk-4.a-5.a"),
			id("blk-4.a-6.a"),
			id("blk-4.a-8.a"),
			id("blk-0.a-8.a"),
			id("blk-0.a-16.a"),
		}
		require.Equal(t, wanted, ancestors)
	})
	t.Run("small step challenge: level zero edge has ancestors from big and block challenge", func(t *testing.T) {
		ancestors, err := tree.AncestorsForHonestEdge(id("smol-0.a-16.a"))
		require.NoError(t, err)
		wanted := []protocol.EdgeId{
			// Big step chal.
			id("big-4.a-5.a"),
			id("big-4.a-6.a"),
			id("big-4.a-8.a"),
			id("big-0.a-8.a"),
			id("big-0.a-16.a"),
			// Block chal.
			id("blk-4.a-5.a"),
			id("blk-4.a-6.a"),
			id("blk-4.a-8.a"),
			id("blk-0.a-8.a"),
			id("blk-0.a-16.a"),
		}
		require.Equal(t, wanted, ancestors)
	})
	t.Run("small step challenge: lowest level edge has full ancestry", func(t *testing.T) {
		ancestors, err := tree.AncestorsForHonestEdge(id("smol-5.a-6.a"))
		require.NoError(t, err)
		wanted := []protocol.EdgeId{
			// Small step chal.
			id("smol-4.a-6.a"),
			id("smol-4.a-8.a"),
			id("smol-0.a-8.a"),
			id("smol-0.a-16.a"),
			// Big step chal.
			id("big-4.a-5.a"),
			id("big-4.a-6.a"),
			id("big-4.a-8.a"),
			id("big-0.a-8.a"),
			id("big-0.a-16.a"),
			// Block chal.
			id("blk-4.a-5.a"),
			id("blk-4.a-6.a"),
			id("blk-4.a-8.a"),
			id("blk-0.a-8.a"),
			id("blk-0.a-16.a"),
		}
		require.Equal(t, wanted, ancestors)
	})
}

func TestHonestChallengeTree_isRivaled(t *testing.T) {
	ht := &HonestChallengeTree{
		mutualIds: threadsafe.NewMap[protocol.MutualId, *threadsafe.Map[protocol.EdgeId, creationTime]](),
	}
	edge := newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.a"})
	rival := newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.b"})
	t.Run("mutual id mapping empty", func(t *testing.T) {
		require.Equal(t, false, ht.isRivaled(edge))
	})
	ht.mutualIds.Put(edge.MutualId(), threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals := ht.mutualIds.Get(edge.MutualId())
	t.Run("mutual id only one item", func(t *testing.T) {
		mutuals.Put(edge.Id(), creationTime(edge.creationTime))
		require.Equal(t, false, ht.isRivaled(edge))
	})
	t.Run("mutual id contains two items and one of them is the specified edge", func(t *testing.T) {
		mutuals.Put(rival.Id(), creationTime(rival.creationTime))
		require.Equal(t, true, ht.isRivaled(edge))
		require.Equal(t, true, ht.isRivaled(rival))
	})
}

func Test_checkEdgeClaim(t *testing.T) {
	t.Run("no claim id", func(t *testing.T) {
		edge := newEdge(&newCfg{t: t, edgeId: "big-0.a-4.a", claimId: ""})
		ok := checkEdgeClaim(edge, protocol.ClaimId(id("blk-4.a-5.a")))
		require.Equal(t, false, ok)
	})
	t.Run("wrong claim id", func(t *testing.T) {
		edge := newEdge(&newCfg{t: t, edgeId: "big-0.a-4.a", claimId: "blk-5.a-6.a"})
		ok := checkEdgeClaim(edge, protocol.ClaimId(id("blk-4.a-5.a")))
		require.Equal(t, false, ok)
	})
	t.Run("OK", func(t *testing.T) {
		edge := newEdge(&newCfg{t: t, edgeId: "big-0.a-4.a", claimId: "blk-4.a-5.a"})
		ok := checkEdgeClaim(edge, protocol.ClaimId(id("blk-4.a-5.a")))
		require.Equal(t, true, ok)
	})
}

func Test_isDirectChild(t *testing.T) {
	t.Run("no children", func(t *testing.T) {
		child := newEdge(&newCfg{t: t, edgeId: "blk-2.a-4.a"})
		parent := newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.a"})
		parent.lowerChildId = ""
		parent.upperChildId = ""
		require.Equal(t, false, isDirectChild(parent, child.Id()))
	})
	t.Run("wrong children", func(t *testing.T) {
		child := newEdge(&newCfg{t: t, edgeId: "blk-2.b-4.b"})
		parent := newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.a"})
		parent.lowerChildId = "blk-0.a-2.a"
		parent.upperChildId = "blk-2.a-4.a"
		require.Equal(t, false, isDirectChild(parent, child.Id()))
	})
	t.Run("is lower", func(t *testing.T) {
		child := newEdge(&newCfg{t: t, edgeId: "blk-0.a-2.a"})
		parent := newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.a"})
		parent.lowerChildId = "blk-0.a-2.a"
		parent.upperChildId = "blk-2.a-4.a"
		require.Equal(t, true, isDirectChild(parent, child.Id()))
	})
	t.Run("is upper", func(t *testing.T) {
		child := newEdge(&newCfg{t: t, edgeId: "blk-2.a-4.a"})
		parent := newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.a"})
		parent.lowerChildId = "blk-0.a-2.a"
		parent.upperChildId = "blk-2.a-4.a"
		require.Equal(t, true, isDirectChild(parent, child.Id()))
	})
}

func Test_hasChildren(t *testing.T) {
	t.Run("no children", func(t *testing.T) {
		edge := newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.a"})
		edge.lowerChildId = ""
		edge.upperChildId = ""
		require.Equal(t, false, hasChildren(edge))
	})
	t.Run("has upper", func(t *testing.T) {
		edge := newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.a"})
		edge.lowerChildId = ""
		edge.upperChildId = "blk-2.a-4.a"
		require.Equal(t, true, hasChildren(edge))
	})
	t.Run("has lower", func(t *testing.T) {
		edge := newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.a"})
		edge.lowerChildId = "blk-0.a-2.a"
		edge.upperChildId = ""
		require.Equal(t, true, hasChildren(edge))
	})
	t.Run("has both", func(t *testing.T) {
		edge := newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.a"})
		edge.lowerChildId = "blk-0.a-2.a"
		edge.upperChildId = "blk-2.a-4.a"
		require.Equal(t, true, hasChildren(edge))
	})
}

func buildEdges(allEdges ...*edge) map[edgeId]*edge {
	m := make(map[edgeId]*edge)
	for _, e := range allEdges {
		m[e.id] = e
	}
	return m
}

// Sets up the following block challenge snapshot:
//
//	      /--5---6-----8-----------16 = Alice
//	0-----4
//	      \--5'--6'----8'----------16' = Bob
//
// and then inserts the respective edges into a challenge tree.
func setupBlockChallengeTreeSnapshot(t *testing.T, tree *HonestChallengeTree) {
	t.Helper()
	aliceEdges := buildEdges(
		// Alice.
		newEdge(&newCfg{t: t, edgeId: "blk-0.a-16.a", createdAt: 1}),
		newEdge(&newCfg{t: t, edgeId: "blk-0.a-8.a", createdAt: 3}),
		newEdge(&newCfg{t: t, edgeId: "blk-8.a-16.a", createdAt: 3}),
		newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.a", createdAt: 5}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-8.a", createdAt: 5}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-6.a", createdAt: 7}),
		newEdge(&newCfg{t: t, edgeId: "blk-6.a-8.a", createdAt: 7}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-5.a", createdAt: 9}),
		newEdge(&newCfg{t: t, edgeId: "blk-5.a-6.a", createdAt: 9}),
	)
	bobEdges := buildEdges(
		// Bob.
		newEdge(&newCfg{t: t, edgeId: "blk-0.a-16.b", createdAt: 2}),
		newEdge(&newCfg{t: t, edgeId: "blk-0.a-8.b", createdAt: 4}),
		newEdge(&newCfg{t: t, edgeId: "blk-8.b-16.b", createdAt: 4}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-8.b", createdAt: 6}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-6.b", createdAt: 6}),
		newEdge(&newCfg{t: t, edgeId: "blk-6.b-8.b", createdAt: 8}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-5.b", createdAt: 10}),
		newEdge(&newCfg{t: t, edgeId: "blk-5.b-6.b", createdAt: 10}),
	)
	// Child-relationship linking.
	// Alice.
	aliceEdges["blk-0.a-16.a"].lowerChildId = "blk-0.a-8.a"
	aliceEdges["blk-0.a-16.a"].upperChildId = "blk-8.a-16.a"
	aliceEdges["blk-0.a-8.a"].lowerChildId = "blk-0.a-4.a"
	aliceEdges["blk-0.a-8.a"].upperChildId = "blk-4.a-8.a"
	aliceEdges["blk-4.a-8.a"].lowerChildId = "blk-4.a-6.a"
	aliceEdges["blk-4.a-8.a"].upperChildId = "blk-6.a-8.a"
	aliceEdges["blk-4.a-6.a"].lowerChildId = "blk-4.a-5.a"
	aliceEdges["blk-4.a-6.a"].upperChildId = "blk-5.a-6.a"
	// Bob.
	bobEdges["blk-0.a-16.b"].lowerChildId = "blk-0.a-8.b"
	bobEdges["blk-0.a-16.b"].upperChildId = "blk-8.b-16.b"
	bobEdges["blk-0.a-8.b"].lowerChildId = "blk-0.a-4.a"
	bobEdges["blk-0.a-8.b"].upperChildId = "blk-4.a-8.b"
	bobEdges["blk-4.a-8.b"].lowerChildId = "blk-4.a-6.b"
	bobEdges["blk-4.a-8.b"].upperChildId = "blk-6.b-6.8"
	bobEdges["blk-4.a-6.b"].lowerChildId = "blk-4.a-5.b"
	bobEdges["blk-4.a-6.b"].upperChildId = "blk-5.b-6.b"

	transformedEdges := make(map[protocol.EdgeId]protocol.EdgeSnapshot)
	for _, v := range aliceEdges {
		transformedEdges[v.Id()] = v
		tree.cumulativeHonestPathTimers.Put(v.Id(), v.creationTime)
	}
	for _, v := range bobEdges {
		transformedEdges[v.Id()] = v
		tree.cumulativeHonestPathTimers.Put(v.Id(), v.creationTime)
	}
	allEdges := threadsafe.NewMapFromItems(transformedEdges)
	tree.edges = allEdges

	// Set up rivaled edges.
	mutual := aliceEdges["blk-0.a-16.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals := tree.mutualIds.Get(mutual)
	aId := id("blk-0.a-16.a")
	bId := id("blk-0.a-16.b")
	a := tree.edges.Get(aId)
	b := tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["blk-0.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("blk-0.a-8.a")
	bId = id("blk-0.a-8.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["blk-4.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("blk-4.a-8.a")
	bId = id("blk-4.a-8.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["blk-4.a-6.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("blk-4.a-6.a")
	bId = id("blk-4.a-6.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["blk-4.a-5.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("blk-4.a-5.a")
	bId = id("blk-4.a-5.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))
}

func id(eId edgeId) protocol.EdgeId {
	return protocol.EdgeId(common.BytesToHash([]byte(eId)))
}

// Sets up the following big step challenge snapshot:
//
//	      /--5---6-----8-----------16 = Alice
//	0-----4
//	      \--5'--6'----8'----------16' = Bob
//
// and then inserts the respective edges into a challenge tree.
func setupBigStepChallengeSnapshot(t *testing.T, tree *HonestChallengeTree, claimId string) {
	t.Helper()
	aliceEdges := buildEdges(
		// Alice.
		newEdge(&newCfg{t: t, edgeId: "big-0.a-16.a", claimId: claimId, createdAt: 11}),
		newEdge(&newCfg{t: t, edgeId: "big-0.a-8.a", createdAt: 13}),
		newEdge(&newCfg{t: t, edgeId: "big-8.a-16.a", createdAt: 13}),
		newEdge(&newCfg{t: t, edgeId: "big-0.a-4.a", createdAt: 15}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-8.a", createdAt: 15}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-6.a", createdAt: 17}),
		newEdge(&newCfg{t: t, edgeId: "big-6.a-8.a", createdAt: 17}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-5.a", createdAt: 19}),
		newEdge(&newCfg{t: t, edgeId: "big-5.a-6.a", createdAt: 19}),
	)
	bobEdges := buildEdges(
		// Bob.
		newEdge(&newCfg{t: t, edgeId: "big-0.a-16.b", createdAt: 12}),
		newEdge(&newCfg{t: t, edgeId: "big-0.a-8.b", createdAt: 14}),
		newEdge(&newCfg{t: t, edgeId: "big-8.b-16.b", createdAt: 14}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-8.b", createdAt: 16}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-6.b", createdAt: 18}),
		newEdge(&newCfg{t: t, edgeId: "big-6.b-8.b", createdAt: 18}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-5.b", createdAt: 20}),
		newEdge(&newCfg{t: t, edgeId: "big-5.b-6.b", createdAt: 20}),
	)
	// Child-relationship linking.
	// Alice.
	aliceEdges["big-0.a-16.a"].lowerChildId = "big-0.a-8.a"
	aliceEdges["big-0.a-16.a"].upperChildId = "big-8.a-16.a"
	aliceEdges["big-0.a-8.a"].lowerChildId = "big-0.a-4.a"
	aliceEdges["big-0.a-8.a"].upperChildId = "big-4.a-8.a"
	aliceEdges["big-4.a-8.a"].lowerChildId = "big-4.a-6.a"
	aliceEdges["big-4.a-8.a"].upperChildId = "big-6.a-8.a"
	aliceEdges["big-4.a-6.a"].lowerChildId = "big-4.a-5.a"
	aliceEdges["big-4.a-6.a"].upperChildId = "big-5.a-6.a"
	// Bob.
	bobEdges["big-0.a-16.b"].lowerChildId = "big-0.a-8.b"
	bobEdges["big-0.a-16.b"].upperChildId = "big-8.b-16.b"
	bobEdges["big-0.a-8.b"].lowerChildId = "big-0.a-4.a"
	bobEdges["big-0.a-8.b"].upperChildId = "big-4.a-8.b"
	bobEdges["big-4.a-8.b"].lowerChildId = "big-4.a-6.b"
	bobEdges["big-4.a-8.b"].upperChildId = "big-6.b-6.8"
	bobEdges["big-4.a-6.b"].lowerChildId = "big-4.a-5.b"
	bobEdges["big-4.a-6.b"].upperChildId = "big-5.b-6.b"

	for _, v := range aliceEdges {
		tree.edges.Put(v.Id(), v)
		tree.cumulativeHonestPathTimers.Put(v.Id(), v.creationTime)
	}
	for _, v := range bobEdges {
		tree.edges.Put(v.Id(), v)
		tree.cumulativeHonestPathTimers.Put(v.Id(), v.creationTime)
	}

	// Set up rivaled edges.
	mutual := aliceEdges["big-0.a-16.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals := tree.mutualIds.Get(mutual)
	aId := id("big-0.a-16.a")
	bId := id("big-0.a-16.b")
	a := tree.edges.Get(aId)
	b := tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["big-0.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("big-0.a-8.a")
	bId = id("big-0.a-8.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["big-4.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("big-4.a-8.a")
	bId = id("big-4.a-8.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["big-4.a-6.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("big-4.a-6.a")
	bId = id("big-4.a-6.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["big-4.a-5.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("big-4.a-5.a")
	bId = id("big-4.a-5.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))
}

// Sets up the following small step challenge snapshot:
//
//	      /--5---6-----8-----------16 = Alice
//	0-----4
//	      \--5'--6'----8'----------16' = Bob
//
// and then inserts the respective edges into a challenge tree.
//
// and then inserts the respective edges into a challenge tree.
func setupSmallStepChallengeSnapshot(t *testing.T, tree *HonestChallengeTree, claimId string) {
	t.Helper()
	aliceEdges := buildEdges(
		// Alice.
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-16.a", claimId: claimId, createdAt: 21}),
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-8.a", createdAt: 23}),
		newEdge(&newCfg{t: t, edgeId: "smol-8.a-16.a", createdAt: 23}),
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-4.a", createdAt: 25}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-8.a", createdAt: 25}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-6.a", createdAt: 27}),
		newEdge(&newCfg{t: t, edgeId: "smol-6.a-8.a", createdAt: 27}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-5.a", createdAt: 29}),
		newEdge(&newCfg{t: t, edgeId: "smol-5.a-6.a", createdAt: 29}),
	)
	bobEdges := buildEdges(
		// Bob.
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-16.b", createdAt: 22}),
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-8.b", createdAt: 24}),
		newEdge(&newCfg{t: t, edgeId: "smol-8.b-16.b", createdAt: 24}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-8.b", createdAt: 26}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-6.b", createdAt: 28}),
		newEdge(&newCfg{t: t, edgeId: "smol-6.b-8.b", createdAt: 28}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-5.b", createdAt: 30}),
		newEdge(&newCfg{t: t, edgeId: "smol-5.b-6.b", createdAt: 30}),
	)
	// Child-relationship linking.
	// Alice.
	aliceEdges["smol-0.a-16.a"].lowerChildId = "smol-0.a-8.a"
	aliceEdges["smol-0.a-16.a"].upperChildId = "smol-8.a-16.a"
	aliceEdges["smol-0.a-8.a"].lowerChildId = "smol-0.a-4.a"
	aliceEdges["smol-0.a-8.a"].upperChildId = "smol-4.a-8.a"
	aliceEdges["smol-4.a-8.a"].lowerChildId = "smol-4.a-6.a"
	aliceEdges["smol-4.a-8.a"].upperChildId = "smol-6.a-8.a"
	aliceEdges["smol-4.a-6.a"].lowerChildId = "smol-4.a-5.a"
	aliceEdges["smol-4.a-6.a"].upperChildId = "smol-5.a-6.a"
	// Bob.
	bobEdges["smol-0.a-16.b"].lowerChildId = "smol-0.a-8.b"
	bobEdges["smol-0.a-16.b"].upperChildId = "smol-8.b-16.b"
	bobEdges["smol-0.a-8.b"].lowerChildId = "smol-0.a-4.a"
	bobEdges["smol-0.a-8.b"].upperChildId = "smol-4.a-8.b"
	bobEdges["smol-4.a-8.b"].lowerChildId = "smol-4.a-6.b"
	bobEdges["smol-4.a-8.b"].upperChildId = "smol-6.b-6.8"
	bobEdges["smol-4.a-6.b"].lowerChildId = "smol-4.a-5.b"
	bobEdges["smol-4.a-6.b"].upperChildId = "smol-5.b-6.b"

	for _, v := range aliceEdges {
		tree.edges.Put(v.Id(), v)
		tree.cumulativeHonestPathTimers.Put(v.Id(), v.creationTime)
	}
	for _, v := range bobEdges {
		tree.edges.Put(v.Id(), v)
		tree.cumulativeHonestPathTimers.Put(v.Id(), v.creationTime)
	}

	// Set up rivaled edges.
	mutual := aliceEdges["smol-0.a-16.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals := tree.mutualIds.Get(mutual)
	aId := id("smol-0.a-16.a")
	bId := id("smol-0.a-16.b")
	a := tree.edges.Get(aId)
	b := tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["smol-0.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("smol-0.a-8.a")
	bId = id("smol-0.a-8.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["smol-4.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("smol-4.a-8.a")
	bId = id("smol-4.a-8.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["smol-4.a-6.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("smol-4.a-6.a")
	bId = id("smol-4.a-6.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))

	mutual = aliceEdges["smol-4.a-5.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aId = id("smol-4.a-5.a")
	bId = id("smol-4.a-5.b")
	a = tree.edges.Get(aId)
	b = tree.edges.Get(bId)
	mutuals.Put(aId, creationTime(a.CreatedAtBlock()))
	mutuals.Put(bId, creationTime(b.CreatedAtBlock()))
}
