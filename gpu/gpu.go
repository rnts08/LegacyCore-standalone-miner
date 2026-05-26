// Package gpu provides yespower 1.0 GPU mining via CUDA or OpenCL.
//
// Build tags:
//   cuda   — use CUDA backend (requires nvcc + CUDA toolkit)
//   opencl — use OpenCL backend (requires OpenCL runtime)
//
// Without either tag, the package reports "no GPU backend".
package gpu

import "fmt"

// DeviceInfo describes a GPU device.
type DeviceInfo struct {
	Name      string
	GlobalMem uint64 // bytes
}

// Miner manages GPU mining state.
type Miner struct {
	devices   []DeviceInfo
	maxBatch  int
	available bool
}

// New creates a GPU miner and initializes the backend.
// Returns a miner (possibly with no available devices).
func New() *Miner {
	m := &Miner{}
	count := gpuInit()
	if count <= 0 {
		return m
	}
	for i := 0; i < count; i++ {
		di := DeviceInfo{}
		var mem uint64
		if gpuDeviceInfo(i, &di.Name, &mem) == 0 {
			di.GlobalMem = mem
			m.devices = append(m.devices, di)
		}
	}
	m.maxBatch = gpuMaxBatch()
	m.available = count > 0
	return m
}

// Available returns true if a GPU backend is initialized.
func (m *Miner) Available() bool { return m.available }

// Devices returns the list of detected GPU devices.
func (m *Miner) Devices() []DeviceInfo { return m.devices }

// MaxBatch returns the maximum batch size (limited by GPU memory).
func (m *Miner) MaxBatch() int { return m.maxBatch }

// Reset resets the GPU device (recovery after timeout/error).
func (m *Miner) Reset() {
	m.available = false
	m.devices = nil
	m.maxBatch = 0
	count := gpuReset()
	if count > 0 {
		for i := 0; i < count; i++ {
			di := DeviceInfo{}
			var mem uint64
			if gpuDeviceInfo(i, &di.Name, &mem) == 0 {
				di.GlobalMem = mem
				m.devices = append(m.devices, di)
			}
		}
		m.maxBatch = gpuMaxBatch()
		m.available = count > 0
	}
}

// Hash submits a batch of block headers (each 80 bytes) to the GPU
// and returns the resulting hashes.
// headers — slice of [count][80]byte serialized headers.
// pers    — personalization string (e.g. "LegacyCoinPoW").
// Returns [count][32]byte hashes.
func (m *Miner) Hash(headers [][80]byte, pers string) ([][32]byte, error) {
	if !m.available {
		return nil, fmt.Errorf("gpu: no backend available")
	}
	count := len(headers)
	out := make([][32]byte, count)

	// Process in batches if needed
	for offset := 0; offset < count; {
		batch := count - offset
		if batch > m.maxBatch {
			batch = m.maxBatch
		}
		flat := make([]byte, batch*80)
		for i := 0; i < batch; i++ {
			copy(flat[i*80:(i+1)*80], headers[offset+i][:])
		}
		outFlat := make([]byte, batch*32)
		if err := gpuHash(flat, outFlat, batch, pers); err != nil {
			return nil, err
		}
		for i := 0; i < batch; i++ {
			copy(out[offset+i][:], outFlat[i*32:(i+1)*32])
		}
		offset += batch
	}
	return out, nil
}

// Close releases GPU resources.
func (m *Miner) Close() {
	if m.available {
		gpuClose()
		m.available = false
	}
}
