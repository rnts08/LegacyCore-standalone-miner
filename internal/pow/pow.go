package pow

import (
	"errors"

	"github.com/legacycoin/standalone-miner/internal/chainhash"
	"github.com/legacycoin/standalone-miner/internal/wire"
)

var ErrYespowerUnavailable = errors.New("yespower hash is not implemented yet")

type Hasher interface {
	HashHeader(header wire.BlockHeader) (chainhash.Hash, error)
	HashHeaderRaw(data []byte) (chainhash.Hash, error)
}

type YespowerHasher struct {
	Personalization string
}
