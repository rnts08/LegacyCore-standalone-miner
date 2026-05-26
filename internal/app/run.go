package app

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/legacycoin/standalone-miner/internal/chainhash"
	"github.com/legacycoin/standalone-miner/internal/consensus"
	"github.com/legacycoin/standalone-miner/internal/wire"
)

func (m *Model) startMining() {
	m.miningCancel()
	ctx, cancel := context.WithCancel(context.Background())
	m.miningCtx = ctx
	m.miningCancel = cancel
	m.count.hashCount.Store(0)
	m.hashPrev = 0
	m.history = m.history[:0]
	m.gpuCount.Store(0)
	m.gpuHashPrev = 0
	m.gpuHistory = m.gpuHistory[:0]
	m.lastStat = time.Now()

	go func() {
		switch m.mode {
		case ModeBench:
			m.runBenchLoop(ctx)
		case ModeRPC:
			m.runRPCLoop(ctx)
		case ModeStratum:
			m.runBenchLoop(ctx)
		}
	}()

	if m.gpuMiner != nil && m.gpuMiner.Available() {
		switch m.mode {
		case ModeBench:
			go m.runGPUBenchLoop(ctx)
		case ModeRPC:
			go m.runRPCGPULoop(ctx)
		}
	}
}

func (m *Model) runBenchLoop(ctx context.Context) {
	bits := m.postGenesisBits
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

func (m *Model) runGPUBenchLoop(ctx context.Context) {
	gm := m.gpuMiner
	if gm == nil {
		return
	}
	header := wire.BlockHeader{
		Version:   1,
		PrevBlock: [32]byte{},
		Timestamp: uint32(time.Now().Unix()),
		Bits:      m.postGenesisBits,
	}

	gpuSlot := uint32(m.threads)
	slotsPerMiner := gpuSlot + 1
	step := slotsPerMiner * m.totalMiners
	nonce := m.minerID*slotsPerMiner + gpuSlot
	batchSize := gm.MaxBatch()
	if batchSize < 1 {
		batchSize = 256
	}
	headers := make([][80]byte, batchSize)
	m.gpuActive.Store(true)
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
			m.gpuActive.Store(false)
			fmt.Fprintf(os.Stderr, "GPU error: %v — resetting\n", err)
			gm.Reset()
			batchSize = gm.MaxBatch()
			if batchSize < 1 {
				batchSize = 256
			}
			headers = make([][80]byte, batchSize)
			time.Sleep(time.Second)
			continue
		}
		m.gpuActive.Store(true)
		m.gpuCount.Add(uint64(batchSize))
	}
}

func (m *Model) runRPCGPULoop(ctx context.Context) {
	gm := m.gpuMiner
	if gm == nil {
		return
	}
	client := newRPCClient(m.rpcURL, m.rpcUser, m.rpcPass, m.dataDir)
	if client == nil {
		return
	}

	m.gpuActive.Store(true)
	batchSize := gm.MaxBatch()
	if batchSize < 1 {
		batchSize = 256
	}
	headers := make([][80]byte, batchSize)
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
			m.gpuActive.Store(false)
			fmt.Fprintf(os.Stderr, "GPU error: %v — resetting\n", err)
			gm.Reset()
			batchSize = gm.MaxBatch()
			if batchSize < 1 {
				batchSize = 256
			}
			headers = make([][80]byte, batchSize)
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}
		m.gpuActive.Store(true)
		m.gpuCount.Add(uint64(batchSize))

		for i, hash := range results {
			if m.tmplState.stale.Load() {
				break
			}
			if consensus.CheckProofOfWork(chainhash.Hash(hash), bits) == nil {
				nonceFound := binary.LittleEndian.Uint32(headers[i][76:])
				m.tmplState.mu.Lock()
				raw := m.tmplState.raw
				height := m.tmplState.height
				m.tmplState.mu.Unlock()

				writeNonce(raw, nonceFound)
				m.found++

				if err := client.SubmitBlock(hex.EncodeToString(raw)); err != nil {
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

func (m *Model) runRPCLoop(ctx context.Context) {
	client := newRPCClient(m.rpcURL, m.rpcUser, m.rpcPass, m.dataDir)
	if client == nil {
		return
	}

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
				m.tmplState.raw = raw
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
		target := consensus.TargetFromBits(bits)
		hasher := newHasher(m.pers)
		miningCtx, miningCancel := context.WithCancel(ctx)
		nonce, ok := mineBlockHashed(hasher, base, target, m.threads, miningCtx, &m.count.hashCount, m.minerID, m.totalMiners, slotsPerMiner, &m.tmplState.stale)
		miningCancel()

		if !ok {
			continue
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		m.tmplState.mu.Lock()
		raw := m.tmplState.raw
		m.tmplState.mu.Unlock()

		writeNonce(raw, nonce)
		blockHex := hex.EncodeToString(raw)

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

		select {
		case m.pollTrigger <- struct{}{}:
		default:
		}

		m.tmplState.mu.Lock()
		m.tmplState.hasStale = false
		m.tmplState.mu.Unlock()

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
