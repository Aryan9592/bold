// Copyright 2023, Offchain Labs, Inc.
// For license information, see https://github.com/offchainlabs/bold/blob/main/LICENSE

package challengetree

import (
	"testing"

	protocol "github.com/OffchainLabs/bold/chain-abstraction"
	"github.com/OffchainLabs/bold/challenge-manager/challenge-tree/mock"
	"github.com/OffchainLabs/bold/containers/threadsafe"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func Test_findOriginEdge(t *testing.T) {
	edges := threadsafe.NewSlice[protocol.ReadOnlyEdge]()
	origin := protocol.OriginId(common.BytesToHash([]byte("foo")))
	_, ok := findOriginEdge(origin, edges)
	require.Equal(t, false, ok)
	edges.Push(newEdge(&newCfg{
		t:         t,
		originId:  "bar",
		edgeId:    "blk-0.a-4.a",
		claimId:   "",
		createdAt: 2,
	}))

	_, ok = findOriginEdge(origin, edges)
	require.Equal(t, false, ok)

	origin = protocol.OriginId(common.BytesToHash([]byte("bar")))
	got, ok := findOriginEdge(origin, edges)
	require.Equal(t, true, ok)
	require.Equal(t, got.Id(), protocol.EdgeId{Hash: common.BytesToHash([]byte("blk-0.a-4.a"))})

	edges.Push(newEdge(&newCfg{
		t:         t,
		originId:  "baz",
		edgeId:    "blk-0.b-4.b",
		claimId:   "",
		createdAt: 2,
	}))

	origin = protocol.OriginId(common.BytesToHash([]byte("baz")))
	got, ok = findOriginEdge(origin, edges)
	require.Equal(t, true, ok)
	require.Equal(t, got.Id(), protocol.EdgeId{Hash: common.BytesToHash([]byte("blk-0.b-4.b"))})
}

func buildEdges(allEdges ...*mock.Edge) map[mock.EdgeId]*mock.Edge {
	m := make(map[mock.EdgeId]*mock.Edge)
	for _, e := range allEdges {
		m[e.ID] = e
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
	aliceEdges["blk-0.a-16.a"].LowerChildID = "blk-0.a-8.a"
	aliceEdges["blk-0.a-16.a"].UpperChildID = "blk-8.a-16.a"
	aliceEdges["blk-0.a-8.a"].LowerChildID = "blk-0.a-4.a"
	aliceEdges["blk-0.a-8.a"].UpperChildID = "blk-4.a-8.a"
	aliceEdges["blk-4.a-8.a"].LowerChildID = "blk-4.a-6.a"
	aliceEdges["blk-4.a-8.a"].UpperChildID = "blk-6.a-8.a"
	aliceEdges["blk-4.a-6.a"].LowerChildID = "blk-4.a-5.a"
	aliceEdges["blk-4.a-6.a"].UpperChildID = "blk-5.a-6.a"
	// Bob.
	bobEdges["blk-0.a-16.b"].LowerChildID = "blk-0.a-8.b"
	bobEdges["blk-0.a-16.b"].UpperChildID = "blk-8.b-16.b"
	bobEdges["blk-0.a-8.b"].LowerChildID = "blk-0.a-4.a"
	bobEdges["blk-0.a-8.b"].UpperChildID = "blk-4.a-8.b"
	bobEdges["blk-4.a-8.b"].LowerChildID = "blk-4.a-6.b"
	bobEdges["blk-4.a-8.b"].UpperChildID = "blk-6.b-6.8"
	bobEdges["blk-4.a-6.b"].LowerChildID = "blk-4.a-5.b"
	bobEdges["blk-4.a-6.b"].UpperChildID = "blk-5.b-6.b"

	transformedEdges := make(map[protocol.EdgeId]protocol.SpecEdge)
	for _, v := range aliceEdges {
		transformedEdges[v.Id()] = v
	}
	allEdges := threadsafe.NewMapFromItems(transformedEdges)
	tree.edges = allEdges

	// Set up rivaled edges.
	mutual := aliceEdges["blk-0.a-16.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals := tree.mutualIds.Get(mutual)
	a := aliceEdges["blk-0.a-16.a"]
	b := bobEdges["blk-0.a-16.b"]
	aCreation, err := a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err := b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["blk-0.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	a = aliceEdges["blk-0.a-8.a"]
	b = bobEdges["blk-0.a-8.b"]
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["blk-4.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	a = aliceEdges["blk-4.a-8.a"]
	b = bobEdges["blk-4.a-8.b"]
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["blk-4.a-6.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["blk-4.a-5.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	a = aliceEdges["blk-4.a-5.a"]
	b = bobEdges["blk-4.a-5.b"]
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))
}

func id(eId mock.EdgeId) protocol.EdgeId {
	return protocol.EdgeId{Hash: common.BytesToHash([]byte(eId))}
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
	originEdge, ok := tree.edges.Get(id(mock.EdgeId(claimId))).(*mock.Edge)
	require.Equal(t, true, ok)
	origin := mock.OriginId(originEdge.ComputeMutualId())
	aliceEdges := buildEdges(
		// Alice.
		newEdge(&newCfg{t: t, edgeId: "big-0.a-16.a", originId: origin, claimId: claimId, createdAt: 11}),
		newEdge(&newCfg{t: t, edgeId: "big-0.a-8.a", originId: origin, createdAt: 13}),
		newEdge(&newCfg{t: t, edgeId: "big-8.a-16.a", originId: origin, createdAt: 13}),
		newEdge(&newCfg{t: t, edgeId: "big-0.a-4.a", originId: origin, createdAt: 15}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-8.a", originId: origin, createdAt: 15}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-6.a", originId: origin, createdAt: 17}),
		newEdge(&newCfg{t: t, edgeId: "big-6.a-8.a", originId: origin, createdAt: 17}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-5.a", originId: origin, createdAt: 19}),
		newEdge(&newCfg{t: t, edgeId: "big-5.a-6.a", originId: origin, createdAt: 19}),
	)
	bobEdges := buildEdges(
		// Bob.
		newEdge(&newCfg{t: t, edgeId: "big-0.a-16.b", originId: origin, createdAt: 12}),
		newEdge(&newCfg{t: t, edgeId: "big-0.a-8.b", originId: origin, createdAt: 14}),
		newEdge(&newCfg{t: t, edgeId: "big-8.b-16.b", originId: origin, createdAt: 14}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-8.b", originId: origin, createdAt: 16}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-6.b", originId: origin, createdAt: 18}),
		newEdge(&newCfg{t: t, edgeId: "big-6.b-8.b", originId: origin, createdAt: 18}),
		newEdge(&newCfg{t: t, edgeId: "big-4.a-5.b", originId: origin, createdAt: 20}),
		newEdge(&newCfg{t: t, edgeId: "big-5.b-6.b", originId: origin, createdAt: 20}),
	)
	// Child-relationship linking.
	// Alice.
	aliceEdges["big-0.a-16.a"].LowerChildID = "big-0.a-8.a"
	aliceEdges["big-0.a-16.a"].UpperChildID = "big-8.a-16.a"
	aliceEdges["big-0.a-8.a"].LowerChildID = "big-0.a-4.a"
	aliceEdges["big-0.a-8.a"].UpperChildID = "big-4.a-8.a"
	aliceEdges["big-4.a-8.a"].LowerChildID = "big-4.a-6.a"
	aliceEdges["big-4.a-8.a"].UpperChildID = "big-6.a-8.a"
	aliceEdges["big-4.a-6.a"].LowerChildID = "big-4.a-5.a"
	aliceEdges["big-4.a-6.a"].UpperChildID = "big-5.a-6.a"
	// Bob.
	bobEdges["big-0.a-16.b"].LowerChildID = "big-0.a-8.b"
	bobEdges["big-0.a-16.b"].UpperChildID = "big-8.b-16.b"
	bobEdges["big-0.a-8.b"].LowerChildID = "big-0.a-4.a"
	bobEdges["big-0.a-8.b"].UpperChildID = "big-4.a-8.b"
	bobEdges["big-4.a-8.b"].LowerChildID = "big-4.a-6.b"
	bobEdges["big-4.a-8.b"].UpperChildID = "big-6.b-6.8"
	bobEdges["big-4.a-6.b"].LowerChildID = "big-4.a-5.b"
	bobEdges["big-4.a-6.b"].UpperChildID = "big-5.b-6.b"

	for _, v := range aliceEdges {
		tree.edges.Put(v.Id(), v)
	}

	// Set up rivaled edges.
	mutual := aliceEdges["big-0.a-16.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals := tree.mutualIds.Get(mutual)
	a := aliceEdges["big-0.a-16.a"]
	b := bobEdges["big-0.a-16.b"]
	aCreation, err := a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err := b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["big-0.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	a = aliceEdges["big-0.a-8.a"]
	b = bobEdges["big-0.a-8.b"]
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["big-4.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	a = aliceEdges["big-4.a-8.a"]
	b = bobEdges["big-4.a-8.b"]
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["big-4.a-6.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	a = aliceEdges["big-4.a-6.a"]
	b = bobEdges["big-4.a-6.b"]
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["big-4.a-5.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	a = aliceEdges["big-4.a-5.a"]
	b = bobEdges["big-4.a-5.b"]
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))
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
	originEdge, ok := tree.edges.Get(id(mock.EdgeId(claimId))).(*mock.Edge)
	require.Equal(t, true, ok)
	origin := mock.OriginId(originEdge.ComputeMutualId())
	aliceEdges := buildEdges(
		// Alice.
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-16.a", originId: origin, claimId: claimId, createdAt: 21}),
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-8.a", originId: origin, createdAt: 23}),
		newEdge(&newCfg{t: t, edgeId: "smol-8.a-16.a", originId: origin, createdAt: 23}),
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-4.a", originId: origin, createdAt: 25}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-8.a", originId: origin, createdAt: 25}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-6.a", originId: origin, createdAt: 27}),
		newEdge(&newCfg{t: t, edgeId: "smol-6.a-8.a", originId: origin, createdAt: 27}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-5.a", originId: origin, createdAt: 29}),
		newEdge(&newCfg{t: t, edgeId: "smol-5.a-6.a", originId: origin, createdAt: 29}),
	)
	bobEdges := buildEdges(
		// Bob.
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-16.b", originId: origin, createdAt: 22}),
		newEdge(&newCfg{t: t, edgeId: "smol-0.a-8.b", originId: origin, createdAt: 24}),
		newEdge(&newCfg{t: t, edgeId: "smol-8.b-16.b", originId: origin, createdAt: 24}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-8.b", originId: origin, createdAt: 26}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-6.b", originId: origin, createdAt: 28}),
		newEdge(&newCfg{t: t, edgeId: "smol-6.b-8.b", originId: origin, createdAt: 28}),
		newEdge(&newCfg{t: t, edgeId: "smol-4.a-5.b", originId: origin, createdAt: 30}),
		newEdge(&newCfg{t: t, edgeId: "smol-5.b-6.b", originId: origin, createdAt: 30}),
	)
	// Child-relationship linking.
	// Alice.
	aliceEdges["smol-0.a-16.a"].LowerChildID = "smol-0.a-8.a"
	aliceEdges["smol-0.a-16.a"].UpperChildID = "smol-8.a-16.a"
	aliceEdges["smol-0.a-8.a"].LowerChildID = "smol-0.a-4.a"
	aliceEdges["smol-0.a-8.a"].UpperChildID = "smol-4.a-8.a"
	aliceEdges["smol-4.a-8.a"].LowerChildID = "smol-4.a-6.a"
	aliceEdges["smol-4.a-8.a"].UpperChildID = "smol-6.a-8.a"
	aliceEdges["smol-4.a-6.a"].LowerChildID = "smol-4.a-5.a"
	aliceEdges["smol-4.a-6.a"].UpperChildID = "smol-5.a-6.a"
	// Bob.
	bobEdges["smol-0.a-16.b"].LowerChildID = "smol-0.a-8.b"
	bobEdges["smol-0.a-16.b"].UpperChildID = "smol-8.b-16.b"
	bobEdges["smol-0.a-8.b"].LowerChildID = "smol-0.a-4.a"
	bobEdges["smol-0.a-8.b"].UpperChildID = "smol-4.a-8.b"
	bobEdges["smol-4.a-8.b"].LowerChildID = "smol-4.a-6.b"
	bobEdges["smol-4.a-8.b"].UpperChildID = "smol-6.b-6.8"
	bobEdges["smol-4.a-6.b"].LowerChildID = "smol-4.a-5.b"
	bobEdges["smol-4.a-6.b"].UpperChildID = "smol-5.b-6.b"

	for _, v := range aliceEdges {
		tree.edges.Put(v.Id(), v)
	}

	// Set up rivaled edges.
	mutual := aliceEdges["smol-0.a-16.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals := tree.mutualIds.Get(mutual)
	a := aliceEdges["smol-0.a-16.a"]
	b := bobEdges["smol-0.a-16.b"]
	aCreation, err := a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err := b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["smol-0.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	a = aliceEdges["smol-0.a-8.a"]
	b = bobEdges["smol-0.a-8.b"]
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["smol-4.a-8.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	a = aliceEdges["smol-4.a-8.a"]
	b = bobEdges["smol-4.a-8.b"]
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["smol-4.a-6.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	a = aliceEdges["smol-4.a-6.a"]
	b = bobEdges["smol-4.a-6.b"]
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))

	mutual = aliceEdges["smol-4.a-5.a"].MutualId()
	tree.mutualIds.Put(mutual, threadsafe.NewMap[protocol.EdgeId, creationTime]())
	mutuals = tree.mutualIds.Get(mutual)
	a = aliceEdges["smol-4.a-5.a"]
	b = bobEdges["smol-4.a-5.b"]
	aCreation, err = a.CreatedAtBlock()
	require.NoError(t, err)
	bCreation, err = b.CreatedAtBlock()
	require.NoError(t, err)
	mutuals.Put(a.Id(), creationTime(aCreation))
	mutuals.Put(b.Id(), creationTime(bCreation))
}
