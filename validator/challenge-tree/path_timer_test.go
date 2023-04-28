package challengetree

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPathTimer(t *testing.T) {
	// Setup the following challenge tree, where
	// branch `a` is honest.
	//
	// 0-----4a----- 8a-------16a
	//        \------8b-------16b
	//
	// Here are the creation times of each edge:
	//
	//   Alice
	//     0-16a        = T1
	//     0-8a, 8a-16a = T2
	//     0-4a, 4a-8a  = T3
	//
	//   Bob
	//     0-16b        = T3
	//     0-8b, 8b-16b = T2
	//     4a-8b        = T3
	edges := buildEdges(
		// Alice.
		withCreationTime("0-16a", 1),
		withCreationTime("8a-16a", 2),
		withCreationTime("0-8a", 2),
		withCreationTime("4a-8a", 3),
		withCreationTime("0-4a", 3),
		// Bob.
		withCreationTime("0-16b", 1),
		withCreationTime("8b-16b", 2),
		withCreationTime("0-8b", 2),
		withCreationTime("4a-8b", 3),
	)
	// Alice.
	edges["0-16a"].lowerChild = "0-8a"
	edges["0-16a"].upperChild = "8a-16a"
	edges["0-8a"].lowerChild = "0-4a"
	edges["0-8a"].upperChild = "4a-8a"
	// Bob.
	edges["0-16b"].lowerChild = "0-8b"
	edges["0-16b"].upperChild = "8b-16b"
	edges["0-8b"].lowerChild = "0-4a"
	edges["0-8b"].upperChild = "4a-8b"

	h := &helper{
		edges: edges,
	}

	// Edge was not created at time 1 nor 2.
	total := h.pathTimer(h.edges["4a-8a"], 1)
	require.Equal(t, uint64(0), total)
	total = h.pathTimer(h.edges["4a-8a"], 2)
	require.Equal(t, uint64(0), total)
	total = h.pathTimer(h.edges["4a-8a"], 3)
	require.Equal(t, uint64(0), total)
}

func buildEdges(allEdges ...*edg) map[edgeId]*edg {
	m := make(map[edgeId]*edg)
	for _, e := range allEdges {
		m[e.id] = e
	}
	return m
}

func withCreationTime(id string, createdAt uint64) *edg {
	return &edg{
		id:           edgeId(id),
		mutualId:     id[:len(id)-1], // Strip off the last char.
		lowerChild:   "",
		upperChild:   "",
		creationTime: createdAt,
	}
}
