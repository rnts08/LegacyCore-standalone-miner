# ## PLAN

### CPU Mining

- Per-thread goroutines, one nonce per hash, TLS-scratch reuse
- CGO backend: yespower-opt.c with x86-64 ASM pwxform
- Fallback: PooledYespower (pure-Go, scratch reuse avoids 8 MB alloc)

### Multi-Instance Nonce Partitioning

- `--miner-id=N --total-miners=M` splits the 32-bit nonce space into M
  disjoint partitions, each of size 2^32 / M.
- Within each miner, CPU and GPU (if enabled) get separate slots so they
  never compete:
  - CPU threads own slots `[0, threads-1]`
  - GPU (if active) owns slot `threads`
- Stride formula: `nonce = minerID * slotsPerMiner + threadSlot`,
  stepping by `slotsPerMiner * totalMiners` (where `slotsPerMiner =
  threads + (1 if GPU else 0)`).
- Validation: the miner exits with a clear error if `minerID >= totalMiners`.

### Template Pipeline

- Background goroutine polls `getblocktemplate` every 500 ms, stores the
  latest block header in `tmplState`.
- When a block is submitted, a `pollTrigger` channel signals the background
  poller to fetch the next template immediately (~0 ms gap) instead of
  waiting for the next 500 ms tick.
- This eliminates idle time between blocks — especially important when
  multiple miners share one RPC endpoint.

### Fast PoW Check (Hot Path)

- `CheckProofOfWork` (which allocates big.Int per hash) is called once per
  template to produce a `[32]byte` target via `TargetFromBits`.
- The inner mining loop uses `CheckHashTarget`, which compares 32 bytes
  directly with no allocations — zero garbage per hash in the hot path.

### GPU Mining

- CUDA or OpenCL backend, selected by build tag
- One thread per nonce, ~8 MB scratch per thread in global memory
- Batch size auto-sized to 80% of GPU free memory
- `gpu.Miner` interface: `New()`, `Hash(batch)`, `Close()`
- Full yespower 1.0 kernel implemented for both CUDA and OpenCL
  (SHA256, Salsa20/8, pwxform, smix1/smix2)
- TUI shows GPU devices when `--gpu` is active

### Config File

- JSON file read before flag parsing; CLI flags override config values.
- Supported keys: `rpc`, `pubkeyhash`, `threads`, `rpcuser`, `rpcpass`,
  `datadir`, `rig`, `gpu`, `miner_id`, `total_miners`, `testnet`.

## Performance

| Variant               | Single-thread | Notes                    |
| --------------------- | ------------- | ------------------------ |
| baseline (x86-64 ASM) | 27 H/s        | i7-1265U                 |
| avx2                  | 21 H/s        | throttles on laptop      |
| native                | 26 H/s        | —                        |
| GPU (RTX 3060, est.)  | ~100–300 H/s  | depends on mem bandwidth |

## TODO

1. Stratum protocol client (press `b` in TUI to switch)
2. Windows/macOS system resource monitoring (non-Linux `/proc`)
3. GPU kernel optimization (shared memory pwxform, warp-cooperative SMix)
4. CPU auto-detect: `make detect` target that reads `/proc/cpuinfo` and
   recommends optimal CGO_CFLAGS
5. ASUS/PCIe reset recovery for GPU hangs
