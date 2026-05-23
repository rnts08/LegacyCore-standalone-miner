package consensus

import (
	"errors"
	"math/big"

	"github.com/legacycoin/standalone-miner/internal/chainhash"
)

var (
	ErrTargetTooHigh = errors.New("target exceeds proof-of-work limit")
	ErrHighHash      = errors.New("hash does not meet target")
)

var PowLimit = new(big.Int).Rsh(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)), 1)

const DGWv3PastBlocks = 24

type BlockWindowEntry struct {
	Height int32
	Time   uint32
	Bits   uint32
}

func CompactToBig(compact uint32) *big.Int {
	size := compact >> 24
	word := compact & 0x007fffff
	result := new(big.Int).SetUint64(uint64(word))
	if size <= 3 {
		result.Rsh(result, uint(8*(3-size)))
	} else {
		result.Lsh(result, uint(8*(size-3)))
	}
	if compact&0x00800000 != 0 {
		result.Neg(result)
	}
	return result
}

func BigToCompact(n *big.Int) uint32 {
	if n.Sign() == 0 {
		return 0
	}
	n = new(big.Int).Set(n)
	negative := n.Sign() < 0
	if negative {
		n.Abs(n)
	}
	size := uint(len(n.Bytes()))
	var compact uint32
	if size <= 3 {
		compact = uint32(n.Uint64() << (8 * (3 - size)))
	} else {
		tmp := new(big.Int).Rsh(n, uint(8*(size-3)))
		compact = uint32(tmp.Uint64())
	}
	if compact&0x00800000 != 0 {
		compact >>= 8
		size++
	}
	compact |= uint32(size) << 24
	if negative && compact&0x007fffff != 0 {
		compact |= 0x00800000
	}
	return compact
}

func DarkGravityWaveV3(recent []BlockWindowEntry, targetSpacingSeconds int64, powLimit *big.Int, powLimitBits uint32) uint32 {
	if len(recent) < DGWv3PastBlocks || targetSpacingSeconds <= 0 {
		return powLimitBits
	}

	pastDifficultyAverage := new(big.Int)
	pastDifficultyAveragePrev := new(big.Int)
	for count := 1; count <= DGWv3PastBlocks; count++ {
		target := CompactToBig(recent[count-1].Bits)
		if target.Sign() <= 0 || target.Cmp(powLimit) > 0 {
			target = new(big.Int).Set(powLimit)
		}
		if count == 1 {
			pastDifficultyAverage.Set(target)
		} else {
			delta := new(big.Int).Sub(target, pastDifficultyAveragePrev)
			delta.Div(delta, big.NewInt(int64(count)))
			pastDifficultyAverage.Add(pastDifficultyAveragePrev, delta)
		}
		pastDifficultyAveragePrev.Set(pastDifficultyAverage)
	}

	actualTimespan := int64(recent[0].Time) - int64(recent[DGWv3PastBlocks-1].Time)
	targetTimespan := int64(DGWv3PastBlocks) * targetSpacingSeconds
	if actualTimespan < targetTimespan/3 {
		actualTimespan = targetTimespan / 3
	}
	if actualTimespan > targetTimespan*3 {
		actualTimespan = targetTimespan * 3
	}

	next := new(big.Int).Mul(pastDifficultyAverage, big.NewInt(actualTimespan))
	next.Div(next, big.NewInt(targetTimespan))
	if next.Cmp(powLimit) > 0 {
		next.Set(powLimit)
	}
	return BigToCompact(next)
}

func HashToBig(hash chainhash.Hash) *big.Int {
	var reversed [chainhash.HashSize]byte
	for i := 0; i < chainhash.HashSize; i++ {
		reversed[i] = hash[chainhash.HashSize-1-i]
	}
	return new(big.Int).SetBytes(reversed[:])
}

func CheckProofOfWork(hash chainhash.Hash, bits uint32) error {
	target := CompactToBig(bits)
	if target.Sign() <= 0 || target.Cmp(PowLimit) > 0 {
		return ErrTargetTooHigh
	}
	if HashToBig(hash).Cmp(target) > 0 {
		return ErrHighHash
	}
	return nil
}
