//go:build !cuda && !opencl

package gpu

func gpuInit() int { return 0 }

func gpuDeviceInfo(_ int, _ *string, _ *uint64) int { return -1 }

func gpuMaxBatch() int { return 0 }

func gpuHash(_ []byte, _ []byte, _ int, _ string) error {
	return errNoGPU
}

func gpuReset() int { return 0 }

func gpuClose() {}

var errNoGPU = &errNoGPUBackend{}

type errNoGPUBackend struct{}

func (e *errNoGPUBackend) Error() string {
	return "gpu: no backend available (build with -tags cuda or -tags opencl)"
}
