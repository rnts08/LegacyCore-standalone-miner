## Standalone CPU/GPU Miner for LegacyCore

Standalone yespower 1.0 miner for LegacyCoin (LBTC).  Connects to a
running `legacywallet` / `legacycoind` RPC for solo mining, or runs
in benchmark mode without a daemon.

### Quick Start

#### Read HOWTO.md for how to build the legacycoin rpc daemon and the legacy-miner for best results.

```bash
make                    # build (CGO + x86-64 ASM)
./legacy-miner          # benchmark mode (TUI)
./legacy-miner --rpc=http://localhost:19556 --pubkeyhash=<hex>
```

### Multi-Instance (Distributed)

```bash
./legacy-miner --config=miner0.json   # miner 0 of 4
./legacy-miner --config=miner1.json   # miner 1 of 4
```

### GPU (CUDA / OpenCL)

```bash
make cuda              # NVIDIA
make opencl            # AMD/Intel
./legacy-miner --gpu   # run with GPU
```

### Documentation

| File       | Contents                                          |
| ---------- | ------------------------------------------------- |
| `HOWTO.md` | Full usage guide, tuning, multi-machine, GPU      |
| `PLAN.md`  | Architecture, build targets, TODO, completed work |

### Support

If you want to support the work of the maintainer feel free to donate to
the following addresses:

- **ETH/ERC20:** `0x968cC7D93c388614f620Ef812C5fdfe64029B92d`
- **SOL:** `HB2o6q6vsW5796U5y7NxNqA7vYZW1vuQjpAHDo7FAMG8`
- **BTC:** `bc1qkmzc6d49fl0edyeynezwlrfqv486nmk6p5pmta`
- **LTC:** `ltc1q0ahxru7nwgey64agffr7x89swekj7sz8stqc6x`
- **XRP:** `rUW7Q64vR4PwDM3F27etd6ipxK8MtuxsFs`

You can also buy me a coffe on https://buymeacoffee.com/timhbergsta, support me with Ko-Fi https://ko-fi.com/rnts08 or simply send any tips to *timh@tbtechvn.com* via PayPal. 
