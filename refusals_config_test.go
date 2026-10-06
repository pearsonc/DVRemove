package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Hazard: H11
func TestExistingOutputRefusedBeforeWork(t *testing.T) {
	for _, p := range profilePaths {
		t.Run(p.name, func(t *testing.T) {
			logPath := stubTools(t, p.stub)
			stubVideoSize(t, p.stub, "3840", "2160")
			out := t.TempDir()
			if err := os.WriteFile(filepath.Join(out, "film.mkv"), []byte("chris"), 0o644); err != nil {
				t.Fatal(err)
			}
			c := newTestConverter(t, out)
			c.SetTempDir(t.TempDir())
			c.freeSpace = plentyOfSpace
			err := c.Convert(writeInput(t, t.TempDir(), "film.mkv", 1024))
			if err == nil {
				t.Fatal("Convert returned nil with a file at the final name")
			}
			if n := ffmpegCalls(t, logPath); n != 0 {
				t.Errorf("%d ffmpeg conversion calls ran before the existing output was refused", n)
			}
		})
	}
}

// Hazard: H16
func TestSizeUnreportedIsRefused(t *testing.T) {
	for _, p := range profilePaths {
		for _, size := range []struct{ name, w, h string }{
			{"neither", "", ""}, {"no height", "1920", ""}, {"no width", "", "1080"},
		} {
			t.Run(p.name+"/"+size.name, func(t *testing.T) {
				logPath := stubTools(t, p.stub)
				stubVideoSize(t, p.stub, size.w, size.h)
				c := newTestConverter(t, t.TempDir())
				c.SetTempDir(t.TempDir())
				c.freeSpace = plentyOfSpace
				err := c.Convert(writeInput(t, t.TempDir(), "film.mkv", 1024))
				if n := ffmpegCalls(t, logPath); err == nil || n != 0 {
					t.Errorf("a file of unreported size was converted: err=%v, %d ffmpeg calls", err, n)
				}
			})
		}
	}
}

func writeConfigBody(t *testing.T, tail string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"in", "out"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(root, "c.yaml")
	body := fmt.Sprintf("input_dir: %s/in\noutput_dir: %s/out\nlog_dir: %s/logs\n", root, root, root) +
		strings.ReplaceAll(tail, "{root}", root)
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Hazard: H3
func TestTempDirInSecondYAMLDocument(t *testing.T) {
	c, err := LoadConfig(writeConfigBody(t, "---\ntemp_dir: {root}/tmp\n"))
	if err == nil {
		t.Fatalf("a config naming temp_dir in a second document loaded, temp_dir=%q", c.TempDir)
	}
	if !strings.Contains(err.Error(), "document") {
		t.Errorf("error %q does not name the extra document as the reason", err)
	}
}

// Hazard: H15
func TestGuardLimitTooLargeForBytesRefusedAtLoad(t *testing.T) {
	const largest = math.MaxUint64 >> 20
	for _, key := range []string{"guard_min_available_mib", "guard_max_swap_growth_mib"} {
		for _, v := range []uint64{1 << 44, math.MaxUint64} {
			if _, err := LoadConfig(writeConfigBody(t, fmt.Sprintf("%s: %d\n", key, v))); err == nil {
				t.Errorf("%s: %d loaded, but it overflows when shifted to bytes", key, v)
			}
		}
		if _, err := LoadConfig(writeConfigBody(t, fmt.Sprintf("%s: %d\n", key, uint64(largest)))); err != nil {
			t.Errorf("%s at the largest value held in bytes was refused: %v", key, err)
		}
	}
}
