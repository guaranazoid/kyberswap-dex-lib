package nemoprop

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/KyberNetwork/ethrpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/goccy/go-json"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/entity"
	poollist "github.com/KyberNetwork/kyberswap-dex-lib/pkg/source/pool/list"
)

type PoolsListUpdater struct {
	config       *Config
	ethrpcClient *ethrpc.Client
}

var _ = poollist.RegisterFactoryCE(DexType, NewPoolsListUpdater)

func NewPoolsListUpdater(config *Config, ethrpcClient *ethrpc.Client) *PoolsListUpdater {
	return &PoolsListUpdater{config: config, ethrpcClient: ethrpcClient}
}

func (u *PoolsListUpdater) GetNewPools(ctx context.Context, metadataBytes []byte) ([]entity.Pool, []byte, error) {
	offset, err := parseOffset(metadataBytes)
	if err != nil {
		return nil, metadataBytes, err
	}

	var result struct {
		Base    common.Address
		Markets []byte
	}
	_, err = u.ethrpcClient.NewRequest().SetContext(ctx).
		AddCall(&ethrpc.Call{ABI: pricingABI, Target: u.config.PricingAddress, Method: "getMarkets_v1"},
			[]any{&result}).Aggregate()
	if err != nil {
		return nil, metadataBytes, err
	}

	markets, err := decodeMarkets(result.Markets)
	if err != nil {
		return nil, metadataBytes, err
	}
	if offset >= len(markets) {
		return nil, metadataBytes, nil
	}

	baseAddress := hexutil.Encode(result.Base[:])
	pools := make([]entity.Pool, 0, len(markets)-offset)
	for i, m := range markets[offset:] {
		staticExtra, err := json.Marshal(StaticExtra{
			PricingAddress: normalizeAddress(u.config.PricingAddress), SwapAddress: normalizeAddress(u.config.SwapAddress),
			MarketIndex: uint64(offset + i), AssetUnitFactor: new(big.Int).SetBytes(m.AssetUnitFactor[:]).String(),
		})
		if err != nil {
			return nil, metadataBytes, err
		}
		marketAddress := hexutil.Encode(m.Token[:])
		pools = append(pools, entity.Pool{
			Address:   fmt.Sprintf("%s_%s_%s", normalizeAddress(u.config.SwapAddress), baseAddress, marketAddress),
			Exchange:  u.config.DexID,
			Type:      DexType,
			Timestamp: time.Now().Unix(),
			Reserves:  entity.PoolReserves{"0", "0"},
			Tokens: []*entity.PoolToken{
				{Address: baseAddress, Swappable: true},
				{Address: marketAddress, Swappable: true},
			},
			Extra:       "{}",
			StaticExtra: string(staticExtra),
		})
	}

	newMetadata, err := json.Marshal(PoolsListUpdaterMetadata{Offset: len(markets)})
	if err != nil {
		return nil, metadataBytes, err
	}
	return pools, newMetadata, nil
}

func parseOffset(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	var metadata PoolsListUpdaterMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return 0, err
	}
	if metadata.Offset < 0 {
		return 0, fmt.Errorf("invalid negative offset %d", metadata.Offset)
	}
	return metadata.Offset, nil
}

func decodeMarkets(data []byte) ([]market, error) {
	if len(data)%marketDataEntrySize != 0 {
		return nil, fmt.Errorf("invalid market data length %d", len(data))
	}
	markets := make([]market, 0, len(data)/marketDataEntrySize)
	for i := 0; i < len(data); i += marketDataEntrySize {
		var factor [12]byte
		copy(factor[:], data[i+common.AddressLength:i+marketDataEntrySize])
		if new(big.Int).SetBytes(factor[:]).Sign() == 0 {
			return nil, fmt.Errorf("zero asset unit factor")
		}
		markets = append(markets, market{Token: common.BytesToAddress(data[i : i+common.AddressLength]), AssetUnitFactor: factor})
	}
	return markets, nil
}

func normalizeAddress(address string) string {
	a := common.HexToAddress(address)
	return hexutil.Encode(a[:])
}
