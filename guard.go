package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Largest video dimensions dvremove converts (H16, D19): above these, decoding exhausts the
// host's memory, which the GPU shares.
const (
	maxVideoWidth  = 3840
	maxVideoHeight = 2160
)

// Defaults for the host memory guard (H15, D19's cold starts, to be replaced from the logged
// peaks). The config keys guard_min_available_mib and guard_max_swap_growth_mib override them.
const (
	defaultMinAvailable   = 4 << 30
	defaultMaxSwapGrowth  = 1 << 30
	defaultGuardInterval  = 2 * time.Second
	hostMeminfo           = "/proc/meminfo"
	cgroupRoot            = "/sys/fs/cgroup"
	unavailable           = "unavailable"
	logKeyMemAvailable    = "mem_available_bytes"
	logKeySwapUsed        = "swap_used_bytes"
	logKeySwapBaseline    = "swap_baseline_bytes"
	logKeyMemPeak         = "memory_peak_bytes"
	logKeyPidsPeak        = "pids_peak"
	logKeyTempFree        = "temp_free_bytes"
	logKeyOutputFree      = "output_free_bytes"
	msgRunStartResources  = "run resources at start"
	msgRunEndResources    = "run resources at end"
	msgMemoryGuardStopped = "memory guard stopped the conversion"
)

// guardSettings configures the host memory guard. Inside the container /proc/meminfo shows the
// host's figures, which is why the guard reads it.
type guardSettings struct {
	memInfoPath   string
	guardInterval time.Duration
	minAvail      uint64 // bytes; a conversion is stopped when MemAvailable falls below it
	maxSwapGrowth uint64 // bytes; or when swap used rises more than this above its start
}

func defaultGuardSettings() guardSettings {
	return guardSettings{
		memInfoPath:   hostMeminfo,
		guardInterval: defaultGuardInterval,
		minAvail:      defaultMinAvailable,
		maxSwapGrowth: defaultMaxSwapGrowth,
	}
}

// SetGuardLimits sets the guard's thresholds in MiB; 0, or a value too large to hold in bytes,
// keeps the current limit, so no value turns the guard off (H15).
func (c *Converter) SetGuardLimits(minAvailMiB, maxSwapGrowthMiB uint64) {
	if minAvailMiB > 0 && minAvailMiB <= maxGuardMiB {
		c.minAvail = minAvailMiB << 20
	}
	if maxSwapGrowthMiB > 0 && maxSwapGrowthMiB <= maxGuardMiB {
		c.maxSwapGrowth = maxSwapGrowthMiB << 20
	}
}

// refuseAboveUHD returns an error naming the size when a video is wider than 3840 or taller than
// 2160, so the file is refused before any ffmpeg starts. A size of 0 means mediainfo reported none.
func refuseAboveUHD(filename string, width, height int) error {
	if width > maxVideoWidth || height > maxVideoHeight {
		return fmt.Errorf("refusing %s: video is %dx%d, above the %dx%d limit",
			filename, width, height, maxVideoWidth, maxVideoHeight)
	}
	return nil
}

// readMeminfo returns MemAvailable and swap used (SwapTotal less SwapFree) in bytes. haveSwap is
// false when either swap line is missing, and swapUsed is then 0. A reading with no MemAvailable
// is an error, because a guard that cannot read it cannot guard (H15).
func readMeminfo(path string) (avail, swapUsed uint64, haveSwap bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false, err
	}
	defer f.Close()
	var total, free uint64
	var haveAvail, haveTotal, haveFree bool
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		var dst *uint64
		var seen *bool
		switch fields[0] {
		case "MemAvailable:":
			dst, seen = &avail, &haveAvail
		case "SwapTotal:":
			dst, seen = &total, &haveTotal
		case "SwapFree:":
			dst, seen = &free, &haveFree
		default:
			continue
		}
		kib, perr := strconv.ParseUint(fields[1], 10, 64)
		if perr != nil {
			return 0, 0, false, fmt.Errorf("parsing %s in %s: %w", fields[0], path, perr)
		}
		*dst, *seen = kib<<10, true
	}
	if err := sc.Err(); err != nil {
		return 0, 0, false, err
	}
	if !haveAvail {
		return 0, 0, false, fmt.Errorf("%s has no MemAvailable line", path)
	}
	if !haveTotal || !haveFree {
		return avail, 0, false, nil
	}
	if free > total {
		return avail, 0, true, nil
	}
	return avail, total - free, true, nil
}

// memGuard stops a conversion's processes when the host runs short of memory.
type memGuard struct {
	guardSettings
	log      zerolog.Logger
	baseline uint64 // swap used when the conversion began
	haveBase bool
	mu       sync.Mutex
	reason   string
}

// newMemGuard reads the swap level the conversion starts from. Call it before starting the
// conversion's processes.
func (c *Converter) newMemGuard() *memGuard {
	g := &memGuard{guardSettings: c.guardSettings, log: c.log}
	_, swap, haveSwap, err := readMeminfo(g.memInfoPath)
	switch {
	case err != nil:
		g.log.Warn().Err(err).Msg("memory guard cannot read the host's memory at the conversion's start")
	case !haveSwap:
		g.log.Warn().Str("swap", unavailable).Msg("memory guard: the swap lines are unavailable, guarding MemAvailable only")
	default:
		g.baseline, g.haveBase = swap, true
	}
	return g
}

// watch polls the host's memory until the returned stop function is called, and kills procs on
// the first breach. stop returns an error carrying memory guard if it did.
func (g *memGuard) watch(procs ...*os.Process) (stop func() error) {
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(g.guardInterval)
		defer tick.Stop()
		for {
			select {
			case <-quit:
				return
			case <-tick.C:
				avail, swap, haveSwap, err := readMeminfo(g.memInfoPath)
				reason := ""
				switch {
				case err != nil:
					reason = "the host's memory could not be read: " + err.Error()
				case avail < g.minAvail:
					reason = "MemAvailable below the minimum"
				case g.haveBase && haveSwap && swap > g.baseline && swap-g.baseline > g.maxSwapGrowth:
					reason = "swap used rose above its limit"
				}
				if reason == "" {
					continue
				}
				g.mu.Lock()
				g.reason = reason
				g.mu.Unlock()
				g.log.Error().Str("reason", reason).
					Uint64(logKeyMemAvailable, avail).Uint64(logKeySwapUsed, swap).Uint64(logKeySwapBaseline, g.baseline).
					Uint64("min_available_bytes", g.minAvail).Uint64("max_swap_growth_bytes", g.maxSwapGrowth).
					Msg(msgMemoryGuardStopped)
				for _, p := range procs {
					p.Kill()
				}
				return
			}
		}
	}()
	return func() error {
		close(quit)
		<-done
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.reason != "" {
			return fmt.Errorf("memory guard stopped the conversion: %s", g.reason)
		}
		return nil
	}
}

// readCounter reads one unsigned number from a cgroup file.
func readCounter(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

// counterField adds the value under key, or the string "unavailable" where it could not be read.
func counterField(ev *zerolog.Event, key string, v uint64, err error) *zerolog.Event {
	if err != nil {
		return ev.Str(key, unavailable)
	}
	return ev.Uint64(key, v)
}

// logRunStart logs the free space on the temporary and output folders; tempDir empty means the
// operating system's default.
func logRunStart(log zerolog.Logger, tempDir, outputDir string, free func(string) (uint64, error)) {
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	tf, terr := free(tempDir)
	of, oerr := free(outputDir)
	ev := log.Info().Str("temp_dir", tempDir)
	ev = counterField(ev, logKeyTempFree, tf, terr).Str("output_dir", outputDir)
	counterField(ev, logKeyOutputFree, of, oerr).Msg(msgRunStartResources)
}

// logRunEnd logs the container's peak memory and process count from the cgroup under root.
func logRunEnd(log zerolog.Logger, root string) {
	mem, merr := readCounter(filepath.Join(root, "memory.peak"))
	pids, perr := readCounter(filepath.Join(root, "pids.peak"))
	ev := counterField(log.Info(), logKeyMemPeak, mem, merr)
	counterField(ev, logKeyPidsPeak, pids, perr).Msg(msgRunEndResources)
}
