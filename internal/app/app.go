package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/legacycoin/standalone-miner/gpu"
	"github.com/legacycoin/standalone-miner/internal/chaincfg"
	"github.com/legacycoin/standalone-miner/internal/pow"
)

const maxHist = 55

var barRunes = []rune("▁▂▃▄▅▆▇█")

type ModeType int

const (
	ModeBench ModeType = iota
	ModeRPC
	ModeStratum
)

func (m ModeType) String() string {
	switch m {
	case ModeBench:
		return "BENCH"
	case ModeRPC:
		return "RPC"
	case ModeStratum:
		return "STRATUM"
	}
	return "?"
}

type Config struct {
	Mode            ModeType
	Threads         int
	MinerID         uint32
	TotalMiners     uint32
	RPCURL          string
	RPCUser         string
	RPCPass         string
	DataDir         string
	PubKeyHash      string
	RigName         string
	Pers            string
	PostGenesisBits uint32
	GPUMiner        *gpu.Miner
	GPUDevices      []string
}

type sharedCount struct {
	hashCount atomic.Uint64
}

type templateState struct {
	mu       sync.Mutex
	height   int
	base     [80]byte
	raw      []byte
	bits     uint32
	stale    atomic.Bool
	hasStale bool
}

type statsMsg struct {
	hashrate float64
	found    uint64
	accepted uint64
	rejected uint64
	stale    uint64
	height   int
	bits     string
	running  bool
}

type tickMsg time.Time

type Model struct {
	rigName         string
	pers            string
	postGenesisBits uint32
	backend         string

	mode        ModeType
	threads     int
	minerID     uint32
	totalMiners uint32

	rpcURL    string
	rpcUser   string
	rpcPass   string
	dataDir   string
	pubKeyHex string

	history  []float64
	hashPrev uint64
	found    uint64
	accepted uint64
	rejected uint64
	stale    uint64
	height   int
	bits     string
	lastStat time.Time

	cpuJiffies uint64
	cpuTime    time.Time
	cpuPercent float64
	memMB      float64
	startTime  time.Time

	count        *sharedCount
	miningCtx    context.Context
	miningCancel context.CancelFunc
	statsCh      chan statsMsg

	gpuMiner    *gpu.Miner
	gpuDevices  []string
	gpuName     string
	gpuCount    atomic.Uint64
	gpuHashPrev uint64
	gpuHistory  []float64
	gpuActive   atomic.Bool

	tmplState   *templateState
	pollTrigger chan struct{}
}

func NewModel(cfg Config) *Model {
	pers := cfg.Pers
	if pers == "" {
		pers = chaincfg.MainNet.YespowerPers
	}
	postBits := cfg.PostGenesisBits
	if postBits == 0 {
		postBits = chaincfg.MainNet.PostGenesisBits
	}
	rigName := cfg.RigName
	if rigName == "" {
		rigName = hostname()
	}
	threads := cfg.Threads
	if threads < 1 {
		threads = 1
	}

	gpuName := ""
	if len(cfg.GPUDevices) > 0 {
		gpuName = cfg.GPUDevices[0]
	}

	m := &Model{
		rigName:         rigName,
		pers:            pers,
		postGenesisBits: postBits,
		backend:         pow.BackendName(),
		mode:            cfg.Mode,
		threads:         threads,
		totalMiners:     cfg.TotalMiners,
		minerID:         cfg.MinerID,
		rpcURL:          cfg.RPCURL,
		rpcUser:         cfg.RPCUser,
		rpcPass:         cfg.RPCPass,
		dataDir:         cfg.DataDir,
		pubKeyHex:       cfg.PubKeyHash,
		history:         make([]float64, 0, maxHist),
		gpuHistory:      make([]float64, 0, maxHist),
		statsCh:         make(chan statsMsg, 64),
		startTime:       time.Now(),
		count:           &sharedCount{},
		tmplState:       &templateState{},
		pollTrigger:     make(chan struct{}, 1),
		gpuMiner:        cfg.GPUMiner,
		gpuDevices:      cfg.GPUDevices,
		gpuName:         gpuName,
	}
	m.miningCtx, m.miningCancel = context.WithCancel(context.Background())
	return m
}
