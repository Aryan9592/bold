package solimpl

import (
	"bytes"

	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	"github.com/OffchainLabs/challenge-protocol-v2/solgen/go/rollupgen"
	"github.com/OffchainLabs/challenge-protocol-v2/util"

	"github.com/ethereum/go-ethereum/common"

	"github.com/pkg/errors"
)

// Assertion is a wrapper around the binding to the type
// of the same name in the protocol contracts. This allows us
// to have a smaller API surface area and attach useful
// methods that callers can use directly.
type Assertion struct {
	StateCommitment util.StateCommitment
	chain           *AssertionChain
	id              uint64
}

func (a *Assertion) Height() (uint64, error) {
	assertionNode, err := a.fetchAssertionNode()
	if err != nil {
		return 0, err
	}
	return assertionNode.Height.Uint64(), nil
}

func (a *Assertion) fetchAssertionNode() (*rollupgen.AssertionNode, error) {
	assertionNode, err := a.chain.userLogic.GetAssertion(a.chain.callOpts, a.id)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(assertionNode.StateHash[:], make([]byte, 32)) {
		return nil, errors.Wrapf(
			ErrNotFound,
			"assertion with id %d",
			a.id,
		)
	}
	return &assertionNode, nil
}

func (a *Assertion) SeqNum() protocol.AssertionSequenceNumber {
	return protocol.AssertionSequenceNumber(a.id)
}

func (a *Assertion) PrevSeqNum() (protocol.AssertionSequenceNumber, error) {
	assertionNode, err := a.fetchAssertionNode()
	if err != nil {
		return 0, err
	}
	return protocol.AssertionSequenceNumber(assertionNode.PrevNum), nil
}

func (a *Assertion) StateHash() (common.Hash, error) {
	assertionNode, err := a.fetchAssertionNode()
	if err != nil {
		return common.Hash{}, err
	}
	return assertionNode.StateHash, nil
}

// Challenge is a developer-friendly wrapper around
// the protocol struct with the same name.
type Challenge struct {
	assertionChain *AssertionChain
	id             [32]byte
}

// ChallengeType defines an enum of the same name
// from the goimpl.
type ChallengeType uint

const (
	BlockChallenge ChallengeType = iota
	BigStepChallenge
	SmallStepChallenge
	OneStepChallenge
)

// ChallengeVertex is a developer-friendly wrapper around
// the protocol struct with the same name.
type ChallengeVertex struct {
	assertionChain *AssertionChain
	id             [32]byte
}
