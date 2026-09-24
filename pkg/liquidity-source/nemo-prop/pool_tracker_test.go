package nemoprop

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnapshotValidation(t *testing.T) {
	p := testSimulator(t)
	balances := [2]*big.Int{p.Info.Reserves[0], p.Info.Reserves[1]}
	require.NoError(t, validateSnapshot(p.Extra, balances))
	e := p.Extra
	e.Allowances[0] = nil
	require.Error(t, validateSnapshot(e, balances))
	e = p.Extra
	e.QLighter = new(big.Int).Lsh(big.NewInt(1), 128)
	require.Error(t, validateSnapshot(e, balances))
	e = p.Extra
	e.RhoAsk = nil // Old six-field snapshots must be refreshed, not interpreted as zero.
	require.Error(t, validateSnapshot(e, balances))
	e = p.Extra
	e.QAnchor = nil
	require.Error(t, validateSnapshot(e, balances))
	e = p.Extra
	e.Bid = new(big.Int).Lsh(big.NewInt(1), 32)
	require.Error(t, validateSnapshot(e, balances))
	e = p.Extra
	e.LastUpdatedTimestampMs = new(big.Int).Lsh(big.NewInt(1), 64)
	require.Error(t, validateSnapshot(e, balances))
	balances[0] = big.NewInt(-1)
	require.Error(t, validateSnapshot(p.Extra, balances))
}
func TestCurrentABI(t *testing.T) {
	require.Len(t, pricingABI.Methods["getAmountOut"].Inputs, 5)
	require.Len(t, pricingABI.Methods["getAmountIn"].Inputs, 5)
	require.Len(t, pricingABI.Methods["getMarketState"].Outputs, 8)
	for _, output := range pricingABI.Methods["getMarketState"].Outputs {
		require.Equal(t, "uint256", output.Type.String())
	}
	require.Equal(t, "uint256", pricingABI.Methods["lastUpdatedTimestampMs"].Outputs[0].Type.String())
	require.Contains(t, pricingABI.Methods, "lastUpdatedTimestampMs")
	require.Contains(t, swapABI.Methods, "owner")
	require.NotContains(t, swapABI.Methods, "netOutflow")
}
