package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/legacycoin/standalone-miner/gpu"
	"github.com/legacycoin/standalone-miner/internal/chaincfg"
	"github.com/legacycoin/standalone-miner/internal/config"
	"github.com/legacycoin/standalone-miner/internal/wire"

	tea "github.com/charmbracelet/bubbletea"
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "LegacyCoin Standalone CPU Miner v1.0\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Modes:\n")
		fmt.Fprintf(os.Stderr, "  (no flags)       Start TUI in benchmark mode\n")
		fmt.Fprintf(os.Stderr, "  --rpc <url>      Start TUI in RPC mining mode\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "RPC mining flags:\n")
		fmt.Fprintf(os.Stderr, "  --rpc <url>          RPC URL (default http://localhost:19556)\n")
		fmt.Fprintf(os.Stderr, "  --pubkeyhash <hex>   Your 40-hex-char public key hash for coinbase\n")
		fmt.Fprintf(os.Stderr, "  --threads <n>        Number of mining threads (default: all CPUs)\n")
		fmt.Fprintf(os.Stderr, "  --rpcuser <user>     RPC username (optional, cookie auth by default)\n")
		fmt.Fprintf(os.Stderr, "  --rpcpass <pass>     RPC password (optional, cookie auth by default)\n")
		fmt.Fprintf(os.Stderr, "  --datadir <path>     Data directory for cookie auth (default ~/.legacycoin)\n")
		fmt.Fprintf(os.Stderr, "  --rig <name>         Rig name shown in TUI (default: hostname)\n")
		fmt.Fprintf(os.Stderr, "  --gpu                Enable GPU mining (requires CUDA/OpenCL build)\n")
		fmt.Fprintf(os.Stderr, "  --miner-id <n>       Miner index for multi-instance nonce partitioning (0-based)\n")
		fmt.Fprintf(os.Stderr, "  --total-miners <n>   Total number of miner instances sharing nonce space\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "TUI controls:\n")
		fmt.Fprintf(os.Stderr, "  [b]     Cycle mining mode: bench → rpc → stratum\n")
		fmt.Fprintf(os.Stderr, "  [+/-]   Increase / decrease thread count\n")
		fmt.Fprintf(os.Stderr, "  [r]     Restart current mining loop\n")
		fmt.Fprintf(os.Stderr, "  [q]     Quit\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "GPU build:\n")
		fmt.Fprintf(os.Stderr, "  make cuda      # Requires CUDA toolkit (nvcc)\n")
		fmt.Fprintf(os.Stderr, "  make opencl    # Requires OpenCL runtime\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Getting started:\n")
		fmt.Fprintf(os.Stderr, "  1. Start legacywallet (RPC cookie auto-generated)\n")
		fmt.Fprintf(os.Stderr, "  2. legacycoin-cli getnewaddress\n")
		fmt.Fprintf(os.Stderr, "  3. legacycoin-cli validateaddress <addr>  # copy pubkeyhash\n")
		fmt.Fprintf(os.Stderr, "  4. %s --rpc=http://localhost:19556 --pubkeyhash=<hex> --threads=12\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Algorithm: yespower 1.0  N=2048 r=32  Pers: %s\n", chaincfg.MainNet.YespowerPers)
	}
}

func main() {
	rpcURL := flag.String("rpc", "", "")
	rpcUser := flag.String("rpcuser", "", "")
	rpcPass := flag.String("rpcpass", "", "")
	dataDir := flag.String("datadir", "", "")
	pubKeyHash := flag.String("pubkeyhash", "", "")
	threads := flag.Int("threads", runtime.NumCPU(), "")
	rig := flag.String("rig", "", "")
	gpuEnable := flag.Bool("gpu", false, "Enable GPU mining (if available)")
	minerID := flag.Uint("miner-id", 0, "Miner index for multi-instance nonce partitioning (0-based)")
	totalMiners := flag.Uint("total-miners", 1, "Total number of miner instances sharing nonce space")
	flag.Parse()

	if *threads < 1 {
		*threads = 1
	}

	if *rpcURL != "" && *pubKeyHash == "" {
		fmt.Fprintf(os.Stderr, "ERROR: --pubkeyhash is required for RPC mining\n\n")
		fmt.Fprintf(os.Stderr, "Run '%s --help' for details.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "To get your public key hash:\n")
		fmt.Fprintf(os.Stderr, "  legacycoin-cli getnewaddress\n")
		fmt.Fprintf(os.Stderr, "  legacycoin-cli validateaddress <address>  # look for pubkeyhash\n")
		os.Exit(1)
	}

	if *pubKeyHash != "" {
		rawPKH, err := hex.DecodeString(*pubKeyHash)
		if err != nil || len(rawPKH) != 20 {
			fmt.Fprintf(os.Stderr, "ERROR: --pubkeyhash must be a 40-hex-char public key hash\n")
			os.Exit(1)
		}
	}

	m := initialModel()
	m.threads = *threads
	m.minerID = uint32(*minerID)
	m.totalMiners = uint32(*totalMiners)
	m.rpcURL = *rpcURL
	m.rpcUser = *rpcUser
	m.rpcPass = *rpcPass
	m.dataDir = *dataDir
	m.pubKeyHex = *pubKeyHash
	if *rig != "" {
		m.rigName = *rig
	}
	if *rpcURL != "" {
		m.mode = modeRPC
	}

	if *gpuEnable {
		gm := gpu.New()
		if gm.Available() {
			m.gpuMiner = gm
			for _, d := range gm.Devices() {
				m.gpuDevices = append(m.gpuDevices, d.Name)
			}
		}
	}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func defaultDataDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".legacycoin")
	}
	return config.DefaultDataDir()
}

func newRPCClient(url, user, pass, dataDir string) *RPCClient {
	if user == "" && pass == "" {
		if dataDir == "" {
			dataDir = defaultDataDir()
		}
		auth, err := config.LoadRPCCookieForDataDir(dataDir)
		if err == nil && auth.Enabled {
			user = auth.User
			pass = auth.Password
		}
	}
	if !strings.Contains(url, "://") {
		url = "http://" + url
	}
	if user != "" {
	}
	return NewRPCClient(url, user, pass)
}

func parseBlockHeader(raw []byte) (wire.BlockHeader, error) {
	if len(raw) < 80 {
		return wire.BlockHeader{}, fmt.Errorf("block too short: %d bytes", len(raw))
	}
	var h wire.BlockHeader
	h.Version = int32(binary.LittleEndian.Uint32(raw[0:4]))
	copy(h.PrevBlock[:], raw[4:36])
	copy(h.MerkleRoot[:], raw[36:68])
	h.Timestamp = binary.LittleEndian.Uint32(raw[68:72])
	h.Bits = binary.LittleEndian.Uint32(raw[72:76])
	h.Nonce = binary.LittleEndian.Uint32(raw[76:80])
	return h, nil
}

func writeNonce(raw []byte, nonce uint32) {
	binary.LittleEndian.PutUint32(raw[76:80], nonce)
}
