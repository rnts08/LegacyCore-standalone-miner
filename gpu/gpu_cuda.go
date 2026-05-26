//go:build cuda

package gpu

/*
#cgo LDFLAGS: ${SRCDIR}/cuda_bridge.o -lcuda -lcudart -lpthread -ldl
#include "../gpu/bridge.h"
*/
import "C"
import (
	"fmt"
	"unsafe"
)

func gpuInit() int {
	n := C.gpu_init()
	return int(n)
}

func gpuDeviceInfo(id int, name *string, mem *uint64) int {
	var cname [256]C.char
	var cmem C.size_t
	rc := C.gpu_device_info(C.int(id), &cname[0], C.size_t(len(cname)), &cmem)
	if rc != 0 {
		return int(rc)
	}
	*name = C.GoString(&cname[0])
	if mem != nil {
		*mem = uint64(cmem)
	}
	return 0
}

func gpuMaxBatch() int {
	return int(C.gpu_max_batch)
}

func gpuHash(headers []byte, outputs []byte, count int, pers string) error {
	cHeaders := (*C.uint8_t)(unsafe.Pointer(&headers[0]))
	cOutputs := (*C.uint8_t)(unsafe.Pointer(&outputs[0]))
	cPers := (*C.uint8_t)(unsafe.Pointer(unsafe.StringData(pers)))

	rc := C.gpu_hash(cHeaders, cOutputs, C.int(count), cPers, C.int(len(pers)))
	if rc != 0 {
		errStr := C.GoString(C.gpu_error_string())
		return fmt.Errorf("gpu_hash failed: %s", errStr)
	}
	return nil
}

func gpuLastError() string {
	return C.GoString(C.gpu_error_string())
}

func gpuClose() {
	C.gpu_close()
}
