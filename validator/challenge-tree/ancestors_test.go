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
		mutualIds: threadsafe.NewMap[protocol.MutualId, *threadsafe.Set[protocol.EdgeId]](),
	}
	setupBlockChallengeTreeSnapshot(t, tree)
	tree.honestBlockChalLevelZeroEdge = util.Some(tree.edges.Get(id("blk-0.a-16.a")))
	claimId := "blk-5.a-6.a"
	setupBigStepChallengeSnapshot(t, tree, claimId)
	tree.honestBigStepChalLevelZeroEdge = util.Some(tree.edges.Get(id("big-0.a-16.a")))
	claimId = "smol-5.a-6.a"
	setupSmallStepChallengeSnapshot(t, tree, claimId)
	tree.honestBigStepChalLevelZeroEdge = util.Some(tree.edges.Get(id("smol-0.a-16.a")))

	t.Run("junk edge fails", func(t *testing.T) {
		// We start by querying for ancestors for a block edge id.
		_, err := tree.AncestorsForHonestEdge(id("foo"))
		require.ErrorContains(t, err, "not found in honest challenge tree")
	})
	t.Run("dishonest edge lookup fails", func(t *testing.T) {
		_, err := tree.AncestorsForHonestEdge(id("blk-0.a-16.b"))
		require.ErrorContains(t, err, "not found in honest challenge tree")
	})

	// Edge ids that belong to block challenges are prefixed with "blk".
	// For big step, prefixed with "big", and small step, prefixed with "smol".
	t.Run("block challenge level zero edge has no ancestors", func(t *testing.T) {
		ancestors, err := tree.AncestorsForHonestEdge(id("blk-0.a-16.a"))
		require.NoError(t, err)
		require.Equal(t, 0, len(ancestors))
	})

	ancestors, err := tree.AncestorsForHonestEdge(id("blk-4.a-5.a"))
	require.NoError(t, err)
	wanted := []protocol.EdgeId{
		id("blk-4.a-6.a"),
		id("blk-4.a-8.a"),
		id("blk-0.a-8.a"),
		id("blk-0.a-16.a"),
	}
	require.Equal(t, wanted, ancestors)

	// // We start query the ancestors of the lowest level, length one, small step edge.
	// ancestors = tree.ancestorsForHonestEdge(id("smol-5-6"))
	// wanted = []protocol.EdgeId{
	// 	id("smol-4-6"),
	// 	id("smol-4-8"),
	// 	id("smol-0-8"),
	// 	id("smol-0-16"),
	// 	id("big-5-6"),
	// 	id("big-4-6"),
	// 	id("big-4-8"),
	// 	id("big-0-8"),
	// 	id("big-0-16"),
	// 	id("blk-5-6"), // TODO: Should the claim id be part of the ancestors as well?
	// 	id("blk-4-6"),
	// 	id("blk-4-8"),
	// 	id("blk-0-8"),
	// 	id("blk-0-16"),
	// }
	// require.Equal(t, wanted, ancestors)

	// // Query the level zero edge at each challenge type.
	// ancestors = tree.ancestorsForHonestEdge(id("blk-0-16"))
	// require.Equal(t, 0, len(ancestors))

	// ancestors = tree.ancestorsForHonestEdge(id("big-0-16"))
	// require.Equal(t, ancestors, []protocol.EdgeId{
	// 	id("blk-5-6"),
	// 	id("blk-4-6"),
	// 	id("blk-4-8"),
	// 	id("blk-0-8"),
	// 	id("blk-0-16"),
	// })

	// ancestors = tree.ancestorsForHonestEdge(id("smol-0-16"))
	// require.Equal(t, ancestors, []protocol.EdgeId{
	// 	id("big-5-6"),
	// 	id("big-4-6"),
	// 	id("big-4-8"),
	// 	id("big-0-8"),
	// 	id("big-0-16"),
	// 	id("blk-5-6"),
	// 	id("blk-4-6"),
	// 	id("blk-4-8"),
	// 	id("blk-0-8"),
	// 	id("blk-0-16"),
	// })
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
		newEdge(&newCfg{t: t, edgeId: "blk-0.a-16.a"}),
		newEdge(&newCfg{t: t, edgeId: "blk-0.a-8.a"}),
		newEdge(&newCfg{t: t, edgeId: "blk-8.a-16.a"}),
		newEdge(&newCfg{t: t, edgeId: "blk-0.a-4.a"}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-8.a"}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-6.a"}),
		newEdge(&newCfg{t: t, edgeId: "blk-6.a-8.a"}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-5.a"}),
		newEdge(&newCfg{t: t, edgeId: "blk-5.a-6.a"}),
	)
	bobEdges := buildEdges(
		// Bob.
		newEdge(&newCfg{t: t, edgeId: "blk-0.a-16.b"}),
		newEdge(&newCfg{t: t, edgeId: "blk-0.a-8.b"}),
		newEdge(&newCfg{t: t, edgeId: "blk-8.b-16.b"}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-8.b"}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-6.b"}),
		newEdge(&newCfg{t: t, edgeId: "blk-6.b-8.b"}),
		newEdge(&newCfg{t: t, edgeId: "blk-4.a-5.b"}),
		newEdge(&newCfg{t: t, edgeId: "blk-5.b-6.b"}),
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
	}
	allEdges := threadsafe.NewMapFromItems(transformedEdges)
	tree.edges = allEdges

	// Set up rivaled edges.
	mutual := aliceEdges["blk-0.a-16.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewSet[protocol.EdgeId]())
	mutuals := tree.mutualIds.Get(mutual)
	mutuals.Insert(id("blk-0.a-16.a"))
	mutuals.Insert(id("blk-0.a-16.b"))

	mutual = aliceEdges["blk-0.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewSet[protocol.EdgeId]())
	mutuals = tree.mutualIds.Get(mutual)
	mutuals.Insert(id("blk-0.a-8.a"))
	mutuals.Insert(id("blk-0.a-8.b"))

	mutual = aliceEdges["blk-4.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewSet[protocol.EdgeId]())
	mutuals = tree.mutualIds.Get(mutual)
	mutuals.Insert(id("blk-4.a-8.a"))
	mutuals.Insert(id("blk-4.a-8.b"))
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
		newEdge(&newCfg{t: t, edgeId: "big-0.a-16.a", claimId: claimId}),
		newEdge(&newCfg{t: t, edgeId: "big-0.a-8.a"}),
		newEdge(&newCfg{t: t, edgeId: "big-8.a-16.a"}),
		newEdge(&newCfg{t: t, edgeId: "big-0.a-4.a"}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-8.a"}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-6.a"}),
		newEdge(&newCfg{t: t, edgeId: "big-6.a-8.a"}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-5.a"}),
		newEdge(&newCfg{t: t, edgeId: "big-5.a-6.a"}),
	)
	bobEdges := buildEdges(
		// Bob.
		newEdge(&newCfg{t: t, edgeId: "big-0.a-16.b"}),
		newEdge(&newCfg{t: t, edgeId: "big-0.a-8.b"}),
		newEdge(&newCfg{t: t, edgeId: "big-8.b-16.b"}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-8.b"}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-6.b"}),
		newEdge(&newCfg{t: t, edgeId: "big-6.b-8.b"}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-5.b"}),
		newEdge(&newCfg{t: t, edgeId: "big-5.b-6.b"}),
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
	}

	// Set up rivaled edges.
	mutual := aliceEdges["big-0.a-16.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewSet[protocol.EdgeId]())
	mutuals := tree.mutualIds.Get(mutual)
	mutuals.Insert(id("big-0.a-16.a"))
	mutuals.Insert(id("big-0.a-16.b"))

	mutual = aliceEdges["big-0.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewSet[protocol.EdgeId]())
	mutuals = tree.mutualIds.Get(mutual)
	mutuals.Insert(id("big-0.a-8.a"))
	mutuals.Insert(id("big-0.a-8.b"))

	mutual = aliceEdges["big-4.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewSet[protocol.EdgeId]())
	mutuals = tree.mutualIds.Get(mutual)
	mutuals.Insert(id("big-4.a-8.a"))
	mutuals.Insert(id("big-4.a-8.b"))
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
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-16.a", claimId: claimId}),
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-8.a"}),
		newEdge(&newCfg{t: t, edgeId: "smol-8.a-16.a"}),
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-4.a"}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-8.a"}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-6.a"}),
		newEdge(&newCfg{t: t, edgeId: "smol-6.a-8.a"}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-5.a"}),
		newEdge(&newCfg{t: t, edgeId: "smol-5.a-6.a"}),
	)
	bobEdges := buildEdges(
		// Bob.
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-16.b"}),
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-8.b"}),
		newEdge(&newCfg{t: t, edgeId: "smol-8.b-16.b"}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-8.b"}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-6.b"}),
		newEdge(&newCfg{t: t, edgeId: "smol-6.b-8.b"}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-5.b"}),
		newEdge(&newCfg{t: t, edgeId: "smol-5.b-6.b"}),
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
	}

	// Set up rivaled edges.
	mutual := aliceEdges["smol-0.a-16.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewSet[protocol.EdgeId]())
	mutuals := tree.mutualIds.Get(mutual)
	mutuals.Insert(id("smol-0.a-16.a"))
	mutuals.Insert(id("smol-0.a-16.b"))

	mutual = aliceEdges["smol-0.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewSet[protocol.EdgeId]())
	mutuals = tree.mutualIds.Get(mutual)
	mutuals.Insert(id("smol-0.a-8.a"))
	mutuals.Insert(id("smol-0.a-8.b"))

	mutual = aliceEdges["smol-4.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewSet[protocol.EdgeId]())
	mutuals = tree.mutualIds.Get(mutual)
	mutuals.Insert(id("smol-4.a-8.a"))
	mutuals.Insert(id("smol-4.a-8.b"))
}
