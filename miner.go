package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	"github.com/legacycoin/standalone-miner/internal/consensus"
)

func mineBlock(hasher Hasher, base [80]byte, bits uint32, workers int, done <-chan struct{}) (uint32, bool) {
	type result struct {
		nonce uint32
		ok    bool
	}
	resc := make(chan result, 1)

	for w := 0; w < workers; w++ {
		go func(start uint32) {
			var buf [80]byte
			for nonce := start; ; nonce += uint32(workers) {
				select {
				case <-done:
					return
				default:
				}
				copy(buf[:], base[:])
				binary.LittleEndian.PutUint32(buf[76:], nonce)
				hash, err := hasher.HashHeaderRaw(buf[:])
				if err != nil {
					continue
				}
				if consensus.CheckProofOfWork(hash, bits) == nil {
					select {
					case resc <- result{nonce, true}:
					default:
					}
					return
				}
			}
		}(uint32(w))
	}

	res := <-resc
	return res.nonce, res.ok
}

func benchHashrate(hasher Hasher, base [80]byte, workers, sec int) float64 {
	var count atomic.Uint64
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(workers)

	start := time.Now()
	for w := 0; w < workers; w++ {
		go func(s uint32) {
			defer wg.Done()
			var buf [80]byte
			for nonce := s; ; nonce += uint32(workers) {
				if stop.Load() {
					return
				}
				copy(buf[:], base[:])
				binary.LittleEndian.PutUint32(buf[76:], nonce)
				_, err := hasher.HashHeaderRaw(buf[:])
				if err == nil {
					count.Add(1)
				}
			}
		}(uint32(w))
	}

	time.Sleep(time.Duration(sec) * time.Second)
	stop.Store(true)
	wg.Wait()

	return float64(count.Load()) / time.Since(start).Seconds()
}
