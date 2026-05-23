package chainhash

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const HashSize = 32

type Hash [HashSize]byte

func DoubleHashB(b []byte) Hash {
	first := sha256.Sum256(b)
	second := sha256.Sum256(first[:])
	return Hash(second)
}

func FromString(s string) (Hash, error) {
	var h Hash
	if len(s) >= 2 && s[:2] == "0x" {
		s = s[2:]
	}
	if len(s) != HashSize*2 {
		return h, errors.New("invalid hash length")
	}
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return h, err
	}
	for i := 0; i < HashSize; i++ {
		h[i] = decoded[HashSize-1-i]
	}
	return h, nil
}

func (h Hash) String() string {
	var reversed [HashSize]byte
	for i := 0; i < HashSize; i++ {
		reversed[i] = h[HashSize-1-i]
	}
	return hex.EncodeToString(reversed[:])
}

func (h Hash) IsZero() bool {
	return h == Hash{}
}
