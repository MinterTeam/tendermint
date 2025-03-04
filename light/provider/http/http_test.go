package http_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/abci/example/kvstore"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/service"
	"github.com/tendermint/tendermint/light/provider"
	lighthttp "github.com/tendermint/tendermint/light/provider/http"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
	rpctest "github.com/tendermint/tendermint/rpc/test"
	"github.com/tendermint/tendermint/types"
)

func TestNewProvider(t *testing.T) {
	c, err := lighthttp.New("chain-test", "192.168.0.1:26657")
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%s", c), "http{http://192.168.0.1:26657}")

	c, err = lighthttp.New("chain-test", "http://153.200.0.1:26657")
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%s", c), "http{http://153.200.0.1:26657}")

	c, err = lighthttp.New("chain-test", "153.200.0.1")
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%s", c), "http{http://153.200.0.1}")
}

// NodeSuite initiates and runs a full node instance in the
// background, stopping it once the test is completed
func NodeSuite(t *testing.T) (service.Service, *config.Config) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	conf := rpctest.CreateConfig()

	// start a tendermint node in the background to test against
	app := kvstore.NewApplication()
	app.RetainBlocks = 9

	node, closer, err := rpctest.StartTendermint(ctx, conf, app)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = closer(ctx)
		cancel()
	})
	return node, conf
}

func TestProvider(t *testing.T) {
	_, cfg := NodeSuite(t)
	rpcAddr := cfg.RPC.ListenAddress
	genDoc, err := types.GenesisDocFromFile(cfg.GenesisFile())
	require.NoError(t, err)

	chainID := genDoc.ChainID
	t.Log("chainID:", chainID)

	c, err := rpchttp.New(rpcAddr)
	require.Nil(t, err)

	p := lighthttp.NewWithClient(chainID, c)
	require.NoError(t, err)
	require.NotNil(t, p)

	// let it produce some blocks
	err = rpcclient.WaitForHeight(c, 10, nil)
	require.NoError(t, err)

	// let's get the highest block
	lb, err := p.LightBlock(context.Background(), 0)
	require.NoError(t, err)
	assert.True(t, lb.Height < 9001, "height=%d", lb.Height)

	// let's check this is valid somehow
	assert.Nil(t, lb.ValidateBasic(chainID))

	// historical queries now work :)
	lower := lb.Height - 3
	lb, err = p.LightBlock(context.Background(), lower)
	require.NoError(t, err)
	assert.Equal(t, lower, lb.Height)

	// fetching missing heights (both future and pruned) should return appropriate errors
	lb, err = p.LightBlock(context.Background(), 9001)
	require.Error(t, err)
	require.Nil(t, lb)
	assert.Equal(t, provider.ErrHeightTooHigh, err)

	_, err = p.LightBlock(context.Background(), 1)
	require.Error(t, err)
	assert.Equal(t, provider.ErrLightBlockNotFound, err)

	// if the provider is unable to provide four more blocks then we should return
	// an unreliable peer error
	for i := 0; i < 4; i++ {
		_, err = p.LightBlock(context.Background(), 1)
	}
	assert.IsType(t, provider.ErrUnreliableProvider{}, err)
}
