# Future Plans

## Stratum Protocol Client

The TUI has a stratum mode slot (`[b]` cycles bench → rpc → stratum) but
no implementation yet. Stratum would allow pooled mining instead of solo
RPC, reducing variance and providing steady payouts.

### Design considerations

- **Protocol:** [Stratum v1](https://en.bitcoin.it/wiki/Stratum_mining_protocol)
  (JSON-RPC over TCP). Yespower 1.0 shares the same block header and
  share-target structure as Bitcoin, so standard Stratum flows apply.
- **Template → job mapping:** Subscribe (`mining.subscribe`), authorize
  (`mining.authorize`), receive jobs (`mining.notify`), submit shares
  (`mining.submit`).
- **Extranonce:** LegacyCoin uses the same 32-bit nonce field in the
  block header. Stratum's extranonce/extranonce2 pattern works as-is.
- **Share difficulty:** The pool sends a per-job `nbits` target; shares
  below pool difficulty are submitted but below the network target are
  not valid blocks. The miner should check both.
- **Reconnection:** Exponential backoff with jitter (like the RPC poller),
  preserve subscription state across reconnect.
- **TUI integration:** Reuse `templateState` and `statsCh` — treat each
  `mining.notify` as a new template, submit shares through the same path.

### Stratum vs RPC mode differences

| Concern                | RPC (solo)                   | Stratum (pool)                    |
| ---------------------- | ---------------------------- | --------------------------------- |
| Template source        | HTTP `getblocktemplate`      | TCP `mining.notify`               |
| Submit path            | HTTP `submitblock`           | TCP `mining.submit`               |
| Share target           | Network bits from template   | Per-job `nbits` from pool         |
| Connection             | Short-lived HTTP             | Long-lived TCP, keepalive         |
| Auth                   | Cookie or user/pass          | `mining.authorize` + worker name  |
| Extra nonce            | None (single 32-bit nonce)   | Extranonce + extranonce2          |

### Priority

Highest — unlocks pooled mining, the most requested feature.

---

## CPU Assembly / Performance Features

The yespower C reference provides several code paths, but the Go/CGO
interface itself has room for optimization.

### 1. Auto-detected ISA dispatch at runtime

Currently the code path is fixed at compile time (`make` / `make avx2` /
`make native` / `make purec`). A runtime CPUID check could select the
best path without needing separate binaries:

- `cpuid` detects AVX2, AVX-512VL, AVX-512BW, VAES, etc.
- A function-pointer table dispatches to the appropriate `pwxform` / smix
  variant.
- Already implemented in yespower-opt.c via `cpuid()` — but the Go build
  always includes the `#define`-selected path. Making it selectable at
  startup would require compiling multiple object variants and loading
  via build tags or dlopen.

### 2. ARM64 / NEON / SVE

Yespower has no upstream ARM64 SIMD path. The C reference uses x86-64
inline assembly for `pwxform`. An ARM64 port would need:

- NEON-based `pwxform` (16× S-box lookup interleaved with SHA256).
- SVE (Scalable Vector Extension) variant if available.
- Verified against the reference C implementation.

### 3. Pure-Go backend performance

The `PooledYespower` (pure-Go experimental) backend currently achieves
~2 H/s — too slow for real mining but useful for validation. Potential
improvements:

- Hand-tuned SHA256 with unsafe pointer arithmetic (avoids `hash.Hash`
  interface overhead).
- Fixed-size stack allocations instead of heap-allocated scratch (~8 MB
  per hash, avoid GC pressure).
- Copy-on-write scratch pool: the pure-Go PooledYespower already reuses
  scratch from a sync.Pool. Further gains come from inlining the Salsa20
  and pwxform cores.

### 4. CGO call overhead reduction

Each `HashHeaderRaw` call crosses the Go/C boundary. Batching multiple
nonces into a single C call (similar to the GPU batch approach) could
amortize overhead:

- Submit a buffer of N headers to C, get N hashes back.
- Requires changes in yespower-opt.c (or a new entry point).
- Gains are likely <10 % since each hash already takes ~30 ms in C.

### 5. Shared-memory pwxform for GPU

Current GPU kernels place the 8 MB scratch in global memory. A
warp-cooperative smix that loads the S-box into shared memory can
reduce latency:

- Requires `__shared__` pwxform table, rotated through warps.
- Limited to GPUs with enough shared memory (≥48 KB per block).
- Could 2–3× GPU hashrate on cards with ample shared memory (e.g.,
  Turing+).

### Priority

| Item                          | Effort | Impact   |
| ----------------------------- | ------ | -------- |
| Runtime ISA dispatch          | Medium | Medium   |
| ARM64 NEON                    | Large  | High     |
| Pure-Go optimization          | Medium | Low–Med  |
| CGO batching                  | Small  | Low      |
| GPU shared-memory pwxform     | Large  | High     |
