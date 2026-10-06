package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Hazard: H1
// A FIFO named *.mkv in the input folder must not stop a -once run returning.
func TestFifoNamedMkvDoesNotHang(t *testing.T) {
	stubTools(t, "dvhe.07")
	in := t.TempDir()
	writeInput(t, in, "a.mkv", 1024)
	fifo := filepath.Join(in, "b.mkv")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	c := newTestConverter(t, t.TempDir())
	c.SetTempDir(t.TempDir())
	c.freeSpace = plentyOfSpace
	w := NewWatcher(in, c, 1, nil, zerolog.Nop())
	w.sleep = func(time.Duration) {}
	done := make(chan error, 1)
	go func() { done <- w.ProcessExisting() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		// release the blocked open so the goroutine ends
		if f, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil {
			f.Close()
		}
		<-done
		t.Fatal("ProcessExisting did not return within 10s with a FIFO named b.mkv in the input folder")
	}
}

// inputChecksConfig writes a config with every folder absolute and returns its path and folders.
func inputChecksConfig(t *testing.T) (cfg, in, state string) {
	t.Helper()
	root := t.TempDir()
	in, out, tmp := filepath.Join(root, "in"), filepath.Join(root, "out"), filepath.Join(root, "tmp")
	state = filepath.Join(root, "state")
	for _, d := range []string{in, out} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg = filepath.Join(root, "config.yaml")
	body := fmt.Sprintf("input_dir: %s\noutput_dir: %s\nlog_dir: %s/logs\ntemp_dir: %s\nstate_dir: %s\n", in, out, state, tmp, state)
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return
}

// Hazard: H2
// A file left unconverted because it cannot be read is not one of D21's three reasons, so the
// -once run exits 1.
func TestUnreadableInputExitsNonZero(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode 000 file")
	}
	stubTools(t, "dvhe.07")
	cfg, in, state := inputChecksConfig(t)
	p := writeInput(t, in, "film.mkv", 1024)
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o644) })
	cmd := exec.Command(os.Args[0], "--", "-config", cfg, "-once", "-no-ui")
	cmd.Env = append(os.Environ(), "DVREMOVE_TEST_MAIN=1")
	out, err := cmd.CombinedOutput()
	got := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		got = ee.ExitCode()
	} else if err != nil {
		got = -1
	}
	if got != 1 {
		t.Errorf("-once with an unreadable input exited %d, want 1 (D21); output %s", got, out)
	}
	data, _ := os.ReadFile(filepath.Join(state, "logs", "dvremove.log"))
	if !strings.Contains(string(data), "film.mkv") {
		t.Errorf("the unreadable input was not logged:\n%s", data)
	}
}

// Hazard: H5
// A ledger line ending CRLF, as a Windows editor leaves it, still skips its title.
func TestLedgerCRLFHandEdit(t *testing.T) {
	logPath := stubTools(t, "dvhe.07")
	ledger := filepath.Join(t.TempDir(), "ledger.txt")
	if err := os.WriteFile(ledger, []byte("1024\tfilm.mkv\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newTestConverter(t, t.TempDir())
	c.SetTempDir(t.TempDir())
	c.freeSpace = plentyOfSpace
	c.SetLedger(NewLedger(ledger))
	if err := c.Convert(writeInput(t, t.TempDir(), "film.mkv", 1024)); err != nil {
		t.Fatal(err)
	}
	if n := ffmpegCalls(t, logPath); n != 0 {
		t.Errorf("a title with a CRLF ledger line was converted again (%d ffmpeg calls)", n)
	}
}
