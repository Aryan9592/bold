package solimpl

import (
	"context"
	"math/big"
	"time"

	"github.com/OffchainLabs/challenge-protocol-v2/protocol"
	"github.com/OffchainLabs/challenge-protocol-v2/solgen/go/challengeV2gen"
	"github.com/OffchainLabs/challenge-protocol-v2/util"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
)

var (
	ErrChallengeNotFound = errors.New("challenge not found")
	ErrPsTimerNotYet     = errors.New("ps timer has not exceeded challenge period")
)

// ChallengeManager --
type ChallengeManager struct {
	assertionChain *AssertionChain
	addr           common.Address
	caller         *challengeV2gen.ChallengeManagerImplCaller
	writer         *challengeV2gen.ChallengeManagerImplTransactor
	filterer       *challengeV2gen.ChallengeManagerImplFilterer
}

// ChallengeManager returns an instance of the current challenge manager
// used by the assertion chain.
func (ac *AssertionChain) CurrentChallengeManager(
	ctx context.Context, tx protocol.ActiveTx,
) (protocol.ChallengeManager, error) {
	addr, err := ac.userLogic.ChallengeManager(ac.callOpts)
	if err != nil {
		return nil, err
	}
	managerBinding, err := challengeV2gen.NewChallengeManagerImpl(addr, ac.backend)
	if err != nil {
		return nil, err
	}
	return &ChallengeManager{
		assertionChain: ac,
		addr:           addr,
		caller:         &managerBinding.ChallengeManagerImplCaller,
		writer:         &managerBinding.ChallengeManagerImplTransactor,
		filterer:       &managerBinding.ChallengeManagerImplFilterer,
	}, nil
}

func (cm *ChallengeManager) Address() common.Address {
	return cm.addr
}

// ChallengePeriodSeconds --
func (cm *ChallengeManager) ChallengePeriodSeconds(
	ctx context.Context, tx protocol.ActiveTx,
) (time.Duration, error) {
	res, err := cm.caller.ChallengePeriodSec(cm.assertionChain.callOpts)
	if err != nil {
		return time.Second, err
	}
	return time.Second * time.Duration(res.Uint64()), nil
}

// CalculateChallengeId calculates the challenge hash for a given assertion and challenge type.
func (cm *ChallengeManager) CalculateChallengeHash(
	ctx context.Context,
	tx protocol.ActiveTx,
	itemId common.Hash,
	cType protocol.ChallengeType,
) (protocol.ChallengeHash, error) {
	c, err := cm.caller.CalculateChallengeId(cm.assertionChain.callOpts, itemId, uint8(cType))
	if err != nil {
		return protocol.ChallengeHash{}, err
	}
	return c, nil
}

func (cm *ChallengeManager) CalculateChallengeVertexId(
	ctx context.Context,
	tx protocol.ActiveTx,
	challengeId protocol.ChallengeHash,
	history util.HistoryCommitment,
) (protocol.VertexHash, error) {
	vertexId, err := cm.caller.CalculateChallengeVertexId(
		cm.assertionChain.callOpts,
		challengeId,
		history.Merkle,
		big.NewInt(int64(history.Height)),
	)
	if err != nil {
		return protocol.VertexHash{}, err
	}
	return protocol.VertexHash(vertexId), nil
}

// GetVertex returns the challenge vertex for the given vertexId.
func (cm *ChallengeManager) GetVertex(
	ctx context.Context,
	tx protocol.ActiveTx,
	vertexId protocol.VertexHash,
) (util.Option[protocol.ChallengeVertex], error) {
	_, err := cm.caller.GetVertex(cm.assertionChain.callOpts, vertexId)
	if err != nil {
		return util.None[protocol.ChallengeVertex](), err
	}
	return util.Some[protocol.ChallengeVertex](&ChallengeVertex{
		chain: cm.assertionChain,
		id:    vertexId,
	}), nil
}

// GetChallenge returns the challenge for the given challengeId.
func (cm *ChallengeManager) GetChallenge(
	ctx context.Context,
	tx protocol.ActiveTx,
	challengeId protocol.ChallengeHash,
) (util.Option[protocol.Challenge], error) {
	_, err := cm.caller.GetChallenge(cm.assertionChain.callOpts, challengeId)
	if err != nil {
		return util.None[protocol.Challenge](), err
	}
	return util.Some[protocol.Challenge](&Challenge{
		chain: cm.assertionChain,
		id:    challengeId,
	}), nil
}

//nolint:unused
func (cm *ChallengeManager) miniStakeAmount() (*big.Int, error) {
	return cm.caller.MiniStakeValue(cm.assertionChain.callOpts)
}
