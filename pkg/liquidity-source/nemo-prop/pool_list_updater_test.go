package nemoprop

import (
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestDecodeMarkets(t *testing.T) {
	t.Parallel()

	token0 := common.HexToAddress("0xcbb7c0000ab88b473b1f5afd9ef808440eed33bf")
	token1 := common.HexToAddress("0x4200000000000000000000000000000000000006")
	data, err := hex.DecodeString("cbb7c0000ab88b473b1f5afd9ef808440eed33bf000000000000000005f5e100" +
		"4200000000000000000000000000000000000006000000000de0b6b3a7640000")
	require.NoError(t, err)

	markets, err := decodeMarkets(data)
	require.NoError(t, err)
	require.Len(t, markets, 2)
	require.Equal(t, token0, markets[0].Token)
	require.Equal(t, token1, markets[1].Token)
	require.Equal(t, "000000000000000005f5e100", hex.EncodeToString(markets[0].AssetUnitFactor[:]))
	require.Equal(t, "000000000de0b6b3a7640000", hex.EncodeToString(markets[1].AssetUnitFactor[:]))
}

func TestDecodeMarketsRejectsPartialEntry(t *testing.T) {
	t.Parallel()
	_, err := decodeMarkets(make([]byte, marketDataEntrySize-1))
	require.Error(t, err)
}
