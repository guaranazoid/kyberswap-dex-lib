package nemoprop

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"
)

// Exercise the actual ethclient JSON-RPC transport, not only the caller interface.
func TestQuoteHTTPRPC(t *testing.T) {
	for _, revert := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "revert"}[revert], func(t *testing.T) {
			type request struct {
				ID     json.RawMessage   `json:"id"`
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			requests := make(chan request, 1)
			output, err := pricingABI.Methods["getAmountOut"].Outputs.Pack(big.NewInt(17))
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req request
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				requests <- req
				response := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": hexutil.Encode(output)}
				if revert {
					delete(response, "result")
					response["error"] = map[string]any{"code": -32000, "message": "execution reverted"}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			client, err := ethclient.Dial(server.URL)
			require.NoError(t, err)
			defer client.Close()
			p := testSimulator(t)
			p.Client = client
			result, err := p.quote(token0, token1, big.NewInt(10), nil, false)
			if revert {
				require.ErrorContains(t, err, "execution reverted")
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(17), result.AmountOut.Int64())
			}
			req := <-requests
			require.Equal(t, "eth_call", req.Method)
			require.Len(t, req.Params, 2)
			require.JSONEq(t, `"0x2a"`, string(req.Params[1]))
			var call map[string]string
			require.NoError(t, json.Unmarshal(req.Params[0], &call))
			require.Equal(t, pricing, call["to"])
			data := call["input"]
			if data == "" {
				data = call["data"]
			}
			require.Contains(t, data, hexutil.Encode(pricingABI.Methods["getAmountOut"].ID)[2:])
		})
	}
}
