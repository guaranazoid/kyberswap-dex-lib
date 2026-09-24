package nemoprop

import _ "embed"

//go:embed abis/NemoPricing.json
var pricingABIData []byte

//go:embed abis/NemoSwap.json
var swapABIData []byte
