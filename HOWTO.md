# How To Use the Standalone Miner

## Prerequisites

- **Go 1.25+** — see `go.mod` for the exact toolchain version
- **C compiler** (gcc/clang) — required for the optimized yespower CGO backend
- A running **legacywallet** / **legacycoind** daemon with RPC enabled (for solo mining)
- **Optional:** CUDA toolkit (`nvcc`) for GPU mining on NVIDIA GPUs
- **Optional:** OpenCL runtime (`libOpenCL.so`) for GPU mining on AMD/Intel GPUs

---

## 1. Build the Miner

Before you begin building the miner, make sure that you have the LegacyCore daemon
compiled and installed on your system. Follow the instructions at
[legacybtc/LegacyCore](https://github.com/legacybtc/LegacyCore) — the standalone
miner connects via RPC using cookie auth.

```bash
cd standalone-miner

# Production build (CGO + x86-64 ASM yespower)
make

# To find out which model of cpu you have and what capabilities it has you can run 
make detect

# CPU-tuned builds (see §3. CPU Tuning below)
make avx2      # enable AVX2 code path
make native    # -march=native for this specific CPU

# Disable x86-64 inline ASM (debug / portability)
make purec

# Pure-Go fallback (~2 H/s, testing only)
make pure

# Static binary (no libc at runtime)
make static

# GPU builds (see §5. GPU Mining below)
make cuda      # NVIDIA — requires CUDA toolkit + nvcc
make opencl    # AMD/Intel — requires OpenCL runtime
```

Output: `./legacy-miner`.

### Project Structure

The entry point is at `cmd/legacy-miner/main.go`. All application logic lives
in `internal/app/` as a single `app` package. The build system handles this
transparently — you only need `make` or `go build ./cmd/legacy-miner`.

---

## 2. Configure the Daemon

The daemon auto-generates a random RPC cookie at `~/.legacycoin/.cookie`
on first start. The miner reads this automatically — no manual credentials.

| Setting  | Default           |
| -------- | ----------------- |
| RPC bind | `127.0.0.1:19556` |
| Auth     | cookie file       |

If needed, edit `~/.legacycoin/legacycoin.conf`:

```ini
rpcbind=0.0.0.0:19556
# rpctls=1
# rpctlscert=/path/to/cert.pem
# rpctlskey=/path/to/key.pem
```

For TLS, use `https://` in `--rpc`.

---

## 3. Get Your Public Key Hash

```bash
legacycoin-cli getnewaddress
legacycoin-cli validateaddress <address>
# → look for "pubkeyhash" in JSON output
```

Example:

```json
{
  "pubkeyhash": "a3f1c8d2e5b7a9c0d1e2f3a4b5c6d7e8f9a0b1c2"
}
```

---

## 4. Run the Miner (TUI)

```bash
# Benchmark mode (default)
./legacy-miner

# Solo RPC mining (cookie auth)
./legacy-miner \
  --rpc=http://localhost:19556 \
  --pubkeyhash=a3f1c8d2e5b7a9c0d1e2f3a4b5c6d7e8f9a0b1c2 \
  --threads=12

# Named rig
./legacy-miner --rig=miner1 --rpc=http://localhost:19556 --pubkeyhash=<hex>

# With GPU (if built with CUDA or OpenCL support)
./legacy-miner --gpu

# From JSON config file
./legacy-miner --config=miner.json

# Testnet
./legacy-miner --testnet --rpc=http://localhost:19656 --pubkeyhash=<hex>
```

### Multi-Instance (Nonce Partitioning)

Run multiple miner processes against the same daemon without duplicating work:

```bash
# Terminal 1 — miner 0 of 4
./legacy-miner --config=miner0.json

# Terminal 2 — miner 1 of 4
./legacy-miner --config=miner1.json

# Terminal 3 — miner 2 of 4
./legacy-miner --config=miner2.json

# Terminal 4 — miner 3 of 4
./legacy-miner --config=miner3.json
```

Each miner gets a disjoint slice of the 32-bit nonce space. CPU threads
and GPU (if enabled) within the same process also get separate slots so
they never compete.

### TUI Layout

```
┌──────────────────────────────────────────────────────────────┐
│  LegacyCoin CPU Miner  —  rig1                               │
├──────────────────────────────────────────────────────────────┤
│  sparkline: ▁▂▃▄▅▆▇█  42 H/s                                 │
│  threads: 12  backend: cgo-c-reference + GPU[GeForce RTX…]   │
│  CPU: 85%  MEM: 128 MB                                       │
├──────────────────────────────────────────────────────────────┤
│  [bench]  rpc  stratum                                       │
│  RPC → localhost:19556  height=398                           │
│  found=1 accepted=1 rejected=0 stale=0                       │
│  uptime: 5m42s                                               │
├──────────────────────────────────────────────────────────────┤
│  [b] mode  [+/-] threads  [r] restart  [q] quit              │
└──────────────────────────────────────────────────────────────┘
```

### TUI Controls

| Key       | Action                      |
| --------- | --------------------------- |
| `b`       | Cycle bench → rpc → stratum |
| `+` / `=` | Increase CPU threads        |
| `-` / `_` | Decrease CPU threads        |
| `r`       | Restart mining loop         |
| `q`       | Quit                        |

### Flags

| Flag                 | Default       | Description                                                 |
| -------------------- | ------------- | ----------------------------------------------------------- |
| `--config <path>`    | —             | JSON config file (defaults for all flags)                   |
| `--rpc <url>`        | —             | RPC URL for solo mining                                     |
| `--pubkeyhash <hex>` | —             | 40-hex-char public key hash                                 |
| `--threads <n>`      | all CPUs      | CPU thread count                                            |
| `--rpcuser <user>`   | —             | RPC username (cookie by default)                            |
| `--rpcpass <pass>`   | —             | RPC password (cookie by default)                            |
| `--datadir <path>`   | ~/.legacycoin | Data dir for cookie auth                                    |
| `--rig <name>`       | hostname      | Rig name in TUI                                             |
| `--gpu`              | false         | Enable GPU mining                                           |
| `--miner-id <n>`     | 0             | Miner index for multi-instance nonce partitioning (0-based) |
| `--total-miners <n>` | 1             | Total number of miner instances sharing nonce space         |
| `--testnet`          | false         | Use testnet parameters (pers, bits, RPC port 19656)         |

---

## 5. Config File

A JSON config file sets flag defaults. CLI flags override config values.
Example `miner.json`:

```json
{
  "rpc": "http://192.168.1.100:19556",
  "pubkeyhash": "a3f1c8d2e5b7a9c0d1e2f3a4b5c6d7e8f9a0b1c2",
  "threads": 8,
  "rig": "miner1",
  "gpu": false,
  "miner_id": 0,
  "total_miners": 4,
  "testnet": false
}
```

Supported keys: `rpc`, `pubkeyhash`, `threads`, `rpcuser`, `rpcpass`,
`datadir`, `rig`, `gpu`, `miner_id`, `total_miners`, `testnet`.

**Tip for multi-instance:** create one JSON file per miner, varying only
`rig`, `miner_id`, and optionally `threads`. Then launch with
`--config=minerN.json`.

---

## 6. CPU Tuning

### 6.1 How yespower Uses the CPU

Yespower 1.0 is memory-hard (~8 MB scratch per hash). The dominant cost
is the `pwxform` S-box lookup loop, which is heavily pointer-chasing.
The reference C code provides three code paths, selected by compiler flags:

| Path              | Flag              | Speed                                           |
| ----------------- | ----------------- | ----------------------------------------------- |
| x86-64 inline ASM | default (no flag) | Fastest on modern x86                           |
| SSE2 generic      | `-DNO_X86_64_ASM` | ~20% slower, portable                           |
| AVX/AVX2 SIMD     | `-mavx2`          | Mixed — helps Salsa20 but prefixes hurt pwxform |
| AVX-512           | `-mavx512vl`      | Best on server SKUs with AVX-512VL              |

### 6.2 Detect Your CPU Features

```bash
# See what your CPU supports
grep flags /proc/cpuinfo | head -1

# Key flags to look for:
#   avx2       → try "make avx2"
#   avx512vl   → try CGO_CFLAGS="-mavx512vl" go build .
#   sse4_1     → baseline (always present on x86-64)
```

### 6.3 Make Targets

```bash
make               # baseline — x86-64 ASM + SSE2
make avx2          # -mavx2 (Sandy Bridge+ / Bulldozer+)
make native        # -march=native (auto-detect & optimize)
make purec         # -DNO_X86_64_ASM (pure C, no inline ASM)

# Custom CGO_CFLAGS
CGO_CFLAGS="-mavx2 -mtune=znver4" make   # AMD Zen 4 specific
CGO_CFLAGS="-mavx512vl" make              # Intel Ice Lake+ / AMD Zen 4
CGO_CFLAGS="-march=native -O3" make       # aggressive native tuning
```

### 6.4 Microarchitecture Guide

| CPU Family                  | Recommended Flags              | Expected vs Baseline    |
| --------------------------- | ------------------------------ | ----------------------- |
| Intel Nehalem/Westmere      | `make` (baseline)              | 1.0×                    |
| Intel Sandy/Ivy Bridge      | `make avx2`                    | 1.0–1.1×                |
| Intel Haswell/Broadwell     | `make native`                  | 1.0–1.15×               |
| Intel Skylake–Rocket Lake   | `make native`                  | 1.05–1.2×               |
| Intel Alder/Raptor Lake     | `make` or `make native`        | 1.0× (AVX2 clocks down) |
| Intel Ice Lake+ (server)    | `CGO_CFLAGS="-mavx512vl"`      | 1.1–1.3×                |
| AMD Zen 1/2                 | `make avx2`                    | 1.1–1.2×                |
| AMD Zen 3/4                 | `make native`                  | 1.15–1.3×               |
| AMD Zen 4 (Genoa/Bergamo)   | `CGO_CFLAGS="-mavx512vl"`      | 1.2–1.4×                |
| ARM (Apple M1/M2, Graviton) | `CGO_CFLAGS="-DNO_X86_64_ASM"` | N/A — pure C path       |

### 6.5 Benchmark All Variants

```bash
make bench-cpu         # baseline
make bench-avx2        # AVX2
make bench-native      # -march=native
make bench-purec       # pure C (no ASM)
```

Each will build with that flag, run for a few seconds, and print the hashrate.

### 6.6 Performance Data (i7-1265U Alder Lake)

| Build    | Single-thread | Notes                                |
| -------- | ------------- | ------------------------------------ |
| baseline | 27 H/s        | x86-64 ASM, SSE2                     |
| avx2     | 21 H/s        | AVX2 enabled but throttles on laptop |
| native   | 26 H/s        | same as avx2                         |

Desktop/server CPUs with adequate cooling will see avx2/native 10–30%
faster than baseline. **Always bench your own hardware.**

---

## 7. GPU Mining

### 7.1 Architecture

Yespower 1.0 requires ~8 MB of scratch memory per hash attempt.
Each GPU thread processes one nonce independently with scratch in
global memory. Batch size is auto-sized to 80% of available GPU memory
(~800 threads on an 8 GB card).

### 7.2 Supported Backends

| Backend | GPU                           | Build Command |
| ------- | ----------------------------- | ------------- |
| CUDA    | NVIDIA (sm_61+, compute 6.1+) | `make cuda`   |
| OpenCL  | AMD, Intel, NVIDIA            | `make opencl` |

### 7.3 Prerequisites

**CUDA:**

```bash
# Verify nvcc is available
which nvcc || apt install nvidia-cuda-toolkit   # Debian/Ubuntu
# Or download from: https://developer.nvidia.com/cuda-downloads

# Build
make cuda
```

**OpenCL:**

```bash
# Verify OpenCL is available
which clinfo || apt install clinfo opencl-headers  # Debian/Ubuntu
# GPU driver (AMD ROCm / Intel compute-runtime / NVIDIA)

# Build
make opencl
```

### 7.4 Running with GPU

```bash
# Benchmark mode with GPU
./legacy-miner --gpu

# RPC mining with GPU + CPU
./legacy-miner \
  --gpu \
  --rpc=http://localhost:19556 \
  --pubkeyhash=<hex> \
  --threads=8
```

When `--gpu` is set, the TUI shows detected GPU devices in the
backend line:

```
backend: cgo-c-reference + GPU[GeForce RTX 3060]
```

GPU threads run alongside CPU threads — both contribute to the
hashrate. Use `+`/`-` to adjust CPU threads independently.

### 7.5 GPU Performance Expectations

The memory-hard nature of yespower (~8 MB scratch per hash, heavy
random-access S-box lookups) limits GPU throughput on most cards.
The primary bottleneck is global memory bandwidth.

| GPU               | VRAM  | Est. Batch | Est. Hashrate |
| ----------------- | ----- | ---------- | ------------- |
| RTX 3060          | 12 GB | ~1100      | ~3–5× CPU     |
| RTX 3080          | 10 GB | ~900       | ~5–10× CPU    |
| RTX 4090          | 24 GB | ~2200      | ~10–20× CPU   |
| RX 6700 XT        | 12 GB | ~1100      | ~3–6× CPU     |
| A100 (datacenter) | 80 GB | ~8000      | ~20–40× CPU   |

Actual performance depends on memory bandwidth, PCIe generation, and
driver overhead. **Benchmark with `--gpu` on your hardware.**

### 7.6 Troubleshooting (GPU)

| Symptom                 | Cause                   | Fix                                     |
| ----------------------- | ----------------------- | --------------------------------------- |
| `make cuda` fails       | no nvcc                 | Install CUDA toolkit                    |
| CUDA `no devices found` | no NVIDIA GPU or driver | `nvidia-smi` to verify                  |
| `make opencl` fails     | no OpenCL headers       | `apt install opencl-headers`            |
| OpenCL `no devices`     | no GPU driver           | Install ROCm / compute-runtime          |
| GPU hashrate equals CPU | batch too small         | Increase GPU memory (batch auto-scales) |

---

## 8. Multi-Machine (Distributed) Mining

Point multiple miners at the same daemon RPC endpoint. Each miner must
have a unique `--miner-id` in `[0, totalMiners-1]`:

```bash
# Miner 0 of 4
./legacy-miner \
  --rig=miner0 \
  --miner-id=0 --total-miners=4 \
  --rpc=http://192.168.1.100:19556 --pubkeyhash=<hex>

# Miner 1 of 4
./legacy-miner \
  --rig=miner1 \
  --miner-id=1 --total-miners=4 \
  --rpc=http://192.168.1.100:19556 --pubkeyhash=<hex>
```

Or use config files for cleaner management:

```bash
# miner0.json — edit rig, miner_id per instance
./legacy-miner --config=miner0.json
./legacy-miner --config=miner1.json
```

Each miner calls `getblocktemplate` independently, searches a disjoint
nonce range, and submits when found. After submission the background
template poller is triggered immediately (no 500 ms wait). The hot-path
PoW check uses direct byte comparison instead of big.Int allocation.

---

## 9. Troubleshooting

| Symptom                                   | Cause              | Fix                                   |
| ----------------------------------------- | ------------------ | ------------------------------------- |
| `connection refused`                      | Daemon not running | Start legacywallet, verify `rpcbind`  |
| `RPC error -32603`                        | Daemon not ready   | Wait for sync                         |
| `pubkeyhash not mine`                     | Wrong wallet       | Use address from this wallet          |
| `miner-id must be less than total-miners` | Bad config         | Ensure `miner-id < total-miners`      |
| Hashrate drops                            | Thermal throttling | Reduce threads, improve cooling       |
| `could not open a new TTY`                | Non-interactive    | Use real terminal or `ssh -t`         |
| C compilation errors                      | Missing C compiler | Install `build-essential` / Xcode CLI |
| Sparkline shows `â`                       | Terminal locale    | Set `LANG=en_US.UTF-8`                |
