package app

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/legacycoin/standalone-miner/internal/chaincfg"
	"github.com/legacycoin/standalone-miner/internal/config"
	"github.com/legacycoin/standalone-miner/internal/wire"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	styleLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleValue = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	styleOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	styleWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	styleBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func hostname() string {
	h, _ := os.Hostname()
	return h
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

func defaultDataDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home + "/.legacycoin"
	}
	return config.DefaultDataDir()
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

func (m *Model) Init() tea.Cmd {
	m.startMining()
	return tea.Batch(
		m.statsCmd(),
		tickCmd(),
	)
}

func (m *Model) statsCmd() tea.Cmd {
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

func (m *Model) updateHashrate() {
	now := time.Now()
	elapsed := now.Sub(m.lastStat).Seconds()
	if elapsed < 0.5 {
		return
	}
	current := m.count.hashCount.Load()
	rate := float64(current-m.hashPrev) / elapsed
	m.hashPrev = current

	gpuCurrent := m.gpuCount.Load()
	gpuRate := float64(gpuCurrent-m.gpuHashPrev) / elapsed
	m.gpuHashPrev = gpuCurrent

	m.lastStat = now

	m.history = append(m.history, rate)
	if len(m.history) > maxHist {
		m.history = m.history[len(m.history)-maxHist:]
	}
	if m.gpuMiner != nil && m.gpuMiner.Available() {
		m.gpuHistory = append(m.gpuHistory, gpuRate)
		if len(m.gpuHistory) > maxHist {
			m.gpuHistory = m.gpuHistory[len(m.gpuHistory)-maxHist:]
		}
	} else {
		m.gpuHistory = m.gpuHistory[:0]
	}
}

func (m *Model) updateSysStats() {
	m.cpuPercent, _ = readCPUPercent(&m.cpuJiffies, &m.cpuTime)
	m.memMB, _ = readMemMB()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.mode == ModeRPC && m.rpcURL != "" {
				m.miningCancel()
				m.startMining()
				return m, tea.Batch(m.statsCmd(), tickCmd())
			}
		}

	case statsMsg:
		if msg.running {
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
		m.updateHashrate()
		m.updateSysStats()
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

func modeIndicator(m *Model) string {
	var parts []string
	for _, mm := range []struct {
		m ModeType
		l string
	}{
		{ModeBench, "bench"},
		{ModeRPC, "rpc"},
		{ModeStratum, "stratum"},
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

func (m *Model) View() string {
	curRate := 0.0
	if len(m.history) > 0 {
		curRate = m.history[len(m.history)-1]
	}
	spark := sparkline(m.history, 48)
	rateStr := fmtHashrate(curRate)

	gpuRate := 0.0
	gpuSparkStr := ""
	gpuRateStr := ""
	hasGPU := m.gpuMiner != nil && m.gpuMiner.Available()
	if hasGPU && len(m.gpuHistory) > 0 {
		gpuRate = m.gpuHistory[len(m.gpuHistory)-1]
		gpuSparkStr = sparkline(m.gpuHistory, 20)
		gpuRateStr = fmtHashrate(gpuRate)
	}

	var modeInfo string
	switch m.mode {
	case ModeBench:
		modeInfo = "benchmark (dummy block)"
	case ModeRPC:
		modeInfo = fmt.Sprintf("RPC → %s", m.rpcURL)
		if m.bits != "" {
			modeInfo += fmt.Sprintf("  height=%d", m.height)
		}
	case ModeStratum:
		modeInfo = "stratum (not implemented)"
	}

	shareLine := fmt.Sprintf("found=%d  accepted=%d  rejected=%d  stale=%d",
		m.found, m.accepted, m.rejected, m.stale)

	helpLine := "[b] mode  [+/-] threads  [r] restart  [q] quit"

	width := 80

	var b strings.Builder

	b.WriteString("┌" + strings.Repeat("─", width) + "┐\n")
	b.WriteString(boxLine(
		styleTitle.Render("LegacyCoin Miner")+"  —  "+styleLabel.Render(m.rigName),
		width,
	))
	b.WriteString("\n")
	b.WriteString("│" + strings.Repeat("─", width) + "│\n")

	if hasGPU {
		b.WriteString(boxLine(
			fmt.Sprintf("CPU sparkline: %s  %s", spark, rateStr),
			width,
		))
		b.WriteString("\n")

		gpuActiveStr := "ACTIVE"
		if !m.gpuActive.Load() {
			gpuActiveStr = "IDLE"
		}
		gpuInfo := fmt.Sprintf("%s  %s", m.gpuName, gpuActiveStr)
		b.WriteString(boxLine(
			fmt.Sprintf("GPU sparkline: %s  %s  [%s]", gpuSparkStr, gpuRateStr, styleLabel.Render(gpuInfo)),
			width,
		))
		b.WriteString("\n")
	} else {
		b.WriteString(boxLine(
			fmt.Sprintf("sparkline: %s  %s", spark, rateStr),
			width,
		))
		b.WriteString("\n")
	}

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

	b.WriteString("│" + strings.Repeat("─", width) + "│\n")

	b.WriteString(boxLine(modeIndicator(m), width))
	b.WriteString("\n")

	b.WriteString(boxLine(styleLabel.Render(modeInfo), width))
	b.WriteString("\n")

	b.WriteString(boxLine(shareLine, width))
	b.WriteString("\n")

	b.WriteString(boxLine("uptime: "+fmtDuration(time.Since(m.startTime)), width))
	b.WriteString("\n")

	b.WriteString("│" + strings.Repeat("─", width) + "│\n")

	b.WriteString(boxLine(styleLabel.Render(helpLine), width))
	b.WriteString("\n")

	b.WriteString("└" + strings.Repeat("─", width) + "┘")

	return b.String()
}
