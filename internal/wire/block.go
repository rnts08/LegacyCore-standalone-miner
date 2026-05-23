package wire

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/legacycoin/standalone-miner/internal/chainhash"
)

type BlockHeader struct {
	Version    int32
	PrevBlock  chainhash.Hash
	MerkleRoot chainhash.Hash
	Timestamp  uint32
	Bits       uint32
	Nonce      uint32
}

func (h *BlockHeader) Serialize(w io.Writer) error {
	if err := binary.Write(w, binary.LittleEndian, h.Version); err != nil {
		return err
	}
	if _, err := w.Write(h.PrevBlock[:]); err != nil {
		return err
	}
	if _, err := w.Write(h.MerkleRoot[:]); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, h.Timestamp); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, h.Bits); err != nil {
		return err
	}
	return binary.Write(w, binary.LittleEndian, h.Nonce)
}

func ReadBlockHeader(r io.Reader) (BlockHeader, error) {
	var h BlockHeader
	if err := binary.Read(r, binary.LittleEndian, &h.Version); err != nil {
		return h, err
	}
	if _, err := io.ReadFull(r, h.PrevBlock[:]); err != nil {
		return h, err
	}
	if _, err := io.ReadFull(r, h.MerkleRoot[:]); err != nil {
		return h, err
	}
	if err := binary.Read(r, binary.LittleEndian, &h.Timestamp); err != nil {
		return h, err
	}
	if err := binary.Read(r, binary.LittleEndian, &h.Bits); err != nil {
		return h, err
	}
	if err := binary.Read(r, binary.LittleEndian, &h.Nonce); err != nil {
		return h, err
	}
	return h, nil
}

func (h *BlockHeader) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := h.Serialize(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (h *BlockHeader) Hash() (chainhash.Hash, error) {
	b, err := h.Bytes()
	if err != nil {
		return chainhash.Hash{}, err
	}
	return chainhash.DoubleHashB(b), nil
}
