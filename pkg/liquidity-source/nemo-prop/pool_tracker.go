package nemoprop

import (
	"context"
	"fmt"
	"math/big"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient/gethclient"
	"github.com/goccy/go-json"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool"
	pooltrack "github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool/tracker"
	utilabi "github.com/KyberNetwork/kyberswap-dex-lib/pkg/util/abi"
)

type PoolTracker struct {
	config       *Config
	ethrpcClient *ethrpc.Client
}

var _ = pooltrack.RegisterFactoryCE0(DexType, NewPoolTracker)

func NewPoolTracker(config *Config, client *ethrpc.Client) *PoolTracker {
	return &PoolTracker{config, client}
}
func (t *PoolTracker) GetNewPoolState(ctx context.Context, p entity.Pool, _ pool.GetNewPoolStateParams) (entity.Pool, error) {
	return t.getNewPoolState(ctx, p, nil)
}
func (t *PoolTracker) GetNewPoolStateWithOverrides(ctx context.Context, p entity.Pool, params pool.GetNewPoolStateWithOverridesParams) (entity.Pool, error) {
	return t.getNewPoolState(ctx, p, params.Overrides)
}

func (t *PoolTracker) getNewPoolState(ctx context.Context, p entity.Pool, overrides map[common.Address]gethclient.OverrideAccount) (entity.Pool, error) {
	var s StaticExtra
	if err := json.Unmarshal([]byte(p.StaticExtra), &s); err != nil {
		return p, err
	}
	if len(p.Tokens) != 2 {
		return p, fmt.Errorf("invalid Nemo token count")
	}
	e := Extra{HasOverrides: len(overrides) > 0}
	var vault common.Address
	req := t.ethrpcClient.NewRequest().SetContext(ctx)
	if overrides != nil {
		req.SetOverrides(overrides)
	}
	req.AddCall(&ethrpc.Call{ABI: swapABI, Target: s.SwapAddress, Method: "owner"}, []any{&vault})
	req.AddCall(&ethrpc.Call{ABI: pricingABI, Target: s.PricingAddress, Method: "getMarketState", Params: []any{new(big.Int).SetUint64(s.MarketIndex)}}, []any{&e})
	req.AddCall(&ethrpc.Call{ABI: pricingABI, Target: s.PricingAddress, Method: "lastUpdatedTimestampMs"}, []any{&e.LastUpdatedTimestampMs})
	resp, err := req.Aggregate()
	if err != nil {
		return p, err
	}
	if resp.BlockNumber == nil || resp.BlockNumber.Sign() <= 0 || vault == (common.Address{}) {
		return p, fmt.Errorf("invalid Nemo snapshot")
	}
	header, err := t.ethrpcClient.GetETHClient().HeaderByNumber(ctx, resp.BlockNumber)
	if err != nil {
		return p, err
	}
	e.BlockTimestamp = header.Time
	e.Vault = hexutil.Encode(vault[:])
	var balances [2]*big.Int
	req = t.ethrpcClient.NewRequest().SetContext(ctx).SetBlockNumber(resp.BlockNumber)
	if overrides != nil {
		req.SetOverrides(overrides)
	}
	for i, token := range p.Tokens {
		req.AddCall(&ethrpc.Call{ABI: utilabi.Erc20ABI, Target: token.Address, Method: "balanceOf", Params: []any{vault}}, []any{&balances[i]})
		req.AddCall(&ethrpc.Call{ABI: utilabi.Erc20ABI, Target: token.Address, Method: "allowance", Params: []any{vault, common.HexToAddress(s.SwapAddress)}}, []any{&e.Allowances[i]})
	}
	if _, err = req.Aggregate(); err != nil {
		return p, err
	}
	if err = validateSnapshot(e, balances); err != nil {
		return p, err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return p, err
	}
	p.Extra = string(data)
	p.Reserves = entity.PoolReserves{balances[0].String(), balances[1].String()}
	p.Timestamp = int64(header.Time)
	p.BlockNumber = resp.BlockNumber.Uint64()
	return p, nil
}

func validateSnapshot(e Extra, balances [2]*big.Int) error {
	for _, v := range []*big.Int{e.Bid, e.Ask, e.KAsk, e.KBid, e.RhoAsk, e.RhoBid, e.QLighter, e.QAnchor, e.LastUpdatedTimestampMs, e.Allowances[0], e.Allowances[1], balances[0], balances[1]} {
		if v == nil || v.Sign() < 0 || v.BitLen() > 256 {
			return fmt.Errorf("invalid Nemo state")
		}
	}
	if e.Bid.BitLen() > 32 || e.Ask.BitLen() > 32 || e.QLighter.BitLen() > 128 || e.QAnchor.BitLen() > 128 || e.LastUpdatedTimestampMs.BitLen() > 64 {
		return fmt.Errorf("invalid Nemo parameters")
	}
	return nil
}
