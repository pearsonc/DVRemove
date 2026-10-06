package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMain lets a test run main() in a child process: the child is this test binary with
// DVREMOVE_TEST_MAIN set, and its arguments after "--" are main's.
func TestMain(m *testing.M) {
	if os.Getenv("DVREMOVE_TEST_MAIN") == "1" {
		for i, a := range os.Args {
			if a == "--" {
				os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
				break
			}
		}
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runOnce runs `dvremove -once -no-ui` over n Profile 7 inputs and returns its exit status.
// mkvmergeExit is the status the stub mkvmerge returns.
func runOnce(t *testing.T, n, mkvmergeExit int) int {
	t.Helper()
	stubTools(t, "dvhe.07")
	override := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", mkvmergeExit)
	if mkvmergeExit == 0 {
		script = "#!/bin/sh\nprev=; for a in \"$@\"; do [ \"$prev\" = -o ] && : > \"$a\"; prev=$a; done; exit 0\n"
	}
	if err := os.WriteFile(filepath.Join(override, "mkvmerge"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", override+string(os.PathListSeparator)+os.Getenv("PATH"))

	root := t.TempDir()
	for _, d := range []string{"in", "out", "logs"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < n; i++ {
		writeInput(t, filepath.Join(root, "in"), fmt.Sprintf("film-%d.mkv", i), 1024)
	}
	cfg := fmt.Sprintf("input_dir: %s/in\noutput_dir: %s/out\nlog_dir: %s/logs\ntemp_dir: %s\n",
		root, root, root, t.TempDir())
	cfgPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "--", "-config", cfgPath, "-once", "-no-ui")
	cmd.Env = append(os.Environ(), "DVREMOVE_TEST_MAIN=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	default:
		t.Fatalf("running dvremove: %v\n%s", err, out)
		return -1
	}
}

// Hazard: H2
func TestOnceExitsNonZeroOnFailure(t *testing.T) {
	if got := runOnce(t, 2, 1); got != 1 {
		t.Errorf("-once with every conversion failing exited %d, want 1", got)
	}
	if got := runOnce(t, 2, 0); got != 0 {
		t.Errorf("-once with every conversion succeeding exited %d, want 0", got)
	}
}
