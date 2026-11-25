//go:build !windows

package app

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// memUsage reports used and total physical memory in bytes by reading
// /proc/meminfo. Systems without procfs (e.g. macOS) report ok=false.
func memUsage() (used, total uint64, ok bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	var memTotal, memAvail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		val, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			memTotal = val * 1024
		case "MemAvailable:":
			memAvail = val * 1024
		}
	}
	if memTotal == 0 {
		return 0, 0, false
	}
	if memAvail > memTotal {
		memAvail = memTotal
	}
	return memTotal - memAvail, memTotal, true
}

// cpuTimes returns cumulative idle and total jiffies from the first line of
// /proc/stat. idle includes iowait so a busy disk does not read as load.
func cpuTimes() (idle, total uint64, ok bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0, false
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	for i, field := range fields[1:] {
		v, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += v
		// Columns: user, nice, system, idle, iowait, ...
		if i == 3 || i == 4 {
			idle += v
		}
	}
	return idle, total, true
}
