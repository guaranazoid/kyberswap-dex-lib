package nemoprop

import (
	"errors"
	"time"

	"github.com/KyberNetwork/kyberswap-dex-lib/pkg/valueobject"
)

const (
	DexType = valueobject.ExchangeNemoProp

	// defaultGas is a placeholder until calibrated against the executor's
	// chosen entrypoint.
	defaultGas = 150_000

	// defaultFeedFresh is how long a snapshot quotes undecayed. The feed
	// server resends faster than this even when nothing changed, so an older
	// snapshot means the feed is unhealthy and its quotes start decaying.
	defaultFeedFresh = time.Second

	// decayScale is 10000 bps * 1000 ms: decay is DecayBps per second of
	// staleness, measured in milliseconds.
	decayScale = 10_000 * 1_000

	// marketEntrySize is one getMarkets_v1 entry: [asset: 160 bits][unit: 96 bits].
	marketEntrySize = 32
)

var (
	// ErrNoFeed is returned when there is no quotable feed snapshot
	// covering the pool's market. Nemo is quoted from its live feed only.
	ErrNoFeed = errors.New("no quotable nemo feed snapshot")

	ErrInvalidMarkets = errors.New("invalid getMarkets_v1 encoding")
)
