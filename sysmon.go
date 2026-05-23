package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func readCPUPercent(prevJiffies *uint64, prevTime *time.Time) (float64, error) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 14 {
		return 0, fmt.Errorf("short /proc/self/stat")
	}
	utime, _ := strconv.ParseUint(fields[13], 10, 64)
	stime, _ := strconv.ParseUint(fields[14], 10, 64)
	total := utime + stime

	now := time.Now()
	if prevTime.IsZero() {
		*prevJiffies = total
		*prevTime = now
		return 0, nil
	}

	clockTick := float64(100)
	elapsed := now.Sub(*prevTime).Seconds()
	jiffyDelta := float64(total - *prevJiffies)

	*prevJiffies = total
	*prevTime = now

	if elapsed <= 0 {
		return 0, nil
	}
	return jiffyDelta / clockTick / elapsed * 100, nil
}

func readMemMB() (float64, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				val, err := strconv.ParseFloat(parts[1], 64)
				if err != nil {
					return 0, err
				}
				return val / 1024, nil
			}
		}
	}
	return 0, fmt.Errorf("VmRSS not found")
}
