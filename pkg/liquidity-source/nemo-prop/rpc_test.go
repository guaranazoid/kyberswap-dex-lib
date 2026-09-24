package nemoprop

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient/gethclient"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	utilabi "github.com/KyberNetwork/kyberswap-dex-lib/pkg/util/abi"
)

func testMulticallABI(t *testing.T) abi.ABI {
	t.Helper()
	multicallABI, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"aggregate","inputs":[{"name":"calls","type":"tuple[]","components":[{"name":"target","type":"address"},{"name":"callData","type":"bytes"}]}],"outputs":[{"name":"blockNumber","type":"uint256"},{"name":"returnData","type":"bytes[]"}]}]`))
	require.NoError(t, err)
	return multicallABI
}

func TestTrackerPinnedSnapshotAndOverrides(t *testing.T) {
	multicallABI := testMulticallABI(t)
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "reverted balance"}[fail], func(t *testing.T) {
			vault := common.HexToAddress("0x1234")
			swap := common.HexToAddress("0x5678")
			pricing := common.HexToAddress("0x9999")
			token0, token1 := common.HexToAddress("0x10"), common.HexToAddress("0x20")
			calls := 0
			pack := func(a abi.ABI, method string, values ...any) []byte {
				data, err := a.Methods[method].Outputs.Pack(values...)
				require.NoError(t, err)
				return data
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ID     json.RawMessage   `json:"id"`
					Method string            `json:"method"`
					Params []json.RawMessage `json:"params"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				var result any
				if request.Method == "eth_getBlockByNumber" {
					require.JSONEq(t, `"0x2a"`, string(request.Params[0]))
					result = &types.Header{Number: big.NewInt(42), Time: 100, Difficulty: new(big.Int), Extra: []byte{}}
				} else {
					require.Equal(t, "eth_call", request.Method)
					require.Len(t, request.Params, 3)
					var overrides map[string]any
					require.NoError(t, json.Unmarshal(request.Params[2], &overrides))
					require.Contains(t, overrides, hexutil.Encode(vault[:]))
					var call map[string]string
					require.NoError(t, json.Unmarshal(request.Params[0], &call))
					encoded := call["input"]
					if encoded == "" {
						encoded = call["data"]
					}
					data, err := hexutil.Decode(encoded)
					require.NoError(t, err)
					args, err := multicallABI.Methods["aggregate"].Inputs.Unpack(data[4:])
					require.NoError(t, err)
					decoded := *abi.ConvertType(args[0], new([]struct {
						Target   common.Address
						CallData []byte
					})).(*[]struct {
						Target   common.Address
						CallData []byte
					})
					var outputs [][]byte
					if calls == 0 {
						require.JSONEq(t, `"latest"`, string(request.Params[1]))
						require.Len(t, decoded, 3)
						require.Equal(t, swap, decoded[0].Target)
						require.Equal(t, pricing, decoded[1].Target)
						require.Equal(t, pricingABI.Methods["getMarketState"].ID, decoded[1].CallData[:4])
						outputs = [][]byte{pack(swapABI, "owner", vault), pack(pricingABI, "getMarketState", big.NewInt(200000), big.NewInt(200001), big.NewInt(3e6), big.NewInt(5e6), big.NewInt(3e10), big.NewInt(4e10), big.NewInt(123), big.NewInt(456)), pack(pricingABI, "lastUpdatedTimestampMs", big.NewInt(99999))}
					} else {
						require.JSONEq(t, `"0x2a"`, string(request.Params[1]))
						require.Len(t, decoded, 4)
						for i, c := range decoded {
							require.Equal(t, []common.Address{token0, token0, token1, token1}[i], c.Target)
							method := "balanceOf"
							if i%2 == 1 {
								method = "allowance"
							}
							require.Equal(t, utilabi.Erc20ABI.Methods[method].ID, c.CallData[:4])
							values, err := utilabi.Erc20ABI.Methods[method].Inputs.Unpack(c.CallData[4:])
							require.NoError(t, err)
							require.Equal(t, vault, values[0])
							if i%2 == 1 {
								require.Equal(t, swap, values[1])
							}
							outputs = append(outputs, pack(utilabi.Erc20ABI, method, big.NewInt(int64(100+i))))
						}
					}
					calls++
					if fail && calls == 2 {
						require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "execution reverted"}}))
						return
					}
					result = hexutil.Encode(pack(multicallABI, "aggregate", big.NewInt(42), outputs))
				}
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}))
			}))
			defer server.Close()
			client := ethrpc.New(server.URL)
			defer client.GetETHClient().Close()
			s, _ := json.Marshal(StaticExtra{PricingAddress: hexutil.Encode(pricing[:]), SwapAddress: hexutil.Encode(swap[:]), AssetUnitFactor: "1000000000000000000", MarketIndex: 1})
			ep := entity.Pool{StaticExtra: string(s), Tokens: []*entity.PoolToken{{Address: hexutil.Encode(token0[:])}, {Address: hexutil.Encode(token1[:])}}}
			got, err := NewPoolTracker(&Config{}, client).GetNewPoolStateWithOverrides(t.Context(), ep, pool.GetNewPoolStateWithOverridesParams{Overrides: map[common.Address]gethclient.OverrideAccount{vault: {Balance: big.NewInt(1)}}})
			require.Equal(t, 2, calls)
			if fail {
				require.Error(t, err)
				require.Equal(t, ep, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, uint64(42), got.BlockNumber)
			require.Equal(t, int64(100), got.Timestamp)
			require.Equal(t, entity.PoolReserves{"100", "102"}, got.Reserves)
			var e Extra
			require.NoError(t, json.Unmarshal([]byte(got.Extra), &e))
			require.True(t, e.HasOverrides)
			require.Equal(t, "99999", e.LastUpdatedTimestampMs.String())
			require.Equal(t, "123", e.QLighter.String())
			require.Equal(t, "456", e.QAnchor.String())
			require.Equal(t, "30000000000", e.RhoAsk.String())
			require.Equal(t, "40000000000", e.RhoBid.String())
			require.Equal(t, "103", e.Allowances[1].String())
		})
	}
}
