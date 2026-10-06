package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Hazard: H12
// The caller discards the *RunLock, as main does, so nothing keeps its *os.File reachable, and
// the garbage collector's cleanup closes the descriptor, which drops the flock.
func TestLockSurvivesGarbageCollection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dvremove.lock")
	func() {
		if _, err := AcquireLock(path); err != nil {
			t.Fatal(err)
		}
	}()
	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}
	if l, err := AcquireLock(path); err == nil {
		l.Release()
		t.Fatal("a second AcquireLock succeeded after a GC: the first run's lock was released while it ran")
	}
}

// Hazard: H12
// Through main: a first run, slowed by a stub ffmpeg and with a small GOGC, is overtaken by a
// second acquisition that should be refused.
func TestSecondRunRefusedWhileFirstRunsAfterGC(t *testing.T) {
	stubTools(t, "dvhe.07")
	stubFirst(t, map[string]string{
		"ffmpeg": `echo "$(basename "$0") $*" >> "$STUB_LOG"
case "$*" in *-filters*) echo ' libplacebo'; exit 0;; *-encoders*) exit 0;; esac
sleep 6
for a in "$@"; do last=$a; done
[ "$last" != - ] && : > "$last"; exit 0
`})
	root := lockRoot(t)
	writeInput(t, filepath.Join(root, "in"), "film.mkv", 1024)
	cfg := configIn(t, root, map[string]string{
		"input_dir": filepath.Join(root, "in"), "output_dir": filepath.Join(root, "out"),
		"state_dir": filepath.Join(root, "state"), "log_dir": filepath.Join(root, "logs"),
		"temp_dir": filepath.Join(root, "tmp"),
	})
	first := exec.Command(os.Args[0], "--", "-config", cfg, "-once", "-no-ui")
	first.Env = append(os.Environ(), "DVREMOVE_TEST_MAIN=1", "GOGC=1")
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Process.Kill(); first.Wait() })
	time.Sleep(5 * time.Second) // the first run is inside ffmpeg's sleep
	if l, err := AcquireLock(filepath.Join(root, "state", "dvremove.lock")); err == nil {
		l.Release()
		t.Fatal("the lock was free while the first run was converting")
	}
}
