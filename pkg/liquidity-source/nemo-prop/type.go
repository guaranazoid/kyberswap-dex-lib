package nemoprop

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type StaticExtra struct {
	PricingAddress  string `json:"pricingAddress"`
	SwapAddress     string `json:"swapAddress"`
	MarketIndex     uint64 `json:"marketIndex"`
	AssetUnitFactor string `json:"assetUnitFactor"`
}

type Extra struct {
	HasOverrides           bool        `json:"hasOverrides,omitempty"`
	Bid                    *big.Int    `json:"bid"`
	Ask                    *big.Int    `json:"ask"`
	KAsk                   *big.Int    `json:"kAsk"`
	KBid                   *big.Int    `json:"kBid"`
	RhoAsk                 *big.Int    `json:"rhoAsk"`
	RhoBid                 *big.Int    `json:"rhoBid"`
	QLighter               *big.Int    `json:"qLighter"`
	QAnchor                *big.Int    `json:"qAnchor"`
	LastUpdatedTimestampMs *big.Int    `json:"lastUpdatedTimestampMs"`
	BlockTimestamp         uint64      `json:"blockTimestamp"`
	Vault                  string      `json:"vault"`
	Allowances             [2]*big.Int `json:"allowances"`
}

type market struct {
	Token           common.Address
	AssetUnitFactor [12]byte
}

type PoolsListUpdaterMetadata struct {
	Offset int `json:"offset"`
}

type SwapInfo struct {
	IndexIn   int
	AmountIn  *big.Int
	AmountOut *big.Int
}
