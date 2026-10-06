package main

import (
	"bytes"
	"fmt"
	"os"
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
