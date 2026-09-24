package nemoprop

// PricingAddress must match the immutable pricing endpoint in SwapAddress's
// implementation. NemoSwap does not expose a pricing getter.
type Config struct {
	DexID          string `json:"dexID"`
	PricingAddress string `json:"pricingAddress"`
	SwapAddress    string `json:"swapAddress"`
}
