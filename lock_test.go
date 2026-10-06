package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// holdLockEnv names the lock file a helper process takes and holds, for TestHelperHoldsLock.
const holdLockEnv = "DVREMOVE_HOLD_LOCK"

// TestHelperHoldsLock is no test: run as a child with holdLockEnv set, it takes the lock, prints
// "locked" and waits for the parent to kill it.
func TestHelperHoldsLock(t *testing.T) {
	path := os.Getenv(holdLockEnv)
	if path == "" {
		t.Skip("helper process only")
	}
	if _, err := AcquireLock(path); err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("locked\n")
	select {}
}

// startLockHolder starts a separate process holding the lock at path and returns it once it
// reports the lock taken.
func startLockHolder(t *testing.T, path string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldsLock$")
	cmd.Env = append(os.Environ(), holdLockEnv+"="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "locked" {
		t.Fatalf("the lock holder said %q, %v", line, err)
	}
	return cmd
}

// lockRun runs `dvremove -once -no-ui` over root's folders and returns its exit status and log.
func lockRun(t *testing.T, root string) (int, string) {
	t.Helper()
	cfg := configIn(t, root, map[string]string{
		"input_dir": filepath.Join(root, "in"), "output_dir": filepath.Join(root, "out"),
		"state_dir": filepath.Join(root, "state"), "log_dir": filepath.Join(root, "logs"),
		"temp_dir": filepath.Join(root, "tmp"),
	})
	cmd := exec.Command(os.Args[0], "--", "-config", cfg, "-once", "-no-ui")
	cmd.Env = append(os.Environ(), "DVREMOVE_TEST_MAIN=1")
	cmd.Run()
	log, _ := os.ReadFile(filepath.Join(root, "logs", "dvremove.log"))
	return cmd.ProcessState.ExitCode(), string(log)
}

func lockRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"in", "out", "state", "logs", "tmp"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// Hazard: H12
func TestSecondRunRefusesLock(t *testing.T) {
	stubTools(t, "dvhe.07")
	root := lockRoot(t)
	lock, err := AcquireLock(filepath.Join(root, "state", "dvremove.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	// What the first run would be using: a journalled .partial and a journalled temp directory.
	partial := mustWrite(t, filepath.Join(root, "out", "live.mkv.partial"), "x")
	tmp := filepath.Join(root, "tmp", "dvremove-live")
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	j := NewJournal(filepath.Join(root, "state", "journal.txt"))
	for _, p := range []string{partial, tmp} {
		if err := j.Add(p); err != nil {
			t.Fatal(err)
		}
	}
	writeInput(t, filepath.Join(root, "in"), "film.mkv", 1024)

	code, log := lockRun(t, root)
	if code == 0 {
		t.Error("a second run with the lock held exited 0")
	}
	if !strings.Contains(log, "dvremove.lock") || !strings.Contains(log, "already running") {
		t.Errorf("the refusal gave no reason in the log:\n%s", log)
	}
	if !exists(partial) || !exists(tmp) {
		t.Error("a refused run removed the first run's journalled files")
	}
	if entries, err := j.Entries(); err != nil || len(entries) != 2 {
		t.Errorf("journal = %v, %v; want the first run's two entries", entries, err)
	}
	if outs, _ := os.ReadDir(filepath.Join(root, "out")); len(outs) != 1 {
		t.Errorf("a refused run touched the output folder: %v", outs)
	}
	if !exists(filepath.Join(root, "in", "film.mkv")) {
		t.Error("a refused run touched the input")
	}
}

// Hazard: H12
func TestSecondRunRefusesLockHeldByAnotherProcess(t *testing.T) {
	stubTools(t, "dvhe.07")
	root := lockRoot(t)
	startLockHolder(t, filepath.Join(root, "state", "dvremove.lock"))
	if code, _ := lockRun(t, root); code == 0 {
		t.Error("a run exited 0 while another process held the lock")
	}
}

// Hazard: H12
func TestLockLeftByKilledRunDoesNotBlock(t *testing.T) {
	stubTools(t, "dvhe.07")
	root := lockRoot(t)
	path := filepath.Join(root, "state", "dvremove.lock")
	holder := startLockHolder(t, path)
	if err := holder.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	holder.Wait()
	if !exists(path) {
		t.Fatal("the killed run left no lock file, so the test proves nothing")
	}
	if code, log := lockRun(t, root); code != 0 {
		t.Errorf("a run after a killed one exited %d:\n%s", code, log)
	}
}

// Hazard: H12
func TestLockIsFreeAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dvremove.lock")
	first, err := AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(path); err == nil {
		t.Fatal("a second AcquireLock succeeded while the first was held")
	}
	first.Release()
	second, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock after Release: %v", err)
	}
	second.Release()
}
