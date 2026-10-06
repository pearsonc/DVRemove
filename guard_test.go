package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Hazard: H16
func TestRefusesAboveUHD(t *testing.T) {
	for _, p := range profilePaths {
		for _, tc := range []struct {
			w, h   string
			refuse bool
		}{
			{"7680", "4320", true},
			{"3841", "2160", true},
			{"3840", "2161", true},
			{"3840", "2160", false},
			{"1920", "1080", false},
		} {
			t.Run(p.name+"/"+tc.w+"x"+tc.h, func(t *testing.T) {
				logPath := stubTools(t, p.stub)
				stubVideoSize(t, p.stub, tc.w, tc.h)
				outDir := t.TempDir()
				c := newTestConverter(t, outDir)
				c.SetTempDir(t.TempDir())
				c.freeSpace = plentyOfSpace
				in := writeInput(t, t.TempDir(), "film.mkv", 1024)

				err := c.Convert(in)
				entries, rerr := os.ReadDir(outDir)
				if rerr != nil {
					t.Fatal(rerr)
				}
				if !tc.refuse {
					if err != nil {
						t.Fatalf("%sx%s: Convert: %v, want it to convert", tc.w, tc.h, err)
					}
					if len(entries) != 1 {
						t.Errorf("%sx%s: %d outputs, want 1", tc.w, tc.h, len(entries))
					}
					return
				}
				if err == nil {
					t.Fatalf("%sx%s: Convert returned nil, want a refusal", tc.w, tc.h)
				}
				if want := tc.w + "x" + tc.h; !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name the size %s", err, want)
				}
				if n := ffmpegCalls(t, logPath); n != 0 {
					t.Errorf("%d ffmpeg conversion calls before the refusal, want 0", n)
				}
				if len(entries) != 0 {
					t.Errorf("%d outputs after a refusal, want 0", len(entries))
				}
			})
		}
	}
}

// lockedBuffer is a log sink safe to write from the guard's goroutine and read from the test.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

const (
	kib = 1 << 10
	gib = 1 << 30
)

// writeMeminfo replaces the injected /proc/meminfo, atomically so a poll never reads half a file.
func writeMeminfo(t *testing.T, path string, availBytes, swapUsedBytes uint64) {
	t.Helper()
	const swapTotal = 8 * gib
	text := fmt.Sprintf("MemTotal:       %d kB\nMemFree:        1024 kB\nMemAvailable:   %d kB\nSwapTotal:      %d kB\nSwapFree:       %d kB\n",
		64*gib/kib, availBytes/kib, swapTotal/kib, (swapTotal-swapUsedBytes)/kib)
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

// Hazard: H15
func TestMemoryGuardStopsConversion(t *testing.T) {
	const swapBase = 2 * gib
	cases := []struct {
		name        string
		avail, swap uint64 // the readings after the conversion has started
		stops       bool
	}{
		{"available below 4 GiB", 4*gib - kib, swapBase, true},
		{"swap used 1 GiB and a KiB above its start", 8 * gib, swapBase + gib + kib, true},
		{"available exactly 4 GiB, swap exactly 1 GiB up", 4 * gib, swapBase + gib, false},
		{"both comfortable", 8 * gib, swapBase, false},
	}
	for _, p := range profilePaths {
		for _, tc := range cases {
			t.Run(p.name+"/"+tc.name, func(t *testing.T) {
				stubTools(t, p.stub)
				started := filepath.Join(t.TempDir(), "started")
				hang := "0"
				if tc.stops {
					hang = "1"
				}
				t.Setenv("STUB_STARTED", started)
				t.Setenv("STUB_HANG", hang)
				stubFirst(t, map[string]string{"ffmpeg": `echo "$(basename "$0") $*" >> "$STUB_LOG"
case "$*" in *-filters*) echo ' libplacebo'; exit 0;; *-encoders*) exit 0;; esac
: > "$STUB_STARTED"
[ "$STUB_HANG" = 1 ] && exec sleep 20
sleep 0.5
for a in "$@"; do last=$a; done
[ "$last" != - ] && : > "$last"
exit 0
`})
				meminfo := filepath.Join(t.TempDir(), "meminfo")
				writeMeminfo(t, meminfo, 16*gib, swapBase)

				outDir := t.TempDir()
				c := newTestConverter(t, outDir)
				sink := &lockedBuffer{}
				c.log = zerolog.New(sink)
				c.SetTempDir(t.TempDir())
				c.freeSpace = plentyOfSpace
				c.memInfoPath = meminfo
				c.guardInterval = 5 * time.Millisecond
				in := writeInput(t, t.TempDir(), "film.mkv", 1024)

				go func() {
					for i := 0; i < 2000; i++ {
						if _, err := os.Stat(started); err == nil {
							writeMeminfo(t, meminfo, tc.avail, tc.swap)
							return
						}
						time.Sleep(5 * time.Millisecond)
					}
				}()
				begin := time.Now()
				err := c.Convert(in)
				elapsed := time.Since(begin)
				entries, rerr := os.ReadDir(outDir)
				if rerr != nil {
					t.Fatal(rerr)
				}

				if !tc.stops {
					if err != nil {
						t.Fatalf("readings at the thresholds stopped the conversion: %v", err)
					}
					if len(entries) != 1 {
						t.Errorf("%d outputs, want 1", len(entries))
					}
					if strings.Contains(sink.String(), "memory guard") {
						t.Errorf("a memory guard message was logged with the readings above the thresholds:\n%s", sink.String())
					}
					return
				}
				if err == nil {
					t.Fatal("Convert returned nil after the guard should have stopped it")
				}
				if !strings.Contains(err.Error(), "memory guard") {
					t.Errorf("error %q does not carry memory guard", err)
				}
				if elapsed > 10*time.Second {
					t.Errorf("the conversion ran %s: the guard did not stop it", elapsed)
				}
				if len(entries) != 0 {
					t.Errorf("%d outputs left by a stopped conversion, want 0", len(entries))
				}
				logged := sink.String()
				for _, want := range []string{"memory guard", `"mem_available_bytes":`, `"swap_used_bytes":`, `"swap_baseline_bytes":`} {
					if !strings.Contains(logged, want) {
						t.Errorf("log lacks %s:\n%s", want, logged)
					}
				}
			})
		}
	}
}

// logLines parses the JSON log lines a test logger wrote.
func logLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// lineWith returns the log line whose message is msg.
func lineWith(t *testing.T, lines []map[string]any, msg string) map[string]any {
	t.Helper()
	for _, l := range lines {
		if l["message"] == msg {
			return l
		}
	}
	t.Fatalf("no log line %q in %v", msg, lines)
	return nil
}

// Hazard: D19
func TestRunLoggingReadsCgroupAndFreeSpace(t *testing.T) {
	root := t.TempDir()
	for name, v := range map[string]string{"memory.peak": "123456789\n", "pids.peak": "42\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	empty := t.TempDir()
	onlyPids := t.TempDir()
	if err := os.WriteFile(filepath.Join(onlyPids, "pids.peak"), []byte("7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unreadable := t.TempDir()
	if err := os.Mkdir(filepath.Join(unreadable, "memory.peak"), 0o755); err != nil { // reading a directory fails
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name         string
		root         string
		wantMem      any
		wantPids     any
		wantMsgStart string
	}{
		{"files present", root, float64(123456789), float64(42), ""},
		{"files absent", empty, "unavailable", "unavailable", ""},
		{"one present, one absent", onlyPids, "unavailable", float64(7), ""},
		{"one unreadable", unreadable, "unavailable", "unavailable", ""},
	} {
		t.Run("end/"+tc.name, func(t *testing.T) {
			sink := &lockedBuffer{}
			logRunEnd(zerolog.New(sink), tc.root)
			l := lineWith(t, logLines(t, sink.String()), "run resources at end")
			if l["memory_peak_bytes"] != tc.wantMem {
				t.Errorf("memory_peak_bytes = %v, want %v", l["memory_peak_bytes"], tc.wantMem)
			}
			if l["pids_peak"] != tc.wantPids {
				t.Errorf("pids_peak = %v, want %v", l["pids_peak"], tc.wantPids)
			}
		})
	}

	t.Run("start", func(t *testing.T) {
		free := func(dir string) (uint64, error) {
			switch dir {
			case "/scratch":
				return 111, nil
			case "/out":
				return 0, errors.New("statfs failed")
			case os.TempDir():
				return 222, nil
			}
			return 0, errors.New("unexpected " + dir)
		}
		sink := &lockedBuffer{}
		logRunStart(zerolog.New(sink), "/scratch", "/out", free)
		l := lineWith(t, logLines(t, sink.String()), "run resources at start")
		if l["temp_dir"] != "/scratch" || l["temp_free_bytes"] != float64(111) {
			t.Errorf("temp_dir, temp_free_bytes = %v, %v, want /scratch, 111", l["temp_dir"], l["temp_free_bytes"])
		}
		if l["output_dir"] != "/out" || l["output_free_bytes"] != "unavailable" {
			t.Errorf("output_dir, output_free_bytes = %v, %v, want /out, unavailable", l["output_dir"], l["output_free_bytes"])
		}

		sink = &lockedBuffer{}
		logRunStart(zerolog.New(sink), "", "/out", free)
		l = lineWith(t, logLines(t, sink.String()), "run resources at start")
		if l["temp_dir"] != os.TempDir() || l["temp_free_bytes"] != float64(222) {
			t.Errorf("with temp_dir unset: temp_dir, temp_free_bytes = %v, %v, want %s, 222", l["temp_dir"], l["temp_free_bytes"], os.TempDir())
		}
	})
}

// Hazard: D19
func TestOnceLogsRunResources(t *testing.T) {
	stubTools(t, "dvhe.07")
	root := t.TempDir()
	for _, d := range []string{"in", "out", "logs"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeInput(t, filepath.Join(root, "in"), "film.mkv", 1024)
	cfg := fmt.Sprintf("input_dir: %s/in\noutput_dir: %s/out\nlog_dir: %s/logs\ntemp_dir: %s\n", root, root, root, t.TempDir())
	cfgPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "--", "-config", cfgPath, "-once", "-no-ui")
	cmd.Env = append(os.Environ(), "DVREMOVE_TEST_MAIN=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dvremove -once: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(root, "logs", "dvremove.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := logLines(t, string(data))
	start := lineWith(t, lines, "run resources at start")
	for _, key := range []string{"temp_dir", "temp_free_bytes", "output_dir", "output_free_bytes"} {
		if _, ok := start[key]; !ok {
			t.Errorf("the run's start line lacks %s: %v", key, start)
		}
	}
	end := lineWith(t, lines, "run resources at end")
	for _, key := range []string{"memory_peak_bytes", "pids_peak"} {
		if _, ok := end[key]; !ok {
			t.Errorf("the run's end line lacks %s: %v", key, end)
		}
	}
}

// Hazard: H15
func TestGuardLimitsComeFromConfig(t *testing.T) {
	dir := t.TempDir()
	write := func(extra string) string {
		path := filepath.Join(dir, "c.yaml")
		text := fmt.Sprintf("input_dir: %s\noutput_dir: %s\nlog_dir: %s\n%s", dir, dir, dir, extra)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cfg, err := LoadConfig(write("guard_min_available_mib: 6144\nguard_max_swap_growth_mib: 512\n"))
	if err != nil {
		t.Fatal(err)
	}
	c := newTestConverter(t, dir)
	c.SetGuardLimits(cfg.GuardMinAvailableMiB, cfg.GuardMaxSwapGrowthMiB)
	if c.minAvail != 6*gib || c.maxSwapGrowth != gib/2 {
		t.Errorf("limits %d, %d, want %d, %d", c.minAvail, c.maxSwapGrowth, 6*gib, gib/2)
	}

	cfg, err = LoadConfig(write(""))
	if err != nil {
		t.Fatal(err)
	}
	c = newTestConverter(t, dir)
	c.SetGuardLimits(cfg.GuardMinAvailableMiB, cfg.GuardMaxSwapGrowthMiB)
	if c.minAvail != 4*gib || c.maxSwapGrowth != gib {
		t.Errorf("defaults %d, %d, want %d, %d", c.minAvail, c.maxSwapGrowth, 4*gib, gib)
	}
}
