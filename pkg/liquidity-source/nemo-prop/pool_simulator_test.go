package nemoprop

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/KyberNetwork/msgpack/v5"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/goccy/go-json"
	"github.com/stretchr/testify/require"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/swaplimit"
)

const (
	token0  = "0x0000000000000000000000000000000000000001"
	token1  = "0x0000000000000000000000000000000000000002"
	pricing = "0x0000000000000000000000000000000000000003"
)

func integer(s string) *big.Int {
	x, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(s)
	}
	return x
}

type quoteCaller func(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error)

func (f quoteCaller) CallContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
	return f(ctx, msg, block)
}
func (f quoteCaller) CodeAt(context.Context, common.Address, *big.Int) ([]byte, error) {
	return nil, nil
}

func testSimulator(t *testing.T) *PoolSimulator {
	t.Helper()
	e := Extra{Bid: big.NewInt(1), Ask: big.NewInt(1), KAsk: new(big.Int), KBid: new(big.Int), RhoAsk: new(big.Int), RhoBid: new(big.Int), QLighter: new(big.Int), QAnchor: new(big.Int), Vault: "vault", BlockTimestamp: 100, LastUpdatedTimestampMs: big.NewInt(100000), Allowances: [2]*big.Int{big.NewInt(10000), big.NewInt(10000)}}
	eb, err := json.Marshal(e)
	require.NoError(t, err)
	sb, err := json.Marshal(StaticExtra{PricingAddress: pricing, SwapAddress: "swap", AssetUnitFactor: "1"})
	require.NoError(t, err)
	p, err := NewPoolSimulator(pool.FactoryParams{EntityPool: entity.Pool{Address: "nemo", Exchange: DexType, Type: DexType, BlockNumber: 42, Tokens: []*entity.PoolToken{{Address: token0}, {Address: token1}}, Reserves: entity.PoolReserves{"1000", "2000"}, Extra: string(eb), StaticExtra: string(sb)}})
	require.NoError(t, err)
	p.Client = quoteCaller(func(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error) {
		return pricingABI.Methods["getAmountOut"].Outputs.Pack(big.NewInt(7))
	})
	return p
}

func TestRPCQuotes(t *testing.T) {
	for _, exact := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			p := testSimulator(t)
			i, j := 0, 1
			if reverse {
				i, j = j, i
			}
			method := "getAmountOut"
			if exact {
				method = "getAmountIn"
			}
			calls := 0
			p.Client = quoteCaller(func(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
				calls++
				require.Equal(t, uint64(42), block.Uint64())
				require.Equal(t, common.HexToAddress(pricing), *msg.To)
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
				expected, err := pricingABI.Pack(method, common.HexToAddress(p.Info.Tokens[i]), common.HexToAddress(p.Info.Tokens[j]), big.NewInt(10), p.Info.Reserves[i], p.Info.Reserves[j])
				require.NoError(t, err)
				require.Equal(t, expected, msg.Data)
				return pricingABI.Methods[method].Outputs.Pack(big.NewInt(7))
			})
			if exact {
				result, err := p.CalcAmountIn(pool.CalcAmountInParams{TokenIn: p.Info.Tokens[i], TokenAmountOut: pool.TokenAmount{Token: p.Info.Tokens[j], Amount: big.NewInt(10)}})
				require.NoError(t, err)
				require.Equal(t, int64(7), result.TokenAmountIn.Amount.Int64())
			} else {
				result, err := p.CalcAmountOut(pool.CalcAmountOutParams{TokenAmountIn: pool.TokenAmount{Token: p.Info.Tokens[i], Amount: big.NewInt(10)}, TokenOut: p.Info.Tokens[j]})
				require.NoError(t, err)
				require.Equal(t, int64(7), result.TokenAmountOut.Amount.Int64())
			}
			require.Equal(t, 1, calls)
		}
	}
}

func TestQuotePurityAndClone(t *testing.T) {
	p := testSimulator(t)
	before, err := json.Marshal(p)
	require.NoError(t, err)
	params := pool.CalcAmountOutParams{TokenAmountIn: pool.TokenAmount{Token: token0, Amount: big.NewInt(10)}, TokenOut: token1}
	first, err := p.CalcAmountOut(params)
	require.NoError(t, err)
	again, err := p.CalcAmountOut(params)
	require.NoError(t, err)
	require.Equal(t, first, again)
	clone := p.CloneState().(*PoolSimulator)
	clone.UpdateBalance(pool.UpdateBalanceParams{SwapInfo: first.SwapInfo})
	require.Equal(t, int64(1010), clone.Info.Reserves[0].Int64())
	require.Equal(t, int64(1993), clone.Info.Reserves[1].Int64())
	require.Equal(t, int64(9993), clone.Extra.Allowances[1].Int64())
	after, err := json.Marshal(p)
	require.NoError(t, err)
	require.Equal(t, before, after)
	first.TokenAmountOut.Amount.SetInt64(1)
	require.Equal(t, int64(7), first.SwapInfo.(SwapInfo).AmountOut.Int64())
	require.Equal(t, uint64(42), p.GetMetaInfo("", "").(pool.MetaInfo).BlockNumber)
}

func TestRPCFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		err  error
	}{
		{name: "revert", err: errors.New("execution reverted")},
		{name: "empty"}, {name: "short", data: []byte{1}},
		{name: "long", data: make([]byte, 64)}, {name: "zero", data: make([]byte, 32)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testSimulator(t)
			p.Client = quoteCaller(func(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error) { return tc.data, tc.err })
			before, _ := json.Marshal(p)
			_, err := p.quote(token0, token1, big.NewInt(10), nil, false)
			require.Error(t, err)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			}
			after, _ := json.Marshal(p)
			require.Equal(t, before, after)
		})
	}
	p := testSimulator(t)
	p.Client = nil
	_, err := p.quote(token0, token1, big.NewInt(10), nil, false)
	require.ErrorContains(t, err, "RPC client required")
	p = testSimulator(t)
	p.Extra.HasOverrides = true
	_, err = p.quote(token0, token1, big.NewInt(10), nil, false)
	require.ErrorContains(t, err, "overridden snapshots")
}

func TestConstraints(t *testing.T) {
	p := testSimulator(t)
	for _, amount := range []*big.Int{nil, big.NewInt(0), big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 255)} {
		_, err := p.quote(token0, token1, amount, nil, false)
		require.Error(t, err)
	}
	for _, tokens := range [][2]string{{"bad", token1}, {token0, token0}} {
		_, err := p.quote(tokens[0], tokens[1], big.NewInt(10), nil, false)
		require.Error(t, err)
	}
	_, err := p.quote(token0, token1, big.NewInt(2001), nil, true)
	require.Error(t, err)
	p.Extra.Allowances[1] = big.NewInt(6)
	_, err = p.quote(token0, token1, big.NewInt(10), nil, false)
	require.Error(t, err)
	p = testSimulator(t)
	p.Info.Reserves[1] = big.NewInt(3)
	s, err := p.quote(token0, token1, big.NewInt(10), nil, false)
	require.NoError(t, err)
	require.Equal(t, int64(3), s.AmountOut.Int64())
	require.Equal(t, int64(10), s.AmountIn.Int64())
	p.Info.Reserves[0] = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	_, err = p.quote(token0, token1, big.NewInt(10), nil, false)
	require.ErrorContains(t, err, "overflow")
}

func TestSharedVaultRPCBalances(t *testing.T) {
	p := testSimulator(t)
	other := p.CloneState().(*PoolSimulator)
	limit := swaplimit.NewInventory(DexType, p.CalculateLimit())
	s, err := p.quote(token0, token1, big.NewInt(10), limit, false)
	require.NoError(t, err)
	p.UpdateBalance(pool.UpdateBalanceParams{SwapInfo: s, SwapLimit: limit})
	require.Equal(t, p.Info.Reserves[1], limit.GetLimit(p.balanceKey(1)))
	require.Equal(t, p.Extra.Allowances[1], limit.GetLimit(p.allowanceKey(1)))
	other.Client = quoteCaller(func(_ context.Context, msg ethereum.CallMsg, _ *big.Int) ([]byte, error) {
		args, err := pricingABI.Methods["getAmountOut"].Inputs.Unpack(msg.Data[4:])
		require.NoError(t, err)
		// Reverse quote must receive updated inventory in token-in/token-out order.
		require.Equal(t, big.NewInt(1993), args[3])
		require.Equal(t, big.NewInt(1010), args[4])
		return pricingABI.Methods["getAmountOut"].Outputs.Pack(big.NewInt(4))
	})
	_, err = other.quote(token1, token0, big.NewInt(10), limit, false)
	require.NoError(t, err)
}

func TestSerializationRequiresClientReinjection(t *testing.T) {
	p := testSimulator(t)
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	enc.IncludeUnexported(true)
	enc.SetForceAsArray(true)
	require.NoError(t, enc.Encode(p))
	var restored PoolSimulator
	dec := msgpack.NewDecoder(&buf)
	dec.IncludeUnexported(true)
	dec.SetForceAsArray(true)
	require.NoError(t, dec.Decode(&restored))
	require.Nil(t, restored.Client)
	_, err := restored.quote(token0, token1, big.NewInt(10), nil, false)
	require.ErrorContains(t, err, "RPC client required")
	restored.Client = p.Client
	_, err = restored.quote(token0, token1, big.NewInt(10), nil, false)
	require.NoError(t, err)
}
