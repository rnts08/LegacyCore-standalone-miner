//go:build cgo && !legacycoin_experimental_pure_yespower

package pow

import (
	"github.com/legacycoin/standalone-miner/internal/chainhash"
	"github.com/legacycoin/standalone-miner/internal/wire"
)

func (p *PooledYespower) HashHeader(header wire.BlockHeader) (chainhash.Hash, error) {
	_ = p.scratch
	return p.hasher.HashHeader(header)
}

func (p *PooledYespower) HashHeaderRaw(data []byte) (chainhash.Hash, error) {
	_ = p.scratch
	return p.hasher.HashHeaderRaw(data)
}
