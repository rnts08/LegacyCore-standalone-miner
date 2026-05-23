# Standalone CPU/GPU Miner for yespower 1.0 (LegacyCoin)

## Build
```bash
make            # baseline CGO + x86-64 ASM
make avx2       # enable AVX2 code path
make native     # -march=native
make purec      # pure C (no inline ASM)
make pure       # pure-Go fallback
make static     # static binary
make cuda       # NVIDIA GPU (requires nvcc)
make opencl     # AMD/Intel GPU (requires OpenCL runtime)
```

## Usage
```bash
./legacy-miner                                 # TUI bench
./legacy-miner --rpc=... --pubkeyhash=...      # RPC mining
./legacy-miner --gpu                           # with GPU
```

## TUI Controls
- `b` — cycle bench / rpc / stratum
- `+`/`-` — adjust CPU threads
- `r` — restart mining
- `q` — quit

## Files
```
standalone-miner/
├── Makefile              # cpu/avx2/native/purec/pure/static/cuda/opencl
├── go.mod / go.sum
├── main.go               # flags + TUI program start
├── miner.go              # mineBlock, benchHashrate (legacy CLI)
├── tui.go                # Bubble Tea model, view, update, loops
├── sysmon.go             # /proc CPU + MEM
├── rpc.go                # JSON-RPC 1.0 client
├── pool.go               # Hasher interface
├── HOWTO.md              # full usage guide
├── PLAN.md               # this file
├── gpu/                  # GPU mining package
│   ├── gpu.go            # Miner interface (Hash, Init, Close)
│   ├── gpu_cuda.go       # CUDA CGO bindings (tag: cuda)
│   ├── gpu_opencl.go     # OpenCL CGO bindings (tag: opencl)
│   ├── gpu_stub.go       # fallback when no GPU tag is set
│   ├── bridge.h          # common C interface
│   ├── cuda_bridge.cu    # CUDA kernel + host bridge (nvcc)
│   ├── opencl_bridge.c   # OpenCL host bridge (gcc)
│   └── yespower_kernel.cl # OpenCL kernel source
└── internal/
    ├── chainhash/        # Hash type
    ├── chaincfg/         # MainNet params
    ├── consensus/        # CheckProofOfWork
    ├── config/           # RPC cookie auth
    ├── wire/             # BlockHeader (stripped)
    └── pow/              # C yespower + PooledYespower
```

## Architecture

### CPU Mining
- Per-thread goroutines, one nonce per hash, TLS-scratch reuse
- CGO backend: yespower-opt.c with x86-64 ASM pwxform
- Fallback: PooledYespower (pure-Go, scratch reuse avoids 8 MB alloc)

### GPU Mining
- CUDA or OpenCL backend, selected by build tag
- One thread per nonce, ~8 MB scratch per thread in global memory
- Batch size auto-sized to 80% of GPU free memory
- `gpu.Miner` interface: `New()`, `Hash(batch)`, `Close()`
- TUI shows GPU devices when `--gpu` is active

## Performance
| Variant | Single-thread | Notes |
|---------|--------------|-------|
| baseline (x86-64 ASM) | 27 H/s | i7-1265U |
| avx2 | 21 H/s | throttles on laptop |
| native | 26 H/s | — |
| GPU (RTX 3060, est.) | ~100–300 H/s | depends on mem bandwidth |

## TODO (Priority Order)
1. Stratum protocol client (press `b` in TUI to switch)
2. Persistent config file (`--config`)
3. Windows/macOS system resource monitoring (non-Linux `/proc`)
4. GPU kernel optimization (shared memory pwxform, warp-cooperative SMix)
5. CPU auto-detect: `make detect` target that reads `/proc/cpuinfo` and
   recommends optimal CGO_CFLAGS
6. Batch multi-block RPC mining (pipeline getblocktemplate across submits)
7. ASUS/PCIe reset recovery for GPU hangs
