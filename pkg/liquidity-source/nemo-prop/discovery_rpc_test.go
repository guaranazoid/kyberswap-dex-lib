package nemoprop

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryRPC(t *testing.T) {
	base, asset := common.HexToAddress("0x1234"), common.HexToAddress("0xabcd")
	markets := append(append([]byte{}, asset[:]...), common.LeftPadBytes(big.NewInt(100000000).Bytes(), 12)...)
	output, err := pricingABI.Methods["getMarkets_v1"].Outputs.Pack(base, markets)
	require.NoError(t, err)
	result, err := testMulticallABI(t).Methods["aggregate"].Outputs.Pack(big.NewInt(42), [][]byte{output})
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, "eth_call", request.Method)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": hexutil.Encode(result)}))
	}))
	defer server.Close()
	client := ethrpc.New(server.URL)
	defer client.GetETHClient().Close()
	updater := NewPoolsListUpdater(&Config{DexID: DexType, PricingAddress: "0x0000000000000000000000000000000000004321", SwapAddress: "0x0000000000000000000000000000000000009876"}, client)
	pools, metadata, err := updater.GetNewPools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, pools, 1)
	p := pools[0]
	require.Equal(t, []string{"0", "0"}, []string(p.Reserves))
	require.Equal(t, hexutil.Encode(base[:]), p.Tokens[0].Address)
	require.Equal(t, hexutil.Encode(asset[:]), p.Tokens[1].Address)
	require.Zero(t, p.Tokens[1].Decimals)
	require.True(t, p.Tokens[1].Swappable)
	var s StaticExtra
	require.NoError(t, json.Unmarshal([]byte(p.StaticExtra), &s))
	require.Equal(t, "100000000", s.AssetUnitFactor)
	require.Zero(t, s.MarketIndex)
	require.Contains(t, p.Address, s.SwapAddress)
	again, next, err := updater.GetNewPools(t.Context(), metadata)
	require.NoError(t, err)
	require.Empty(t, again)
	require.Equal(t, metadata, next)
	for _, invalid := range []string{`{"offset":-1}`, `broken`} {
		_, _, err = updater.GetNewPools(t.Context(), []byte(invalid))
		require.Error(t, err)
	}
}
