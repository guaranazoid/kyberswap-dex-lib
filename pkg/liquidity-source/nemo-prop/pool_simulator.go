package nemoprop

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/goccy/go-json"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

var (
	_ = pool.RegisterFactory(DexType, NewPoolSimulator)
	_ = pool.RegisterUseSwapLimit(valueobject.ExchangeNemoProp)
)

type PoolSimulator struct {
	pool.Pool
	StaticExtra StaticExtra
	Extra       Extra
	// RPC clients are runtime dependencies, not serializable pool state.
	Client ethereum.ContractCaller `json:"-" msgpack:"-"`
}

func NewPoolSimulator(params pool.FactoryParams) (*PoolSimulator, error) {
	ep := params.EntityPool
	if len(ep.Tokens) != 2 || len(ep.Reserves) != 2 || ep.Tokens[0] == nil || ep.Tokens[1] == nil || ep.Tokens[0].Address == ep.Tokens[1].Address {
		return nil, fmt.Errorf("invalid Nemo pool")
	}
	p := &PoolSimulator{Pool: pool.FromEntity(ep), Client: params.EthClient}
	if err := json.Unmarshal([]byte(ep.StaticExtra), &p.StaticExtra); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(ep.Extra), &p.Extra); err != nil {
		return nil, err
	}
	if ep.BlockNumber == 0 || !common.IsHexAddress(p.StaticExtra.PricingAddress) || common.HexToAddress(p.StaticExtra.PricingAddress) == (common.Address{}) || p.Extra.Vault == "" || p.StaticExtra.SwapAddress == "" || !common.IsHexAddress(ep.Tokens[0].Address) || !common.IsHexAddress(ep.Tokens[1].Address) {
		return nil, fmt.Errorf("invalid Nemo metadata")
	}
	if err := validateSnapshot(p.Extra, [2]*big.Int{p.Info.Reserves[0], p.Info.Reserves[1]}); err != nil {
		return nil, err
	}
	if params.Opts.StaleCheck && time.Since(time.Unix(int64(p.Extra.BlockTimestamp), 0)) > time.Minute {
		return nil, fmt.Errorf("stale Nemo snapshot")
	}
	return p, nil
}
func (p *PoolSimulator) balanceKey(i int) string { return p.Extra.Vault + ":" + p.Info.Tokens[i] }
func (p *PoolSimulator) allowanceKey(i int) string {
	return p.balanceKey(i) + ":" + p.StaticExtra.SwapAddress
}
func (p *PoolSimulator) CalculateLimit() map[string]*big.Int {
	limits := make(map[string]*big.Int, 4)
	for i := range 2 {
		limits[p.balanceKey(i)] = new(big.Int).Set(p.Info.Reserves[i])
		limits[p.allowanceKey(i)] = new(big.Int).Set(p.Extra.Allowances[i])
	}
	return limits
}
func (p *PoolSimulator) quote(tokenIn, tokenOut string, amount *big.Int, limit pool.SwapLimit, exactOut bool) (info SwapInfo, err error) {
	if p.Client == nil {
		return info, fmt.Errorf("Nemo RPC client required; inject FactoryParams.EthClient or restore Client after deserialization")
	}
	if p.Extra.HasOverrides {
		return info, fmt.Errorf("Nemo RPC quotes do not support overridden snapshots")
	}
	i, j := p.GetTokenIndex(tokenIn), p.GetTokenIndex(tokenOut)
	if i < 0 || j < 0 || i == j || amount == nil || amount.Sign() <= 0 || amount.BitLen() > 255 {
		return info, fmt.Errorf("invalid Nemo swap")
	}
	balances := [2]*big.Int{p.Info.Reserves[0], p.Info.Reserves[1]}
	allowance := p.Extra.Allowances[j]
	if limit != nil {
		for k := range 2 {
			balances[k] = limit.GetLimit(p.balanceKey(k))
			if balances[k] == nil {
				return info, fmt.Errorf("missing Nemo vault limit")
			}
			if balances[k].Sign() < 0 || balances[k].BitLen() > 256 {
				return info, fmt.Errorf("invalid Nemo vault limit")
			}
		}
		allowance = limit.GetLimit(p.allowanceKey(j))
		if allowance == nil {
			return info, fmt.Errorf("missing Nemo allowance limit")
		}
		if allowance.Sign() < 0 || allowance.BitLen() > 256 {
			return info, fmt.Errorf("invalid Nemo allowance limit")
		}
	}
	if exactOut && amount.Cmp(balances[j]) > 0 {
		return info, fmt.Errorf("insufficient Nemo reserve")
	}
	method := "getAmountOut"
	if exactOut {
		method = "getAmountIn"
	}
	data, err := pricingABI.Pack(method, common.HexToAddress(tokenIn), common.HexToAddress(tokenOut), amount, balances[i], balances[j])
	if err != nil {
		return info, err
	}
	address := common.HexToAddress(p.StaticExtra.PricingAddress)
	// CalcAmountOut has no caller context; bound every RPC to avoid hanging routing.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := p.Client.CallContract(ctx, ethereum.CallMsg{To: &address, Data: data}, new(big.Int).SetUint64(p.Info.BlockNumber))
	if err != nil {
		return info, fmt.Errorf("Nemo %s RPC: %w", method, err)
	}
	if len(response) != 32 {
		return info, fmt.Errorf("invalid Nemo quote response length: %d", len(response))
	}
	values, err := pricingABI.Unpack(method, response)
	if err != nil {
		return info, err
	}
	result, ok := values[0].(*big.Int)
	if !ok {
		return info, fmt.Errorf("invalid Nemo quote response")
	}
	in, out := amount, result
	if exactOut {
		in, out = result, amount
	} else if out.Cmp(balances[j]) > 0 {
		out = new(big.Int).Set(balances[j])
	}
	if in.Sign() <= 0 || in.BitLen() > 255 || out.Sign() <= 0 || out.Cmp(allowance) > 0 {
		return info, fmt.Errorf("zero quote or insufficient Nemo allowance")
	}
	if new(big.Int).Add(balances[i], in).BitLen() > 256 {
		return info, fmt.Errorf("Nemo balance overflow")
	}
	return SwapInfo{IndexIn: i, AmountIn: new(big.Int).Set(in), AmountOut: new(big.Int).Set(out)}, nil
}
func (p *PoolSimulator) CalcAmountOut(params pool.CalcAmountOutParams) (*pool.CalcAmountOutResult, error) {
	s, err := p.quote(params.TokenAmountIn.Token, params.TokenOut, params.TokenAmountIn.Amount, params.Limit, false)
	if err != nil {
		return nil, err
	}
	return &pool.CalcAmountOutResult{TokenAmountOut: &pool.TokenAmount{Token: params.TokenOut, Amount: new(big.Int).Set(s.AmountOut)}, Fee: &pool.TokenAmount{Token: params.TokenAmountIn.Token, Amount: new(big.Int)}, Gas: defaultGas, SwapInfo: s}, nil
}
func (p *PoolSimulator) CalcAmountIn(params pool.CalcAmountInParams) (*pool.CalcAmountInResult, error) {
	s, err := p.quote(params.TokenIn, params.TokenAmountOut.Token, params.TokenAmountOut.Amount, params.Limit, true)
	if err != nil {
		return nil, err
	}
	return &pool.CalcAmountInResult{TokenAmountIn: &pool.TokenAmount{Token: params.TokenIn, Amount: new(big.Int).Set(s.AmountIn)}, Fee: &pool.TokenAmount{Token: params.TokenIn, Amount: new(big.Int)}, Gas: defaultGas, SwapInfo: s}, nil
}
func (p *PoolSimulator) UpdateBalance(params pool.UpdateBalanceParams) {
	s, ok := params.SwapInfo.(SwapInfo)
	if !ok || s.IndexIn < 0 || s.IndexIn > 1 || s.AmountIn == nil || s.AmountOut == nil {
		return
	}
	i, j := s.IndexIn, 1-s.IndexIn
	balances := [2]*big.Int{p.Info.Reserves[0], p.Info.Reserves[1]}
	allowance := p.Extra.Allowances[j]
	if params.SwapLimit != nil {
		for k := range 2 {
			balances[k] = params.SwapLimit.GetLimit(p.balanceKey(k))
		}
		allowance = params.SwapLimit.GetLimit(p.allowanceKey(j))
	}
	if balances[i] == nil || balances[j] == nil || allowance == nil || s.AmountIn.Sign() <= 0 || s.AmountOut.Sign() <= 0 || balances[j].Cmp(s.AmountOut) < 0 || allowance.Cmp(s.AmountOut) < 0 {
		return
	}
	nextIn := new(big.Int).Add(balances[i], s.AmountIn)
	if nextIn.BitLen() > 256 {
		return
	}
	nextOut := new(big.Int).Sub(balances[j], s.AmountOut)
	// ERC20s commonly preserve infinite approval. Decreasing it conservatively
	// also supports tokens that spend even a uint256-max allowance.
	nextAllowance := new(big.Int).Sub(allowance, s.AmountOut)
	if params.SwapLimit != nil {
		if _, _, err := params.SwapLimit.UpdateLimit(p.balanceKey(j), p.balanceKey(i), s.AmountOut, s.AmountIn); err != nil {
			return
		}
		if _, _, err := params.SwapLimit.UpdateLimit(p.allowanceKey(j), p.allowanceKey(i), s.AmountOut, new(big.Int)); err != nil {
			return
		}
	}
	p.Info.Reserves[i], p.Info.Reserves[j] = nextIn, nextOut
	p.Extra.Allowances[j] = nextAllowance
}
func (p *PoolSimulator) CloneState() pool.IPoolSimulator {
	c := *p
	c.Info.Reserves = append([]*big.Int(nil), p.Info.Reserves...)
	// Integers are immutable after construction; updates replace pointers.
	return &c
}
func (p *PoolSimulator) GetMetaInfo(_, _ string) any {
	return pool.MetaInfo{ApprovalAddress: p.StaticExtra.SwapAddress, BlockNumber: p.Info.BlockNumber}
}
