// Package sysinfo lee métricas básicas del host desde /proc y syscalls, sin
// dependencias externas.
package sysinfo

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Info struct {
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	CPUCores     int       `json:"cpu_cores"`
	LoadAvg      [3]float64 `json:"load_avg"`
	MemTotalMB   uint64    `json:"mem_total_mb"`
	MemFreeMB    uint64    `json:"mem_free_mb"`
	MemUsedMB    uint64    `json:"mem_used_mb"`
	SwapTotalMB  uint64    `json:"swap_total_mb"`
	SwapFreeMB   uint64    `json:"swap_free_mb"`
	DiskTotalGB  float64   `json:"disk_total_gb"`
	DiskFreeGB   float64   `json:"disk_free_gb"`
	UptimeSecs   uint64    `json:"uptime_secs"`
	SampledAt    time.Time `json:"sampled_at"`
	GoRoutines   int       `json:"goroutines"`
}

func Collect() (*Info, error) {
	host, _ := os.Hostname()
	info := &Info{
		Hostname:   host,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		CPUCores:   runtime.NumCPU(),
		GoRoutines: runtime.NumGoroutine(),
		SampledAt:  time.Now(),
	}

	if runtime.GOOS != "linux" {
		// Fuera de Linux devolvemos lo que sí es portable.
		return info, nil
	}

	info.LoadAvg = readLoadAvg()
	readMeminfo(info)
	info.UptimeSecs = readUptime()

	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err == nil {
		total := float64(st.Blocks) * float64(st.Bsize)
		free := float64(st.Bavail) * float64(st.Bsize)
		info.DiskTotalGB = round2(total / (1 << 30))
		info.DiskFreeGB = round2(free / (1 << 30))
	}
	return info, nil
}

func readLoadAvg() [3]float64 {
	var out [3]float64
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return out
	}
	fields := strings.Fields(string(data))
	for i := 0; i < 3 && i < len(fields); i++ {
		out[i], _ = strconv.ParseFloat(fields[i], 64)
	}
	return out
}

func readMeminfo(info *Info) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()

	vals := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		vals[key] = kb / 1024 // MiB
	}

	info.MemTotalMB = vals["MemTotal"]
	info.MemFreeMB = vals["MemAvailable"]
	if info.MemTotalMB >= info.MemFreeMB {
		info.MemUsedMB = info.MemTotalMB - info.MemFreeMB
	}
	info.SwapTotalMB = vals["SwapTotal"]
	info.SwapFreeMB = vals["SwapFree"]
}

func readUptime() uint64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	secs, _ := strconv.ParseFloat(fields[0], 64)
	return uint64(secs)
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
