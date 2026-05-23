//go:build !cgo || legacycoin_experimental_pure_yespower

package pow

import (
	"crypto/hmac"
	"crypto/sha256"

	"github.com/legacycoin/standalone-miner/internal/chainhash"
	"github.com/legacycoin/standalone-miner/internal/wire"
)

func (p *PooledYespower) HashHeader(header wire.BlockHeader) (chainhash.Hash, error) {
	b, err := header.Bytes()
	if err != nil {
		return chainhash.Hash{}, err
	}
	return p.HashHeaderRaw(b)
}

func (p *PooledYespower) HashHeaderRaw(data []byte) (chainhash.Hash, error) {
	pers := p.hasher.Personalization
	if pers == "" {
		pers = "LegacyCoinPoW"
	}
	return pooledYespowerHash(data, pers, &p.scratch), nil
}

func pooledYespowerHash(input []byte, pers string, s *ypScratch) chainhash.Hash {
	srcHash := sha256.Sum256(input)
	bSize := 128 * yespowerR
	b := pbkdf2SHA256One(srcHash[:], []byte(pers), bSize)
	seedHash := b[:32]

	neededV := yespowerN * 2 * yespowerR
	if cap(s.v) < neededV {
		s.v = make([]ypBlock, neededV)
	}
	s.v = s.v[:neededV]

	neededXY := 2*yespowerR + 1
	if cap(s.xy) < neededXY {
		s.xy = make([]ypBlock, neededXY)
	}
	s.xy = s.xy[:neededXY]

	neededSMem := 3 * sBytes1 / 64
	if cap(s.smem) < neededSMem {
		s.smem = make([]ypBlock, neededSMem)
	}
	s.smem = s.smem[:neededSMem]

	ctx := &ypCtx{
		mem: s.smem,
		s0:  s.smem[:sBytes1/64],
		s1:  s.smem[sBytes1/64 : 2*sBytes1/64],
		s2:  s.smem[2*sBytes1/64:],
	}

	smix1(b, 1, len(s.smem)/2, s.smem, s.xy, nil)
	smix1(b, yespowerR, yespowerN, s.v, s.xy, ctx)
	nLoopRW := ((yespowerN + 2) / 3) + 1
	nLoopRW &^= 1
	smix2(b, yespowerR, yespowerN, nLoopRW, s.v, s.xy, ctx)

	var out chainhash.Hash
	mac := hmac.New(sha256.New, b[bSize-64:bSize])
	mac.Write(seedHash)
	copy(out[:], mac.Sum(nil))
	return out
}
