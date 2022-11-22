package inbox_feeder

import (
	"context"
	"testing"
	"time"

	"github.com/OffchainLabs/new-rollup-exploration/protocol"
	"github.com/OffchainLabs/new-rollup-exploration/util"
	"github.com/stretchr/testify/require"
)

func TestInboxFeeder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timeRef := util.NewArtificialTimeReference()
	chain := protocol.NewAssertionChain(ctx, timeRef, time.Second, nil)
	StartInboxFeeder(ctx, chain, time.Second, []byte{})
	time.Sleep(100 * time.Millisecond)

	getNumMsgs := func() uint64 {
		t.Helper()
		var numMsgs uint64
		err := chain.Call(func(tx *protocol.ActiveTx, innerChain *protocol.AssertionChain) error {
			numMsgs = innerChain.Inbox().NumMessages(tx)
			return nil
		})
		require.NoError(t, err)
		return numMsgs
	}

	require.Equal(t, getNumMsgs(), uint64(0))
	timeRef.Add(1500 * time.Millisecond)
	time.Sleep(100 * time.Millisecond) // allow some time for msg to land in inbox
	require.Equal(t, getNumMsgs(), uint64(1))
	timeRef.Add(time.Second)
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, getNumMsgs(), uint64(2))
	timeRef.Add(time.Second)
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, getNumMsgs(), uint64(3))
}
