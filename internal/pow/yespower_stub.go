//go:build !cgo || legacycoin_experimental_pure_yespower

package pow

import (
	"github.com/legacycoin/standalone-miner/internal/chainhash"
	"github.com/legacycoin/standalone-miner/internal/wire"
)

func (h YespowerHasher) pers() string {
	if h.Personalization == "" {
		return "LegacyCoinPoW"
	}
	return h.Personalization
}

// HashHeader uses the pure-Go yespower implementation only for non-CGO builds
// or explicit experimental/debug builds.
func (h YespowerHasher) HashHeader(header wire.BlockHeader) (chainhash.Hash, error) {
	b, err := header.Bytes()
	if err != nil {
		return chainhash.Hash{}, err
	}
	return h.HashHeaderRaw(b)
}

// HashHeaderRaw hashes pre-serialized 80-byte block header data.
func (h YespowerHasher) HashHeaderRaw(data []byte) (chainhash.Hash, error) {
	return yespowerHash(data, h.pers()), nil
}

func BackendName() string {
	return "pure-go-experimental"
}
