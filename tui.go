package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/legacycoin/standalone-miner/gpu"
	"github.com/legacycoin/standalone-miner/internal/chaincfg"
	"github.com/legacycoin/standalone-miner/internal/chainhash"
	"github.com/legacycoin/standalone-miner/internal/consensus"
	"github.com/legacycoin/standalone-miner/internal/pow"
	"github.com/legacycoin/standalone-miner/internal/wire"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	maxHist = 60
)

var barRunes = []rune("▁▂▃▄▅▆▇█")

type modeType int

const (
	modeBench modeType = iota
	modeRPC
	modeStratum
)

func (m modeType) String() string {
	switch m {
	case modeBench:
		return "BENCH"
	case modeRPC:
		return "RPC"
	case modeStratum:
		return "STRATUM"
	}
	return "?"
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

type sharedCount struct {
	hashCount atomic.Uint64
}

type templateState struct {
	mu       sync.Mutex
	height   int
	base     [80]byte
	raw      [80]byte
	bits     uint32
	stale    atomic.Bool
	hasStale bool
}

type model struct {
	rigName string
	pers    string
	backend string

	mode       modeType
	threads    int
	minerID    uint32
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

	count      *sharedCount
	miningCtx    context.Context
	miningCancel context.CancelFunc
	statsCh      chan statsMsg

	gpuMiner   *gpu.Miner
	gpuDevices []string

	tmplState  *templateState
	pollTrigger chan struct{}
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

func initialModel() model {
	pers := chaincfg.MainNet.YespowerPers
	m := model{
		rigName:     hostname(),
		pers:        pers,
		backend:     pow.BackendName(),
		mode:        modeBench,
		threads:     runtime.NumCPU(),
		totalMiners: 1,
		history:     make([]float64, 0, maxHist),
		statsCh:     make(chan statsMsg, 64),
		startTime:   time.Now(),
		count:       &sharedCount{},
		tmplState:   &templateState{},
		pollTrigger: make(chan struct{}, 1),
	}
	m.miningCtx, m.miningCancel = context.WithCancel(context.Background())
	return m
}

func (m *model) startMining() {
	m.miningCancel()
	ctx, cancel := context.WithCancel(context.Background())
	m.miningCtx = ctx
	m.miningCancel = cancel
	m.count.hashCount.Store(0)
	m.hashPrev = 0
	m.history = m.history[:0]
	m.lastStat = time.Now()

	go func() {
		switch m.mode {
		case modeBench:
			m.runBenchLoop(ctx)
		case modeRPC:
			m.runRPCLoop(ctx)
		case modeStratum:
			m.runBenchLoop(ctx)
		}
	}()

	if m.gpuMiner != nil && m.gpuMiner.Available() {
		switch m.mode {
		case modeBench:
			go m.runGPUBenchLoop(ctx)
		case modeRPC:
			go m.runRPCGPULoop(ctx)
		}
	}
}

func (m *model) runBenchLoop(ctx context.Context) {
	bits := chaincfg.MainNet.PostGenesisBits
	base := serializeHeader(wire.BlockHeader{
		Version:   1,
		Bits:      bits,
		Timestamp: uint32(time.Now().Unix()),
	})
	hasher := newHasher(m.pers)

	gpuSlots := uint32(0)
	if m.gpuMiner != nil && m.gpuMiner.Available() {
		gpuSlots = 1
	}
	slotsPerMiner := uint32(m.threads) + gpuSlots
	step := slotsPerMiner * m.totalMiners
	for w := 0; w < m.threads; w++ {
		go func(start uint32) {
			var buf [80]byte
			for nonce := m.minerID*slotsPerMiner + start; ; nonce += step {
				select {
				case <-ctx.Done():
					return
				default:
				}
				copy(buf[:], base[:])
				binary.LittleEndian.PutUint32(buf[76:], nonce)
				hasher.HashHeaderRaw(buf[:])
				m.count.hashCount.Add(1)
			}
		}(uint32(w))
	}

	<-ctx.Done()
}

// serializeHeader serializes a BlockHeader into a fixed-size [80]byte
// for use with HashHeaderRaw in hot paths. Panics on error (shouldn't happen).
func serializeHeader(h wire.BlockHeader) (out [80]byte) {
	if h.Bits == 0 {
		h.Bits = chaincfg.MainNet.PostGenesisBits
	}
	b, err := h.Bytes()
	if err != nil {
		panic("serializeHeader: " + err.Error())
	}
	copy(out[:], b)
	return
}

func (m *model) runGPUBenchLoop(ctx context.Context) {
	gm := m.gpuMiner
	batchSize := gm.MaxBatch()
	if batchSize < 1 {
		batchSize = 256
	}
	header := wire.BlockHeader{
		Version:   1,
		PrevBlock: [32]byte{},
		Timestamp: uint32(time.Now().Unix()),
		Bits:      chaincfg.MainNet.PostGenesisBits,
	}

	gpuSlot := uint32(m.threads)
	slotsPerMiner := gpuSlot + 1
	step := slotsPerMiner * m.totalMiners
	nonce := m.minerID*slotsPerMiner + gpuSlot
	headers := make([][80]byte, batchSize)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		for i := range headers {
			h := header
			h.Nonce = nonce
			nonce += step
			b, _ := h.Bytes()
			copy(headers[i][:], b)
		}

		_, err := gm.Hash(headers, m.pers)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		m.count.hashCount.Add(uint64(batchSize))
	}
}

func (m *model) runRPCGPULoop(ctx context.Context) {
	gm := m.gpuMiner
	batchSize := gm.MaxBatch()
	if batchSize < 1 {
		batchSize = 256
	}
	headers := make([][80]byte, batchSize)
	client := newRPCClient(m.rpcURL, m.rpcUser, m.rpcPass, m.dataDir)
	if client == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		m.tmplState.mu.Lock()
		base := m.tmplState.base
		bits := m.tmplState.bits
		hasTmpl := m.tmplState.hasStale
		m.tmplState.mu.Unlock()

		if !hasTmpl {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}

		gpuSlot := uint32(m.threads)
		slotsPerMiner := gpuSlot + 1
		step := slotsPerMiner * m.totalMiners
		nonce := m.minerID*slotsPerMiner + gpuSlot
		for i := range headers {
			copy(headers[i][:], base[:])
			binary.LittleEndian.PutUint32(headers[i][76:], nonce)
			nonce += step
		}

		results, err := gm.Hash(headers, m.pers)
		if err != nil {
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}

		m.count.hashCount.Add(uint64(batchSize))

		for i, hash := range results {
			if consensus.CheckProofOfWork(chainhash.Hash(hash), bits) == nil {
				nonceFound := binary.LittleEndian.Uint32(headers[i][76:])
				m.tmplState.mu.Lock()
				raw := m.tmplState.raw
				height := m.tmplState.height
				m.tmplState.mu.Unlock()

				writeNonce(raw[:], nonceFound)
				m.found++

				if err := client.SubmitBlock(hex.EncodeToString(raw[:])); err != nil {
					errStr := err.Error()
					if strings.Contains(errStr, "already") || strings.Contains(errStr, "stale") {
						m.stale++
					} else {
						m.rejected++
					}
				} else {
					m.accepted++
				}

				select {
				case m.pollTrigger <- struct{}{}:
				default:
				}

				select {
				case m.statsCh <- statsMsg{
					found:    m.found,
					accepted: m.accepted,
					rejected: m.rejected,
					stale:    m.stale,
					height:   height,
					bits:     fmt.Sprintf("%08x", bits),
					running:  true,
				}:
				case <-ctx.Done():
					return
				default:
				}
				break
			}
		}
	}
}

func (m *model) runRPCLoop(ctx context.Context) {
	client := newRPCClient(m.rpcURL, m.rpcUser, m.rpcPass, m.dataDir)
	if client == nil {
		return
	}

	// Background template poller
	pollCtx, pollCancel := context.WithCancel(ctx)
	defer pollCancel()
	go func() {
		backoff := time.Second
		for {
			select {
			case <-pollCtx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			case <-m.pollTrigger:
			}

			tmpl, err := client.GetBlockTemplate(m.pubKeyHex)
			if err != nil {
				if backoff < 60*time.Second {
					backoff *= 2
				}
				continue
			}
			backoff = time.Second

			raw, err := hex.DecodeString(tmpl.Hex)
			if err != nil {
				continue
			}

			header, err := parseBlockHeader(raw)
			if err != nil {
				continue
			}
			header.Nonce = 0
			base := serializeHeader(header)
			bits, _ := strconv.ParseUint(tmpl.Bits, 16, 32)

			m.tmplState.mu.Lock()
			if tmpl.Height != m.tmplState.height {
				m.tmplState.base = base
				m.tmplState.bits = uint32(bits)
				m.tmplState.height = tmpl.Height
				copy(m.tmplState.raw[:], raw[:80])
				m.tmplState.stale.Store(true)
				m.tmplState.hasStale = true
			}
			m.tmplState.mu.Unlock()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Wait for first valid template
		m.tmplState.mu.Lock()
		hasStale := m.tmplState.hasStale
		m.tmplState.mu.Unlock()
		if !hasStale {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}

		m.tmplState.mu.Lock()
		base := m.tmplState.base
		bits := m.tmplState.bits
		height := m.tmplState.height
		m.tmplState.stale.Store(false)
		m.tmplState.mu.Unlock()

		select {
		case m.statsCh <- statsMsg{
			height:  height,
			bits:    fmt.Sprintf("%08x", bits),
			running: true,
		}:
		case <-ctx.Done():
			return
		default:
		}

		gpuSlots := uint32(0)
		if m.gpuMiner != nil && m.gpuMiner.Available() {
			gpuSlots = 1
		}
		slotsPerMiner := uint32(m.threads) + gpuSlots
		hasher := newHasher(m.pers)
		miningCtx, miningCancel := context.WithCancel(ctx)
		nonce, ok := mineBlockHashed(hasher, base, bits, m.threads, miningCtx, &m.count.hashCount, m.minerID, m.totalMiners, slotsPerMiner, &m.tmplState.stale)
		miningCancel()

		if !ok {
			continue
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		// Write nonce into template raw bytes and encode for submission
		m.tmplState.mu.Lock()
		raw := m.tmplState.raw
		m.tmplState.mu.Unlock()

		writeNonce(raw[:], nonce)
		blockHex := hex.EncodeToString(raw[:])

		m.found++

		if err := client.SubmitBlock(blockHex); err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "already") || strings.Contains(errStr, "stale") {
				m.stale++
			} else {
				m.rejected++
			}
		} else {
			m.accepted++
		}

		// Trigger immediate template poll — don't wait 500ms
		select {
		case m.pollTrigger <- struct{}{}:
		default:
		}

		select {
		case m.statsCh <- statsMsg{
			found:    m.found,
			accepted: m.accepted,
			rejected: m.rejected,
			stale:    m.stale,
			height:   height,
			bits:     fmt.Sprintf("%08x", bits),
			running:  true,
		}:
		case <-ctx.Done():
			return
		default:
		}
	}
}

func mineBlockHashed(hasher Hasher, base [80]byte, bits uint32, workers int, ctx context.Context, hashCount *atomic.Uint64, minerID, totalMiners, slotsPerMiner uint32, stale *atomic.Bool) (uint32, bool) {
	type result struct {
		nonce uint32
	}
	resc := make(chan result, 1)
	var wg sync.WaitGroup

	step := slotsPerMiner * totalMiners
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(start uint32) {
			defer wg.Done()
			var buf [80]byte
			for nonce := minerID*slotsPerMiner + start; ; nonce += step {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if stale.Load() {
					return
				}
				copy(buf[:], base[:])
				binary.LittleEndian.PutUint32(buf[76:], nonce)
				hash, err := hasher.HashHeaderRaw(buf[:])
				if err != nil {
					continue
				}
				hashCount.Add(1)
				if consensus.CheckProofOfWork(hash, bits) == nil {
					select {
					case resc <- result{nonce}:
					default:
					}
					return
				}
			}
		}(uint32(w))
	}

	go func() {
		wg.Wait()
		close(resc)
	}()

	for res := range resc {
		return res.nonce, true
	}
	return 0, false
}

func (m model) Init() tea.Cmd {
	m.startMining()
	return tea.Batch(
		m.statsCmd(),
		tickCmd(),
	)
}

func (m model) statsCmd() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.statsCh
		if !ok {
			return nil
		}
		return msg
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func updateHashrate(m *model) {
	now := time.Now()
	elapsed := now.Sub(m.lastStat).Seconds()
	if elapsed < 0.5 {
		return
	}
	current := m.count.hashCount.Load()
	rate := float64(current-m.hashPrev) / elapsed
	m.hashPrev = current
	m.lastStat = now

	m.history = append(m.history, rate)
	if len(m.history) > maxHist {
		m.history = m.history[len(m.history)-maxHist:]
	}
}

func updateSysStats(m *model) {
	m.cpuPercent, _ = readCPUPercent(&m.cpuJiffies, &m.cpuTime)
	m.memMB, _ = readMemMB()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.miningCancel()
			return m, tea.Quit
		case "b", "B":
			m.miningCancel()
			m.mode = (m.mode + 1) % 3
			m.found = 0
			m.accepted = 0
			m.rejected = 0
			m.stale = 0
			m.startMining()
			return m, tea.Batch(m.statsCmd(), tickCmd())
		case "+", "=":
			if m.threads < runtime.NumCPU() {
				m.threads++
				m.miningCancel()
				m.startMining()
				return m, tea.Batch(m.statsCmd(), tickCmd())
			}
		case "-", "_":
			if m.threads > 1 {
				m.threads--
				m.miningCancel()
				m.startMining()
				return m, tea.Batch(m.statsCmd(), tickCmd())
			}
		case "r", "R":
			if m.mode == modeRPC && m.rpcURL != "" {
				m.miningCancel()
				m.startMining()
				return m, tea.Batch(m.statsCmd(), tickCmd())
			}
		}

	case statsMsg:
		if msg.running {
			if msg.hashrate > 0 {
			}
			if msg.found > 0 || msg.found != m.found {
				m.found = msg.found
				m.accepted = msg.accepted
				m.rejected = msg.rejected
				m.stale = msg.stale
			}
			if msg.height > 0 {
				m.height = msg.height
				m.bits = msg.bits
			}
		}
		return m, m.statsCmd()

	case tickMsg:
		updateHashrate(&m)
		updateSysStats(&m)
		return m, tickCmd()
	}

	return m, nil
}

func sparkline(data []float64, width int) string {
	if len(data) == 0 || width <= 0 {
		return ""
	}
	n := len(data)
	if n > width {
		data = data[n-width:]
	}
	maxVal := 0.0
	for _, v := range data {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}
	var sb strings.Builder
	for _, v := range data {
		idx := int(v / maxVal * 7)
		if idx < 0 {
			idx = 0
		}
		if idx > 7 {
			idx = 7
		}
		sb.WriteRune(barRunes[idx])
	}
	return sb.String()
}

func fmtHashrate(h float64) string {
	switch {
	case h >= 1_000_000:
		return fmt.Sprintf("%.2f MH/s", h/1_000_000)
	case h >= 1_000:
		return fmt.Sprintf("%.2f KH/s", h/1_000)
	default:
		return fmt.Sprintf("%.0f H/s", h)
	}
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

var (
	styleTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	styleLabel  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleValue  = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	styleOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	styleWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	styleBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func modeIndicator(m model) string {
	var parts []string
	for _, mm := range []struct {
		m modeType
		l string
	}{
		{modeBench, "bench"},
		{modeRPC, "rpc"},
		{modeStratum, "stratum"},
	} {
		if m.mode == mm.m {
			parts = append(parts, styleOK.Render("["+mm.l+"]"))
		} else {
			parts = append(parts, styleLabel.Render(" "+mm.l+" "))
		}
	}
	return strings.Join(parts, " ")
}

func boxLine(left string, width int) string {
	dw := lipgloss.Width(left)
	pad := width - 4 - dw
	if pad < 0 {
		pad = 0
	}
	return "│  " + left + strings.Repeat(" ", pad) + "  │"
}

func (m model) View() string {
	curRate := 0.0
	if len(m.history) > 0 {
		curRate = m.history[len(m.history)-1]
	}
	spark := sparkline(m.history, 48)
	rateStr := fmtHashrate(curRate)

	var modeInfo string
	switch m.mode {
	case modeBench:
		modeInfo = "benchmark (dummy block)"
	case modeRPC:
		modeInfo = fmt.Sprintf("RPC → %s", m.rpcURL)
		if m.bits != "" {
			modeInfo += fmt.Sprintf("  height=%d", m.height)
		}
	case modeStratum:
		modeInfo = "stratum (not implemented)"
	}

	shareLine := fmt.Sprintf("found=%d  accepted=%d  rejected=%d  stale=%d",
		m.found, m.accepted, m.rejected, m.stale)

	helpLine := "[b] mode  [+/-] threads  [r] restart  [q] quit"

	width := 80

	var b strings.Builder

	b.WriteString("┌" + strings.Repeat("─", width-2) + "┐\n")
	b.WriteString(boxLine(
		styleTitle.Render("LegacyCoin CPU Miner")+"  —  "+styleLabel.Render(m.rigName),
		width,
	))
	b.WriteString("\n")
	b.WriteString("│" + strings.Repeat("─", width-2) + "│\n")

	b.WriteString(boxLine(
		fmt.Sprintf("sparkline: %s  %s", spark, rateStr),
		width,
	))
	b.WriteString("\n")

	backendStr := m.backend
	if len(m.gpuDevices) > 0 {
		backendStr += " + GPU[" + strings.Join(m.gpuDevices, ",") + "]"
	}
	b.WriteString(boxLine(
		fmt.Sprintf("threads: %s  backend: %s",
			styleValue.Render(strconv.Itoa(m.threads)),
			styleLabel.Render(backendStr)),
		width,
	))
	b.WriteString("\n")

	b.WriteString(boxLine(
		fmt.Sprintf("CPU: %s  MEM: %s",
			styleValue.Render(fmt.Sprintf("%.1f%%", m.cpuPercent)),
			styleValue.Render(fmt.Sprintf("%.0f MB", m.memMB))),
		width,
	))
	b.WriteString("\n")

	b.WriteString("│" + strings.Repeat("─", width-2) + "│\n")

	b.WriteString(boxLine(modeIndicator(m), width))
	b.WriteString("\n")

	b.WriteString(boxLine(styleLabel.Render(modeInfo), width))
	b.WriteString("\n")

	b.WriteString(boxLine(shareLine, width))
	b.WriteString("\n")

	b.WriteString(boxLine("uptime: "+fmtDuration(time.Since(m.startTime)), width))
	b.WriteString("\n")

	b.WriteString("│" + strings.Repeat("─", width-2) + "│\n")

	b.WriteString(boxLine(styleLabel.Render(helpLine), width))
	b.WriteString("\n")

	b.WriteString("└" + strings.Repeat("─", width-2) + "┘")

	return b.String()
}
