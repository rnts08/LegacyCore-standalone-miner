//go:build cgo && !legacycoin_experimental_pure_yespower

package pow

/*
#cgo CFLAGS: -I${SRCDIR}/yespower -O3
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "yespower/yespower.h"
#include "yespower/sha256.c"
#include "yespower/yespower-opt.c"

static int legacy_yespower_hash(const unsigned char* input, size_t inputlen, const char* pers, size_t perslen, unsigned char* output) {
	yespower_binary_t dst;
	yespower_params_t params = {YESPOWER_1_0, 2048, 32, (const uint8_t*)pers, perslen};
	if (yespower_tls((const uint8_t*)input, inputlen, &params, &dst) != 0) {
		memset(output, 0xff, 32);
		return -1;
	}
	memcpy(output, dst.uc, 32);
	return 0;
}
*/
import "C"

import (
	"sync"
	"unsafe"

	"github.com/legacycoin/standalone-miner/internal/chainhash"
	"github.com/legacycoin/standalone-miner/internal/wire"
)

const defaultPers = "LegacyCoinPoW"

var (
	cachedDefaultCPers     *C.char
	cachedDefaultCPersOnce sync.Once
)

func getDefaultCPers() *C.char {
	cachedDefaultCPersOnce.Do(func() {
		cachedDefaultCPers = C.CString(defaultPers)
	})
	return cachedDefaultCPers
}

func (h YespowerHasher) pers() string {
	if h.Personalization == "" {
		return defaultPers
	}
	return h.Personalization
}

func (h YespowerHasher) hashRaw(data []byte) (chainhash.Hash, error) {
	pers := h.pers()
	var cPers *C.char
	if pers == defaultPers {
		cPers = getDefaultCPers()
	} else {
		cPers = C.CString(pers)
		defer C.free(unsafe.Pointer(cPers))
	}

	var out chainhash.Hash
	inPtr := (*C.uchar)(unsafe.Pointer(&data[0]))
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))
	rc := C.legacy_yespower_hash(inPtr, C.size_t(len(data)), cPers, C.size_t(len(pers)), outPtr)
	if rc != 0 {
		return chainhash.Hash{}, ErrYespowerUnavailable
	}
	return out, nil
}

// HashHeader uses the bundled C yespower 1.0 reference implementation.
func (h YespowerHasher) HashHeader(header wire.BlockHeader) (chainhash.Hash, error) {
	b, err := header.Bytes()
	if err != nil {
		return chainhash.Hash{}, err
	}
	return h.hashRaw(b)
}

// HashHeaderRaw hashes pre-serialized 80-byte block header data.
func (h YespowerHasher) HashHeaderRaw(data []byte) (chainhash.Hash, error) {
	return h.hashRaw(data)
}

func BackendName() string {
	return "cgo-c-reference"
}
