package nemoprop

import (
	"bytes"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

var (
	pricingABI abi.ABI
	swapABI    abi.ABI
)

func init() {
	for _, item := range []struct {
		target *abi.ABI
		data   []byte
	}{{&pricingABI, pricingABIData}, {&swapABI, swapABIData}} {
		parsed, err := abi.JSON(bytes.NewReader(item.data))
		if err != nil {
			panic(err)
		}
		*item.target = parsed
	}
}
