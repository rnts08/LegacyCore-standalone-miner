package main

import (
	"github.com/legacycoin/standalone-miner/internal/chainhash"
	"github.com/legacycoin/standalone-miner/internal/pow"
	"github.com/legacycoin/standalone-miner/internal/wire"
)

type Hasher interface {
	HashHeader(header wire.BlockHeader) (chainhash.Hash, error)
	HashHeaderRaw(data []byte) (chainhash.Hash, error)
}

var _ Hasher = pow.YespowerHasher{}
var _ Hasher = (*pow.PooledYespower)(nil)

func newHasher(pers string) Hasher {
	if pow.BackendName() == "pure-go-experimental" {
		return pow.NewPooledYespower(pers)
	}
	return pow.YespowerHasher{Personalization: pers}
}
