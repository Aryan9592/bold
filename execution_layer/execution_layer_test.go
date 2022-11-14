package execution_layer

import (
	"context"
	"encoding/binary"
	"github.com/OffchainLabs/new-rollup-exploration/protocol"
	"github.com/OffchainLabs/new-rollup-exploration/util"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestExecutionLayer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timeRef := util.NewArtificialTimeReference()
	chain := protocol.NewAssertionChain(ctx, timeRef, time.Minute)
	execLayer := GenesisExecutionState(chain)
	proofChecker := execLayer.GetProofChecker()

	genesisState := execLayer.Clone()
	genesisRoot := genesisState.Root()
	require.Equal(t, genesisRoot, crypto.Keccak256Hash(binary.BigEndian.AppendUint64(common.Hash{}.Bytes(), 0)))

	msg0 := []byte{0}
	appendMessage(chain, msg0)
	execLayer, err := execLayer.ExecuteOne()
	require.NoError(t, err)
	require.NotEqualf(t, execLayer.Root(), genesisRoot, "root did not change after executing first message")

	proof0, err := genesisState.Prove(msg0, execLayer.Root())
	require.NoError(t, err)

	require.True(t, proofChecker(genesisRoot, execLayer.Root(), proof0))

	require.False(t, proofChecker(genesisRoot, execLayer.Root(), proof0[:len(proof0)-1]))
}

func appendMessage(chain *protocol.AssertionChain, msg []byte) {
	_ = chain.Tx(func(tx *protocol.ActiveTx, innerChain *protocol.AssertionChain) error {
		innerChain.Inbox().Append(tx, msg)
		return nil
	})
}
