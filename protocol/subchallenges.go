package protocol

import (
	"fmt"

	"github.com/OffchainLabs/challenge-protocol-v2/util"
	"github.com/pkg/errors"
)

var (
	ErrWrongChallengeKind        = errors.New("wrong top-level kind for subchallenge creation")
	ErrNoChallenge               = errors.New("no challenge corresponds to vertex")
	ErrChallengeNotRunning       = errors.New("challenge is not ongoing")
	ErrSubchallengeAlreadyExists = errors.New("subchallenge already exists on vertex")
	ErrNotEnoughValidChildren    = errors.New("vertex needs at least two unexpired children")
)

// CreateBigStepChallenge creates a BigStep subchallenge on a vertex.
func (v *ChallengeVertex) CreateBigStepChallenge(tx *ActiveTx) error {
	tx.verifyReadWrite()
	if err := v.canCreateSubChallenge(BigStepChallenge); err != nil {
		return err
	}
	// TODO: Add all other required challenge fields.
	v.SubChallenge = util.Some(&Challenge{
		// Set the creation time of the subchallenge to be
		// the same as the top-level challenge, as they should
		// expire at the same timestamp.
		creationTime: v.Challenge.Unwrap().creationTime,
		kind:         BigStepChallenge,
	})
	return nil
}

// CreateSmallStepChallenge creates a SmallStep subchallenge on a vertex.
func (v *ChallengeVertex) CreateSmallStepChallenge(tx *ActiveTx) error {
	tx.verifyReadWrite()
	if err := v.canCreateSubChallenge(SmallStepChallenge); err != nil {
		return err
	}
	// TODO: Add all other required challenge fields.
	v.SubChallenge = util.Some(&Challenge{
		creationTime: v.Challenge.Unwrap().creationTime,
		kind:         SmallStepChallenge,
	})
	return nil
}

// Verifies the a subchallenge can be created on a challenge vertex
// based on specification validity conditions below:
//
//	A subchallenge can be created at a vertex P in a “parent” BlockChallenge if:
//	  - P’s challenge has not reached its end time
//	  - P’s has at least two children with unexpired chess clocks
//	The end time of the new challenge is set equal to the end time of P’s challenge.
func (v *ChallengeVertex) canCreateSubChallenge(
	subChallengeKind ChallengeKind,
) error {
	if v.Challenge.IsNone() {
		return ErrNoChallenge
	}
	chal := v.Challenge.Unwrap()
	// Can only create a subchallenge if the vertex is
	// part of a challenge of a specified kind.
	switch subChallengeKind {
	case BlockChallenge:
		return ErrWrongChallengeKind
	case BigStepChallenge:
		if chal.kind != BlockChallenge {
			return ErrWrongChallengeKind
		}
	case SmallStepChallenge:
		if chal.kind != BigStepChallenge {
			return ErrWrongChallengeKind
		}
	}
	if chal.kind != subChallengeKind {
		return ErrWrongChallengeKind
	}
	// The challenge must be ongoing.
	if hasEnded(chal) {
		return ErrChallengeNotRunning
	}
	// There must not exist a subchallenge.
	if !v.SubChallenge.IsNone() {
		return ErrSubchallengeAlreadyExists
	}
	// The vertex must not be confirmed.
	if v.Status == ConfirmedAssertionState {
		return errors.Wrap(ErrWrongState, "vertex already confirmed")
	}
	// The vertex must have at least two children with unexpired
	// chess clocks in order to create a big step challenge.
	chain := chal.rootAssertion.Unwrap().chain
	ok, err := hasUnexpiredChildren(chain, v)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotEnoughValidChildren
	}
	return nil
}

// Checks if a challenge vertex has at least two children with
// unexpired chess-clocks. It does this by filtering out vertices from the chain
// that are the specified vertex's children and checking that at least two in this
// filtered list have unexpired chess clocks.
func hasUnexpiredChildren(chain *AssertionChain, v *ChallengeVertex) (bool, error) {
	challengeCommit := v.Challenge.Unwrap().ParentStateCommitment()
	challengeHash := ChallengeCommitHash(challengeCommit.Hash())
	vertices, ok := chain.challengeVerticesByCommitHash[challengeHash]
	if !ok {
		return false, fmt.Errorf("vertices not found for challenge with hash: %#x", challengeHash)
	}
	vertexCommitHash := v.Commitment.Hash()
	unexpiredChildrenTotal := 0
	for _, otherVertex := range vertices {
		if otherVertex.Prev.IsNone() {
			continue
		}
		parentCommitHash := otherVertex.Prev.Unwrap().Commitment.Hash()
		isChild := parentCommitHash == vertexCommitHash

		if isChild && !chessClockExpired(otherVertex) {
			unexpiredChildrenTotal++
		}
	}
	return unexpiredChildrenTotal > 1, nil
}

// Checks if a challenge is still ongoing by making sure the current
// timestamp is within the challenge's creation time + challenge period.
func hasEnded(challenge *Challenge) bool {
	chain := challenge.rootAssertion.Unwrap().chain
	now := chain.timeReference.Get()
	return now.Unix() > challenge.creationTime.Add(challenge.challengePeriod).Unix()
}

// Checks if a vertex's chess-clock has expired according
// to the challenge period length.
func chessClockExpired(v *ChallengeVertex) bool {
	chain := v.Challenge.Unwrap().rootAssertion.Unwrap().chain
	return v.PsTimer.Get() > chain.challengePeriod
}
