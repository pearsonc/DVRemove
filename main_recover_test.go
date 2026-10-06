package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Hazard: H9
func TestOnceRecoversJournalledStateBeforeConverting(t *testing.T) {
	stubTools(t, "dvhe.07")
	root := t.TempDir()
	for _, d := range []string{"in", "out", "state", "state/tmp"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stale := mustWrite(t, filepath.Join(root, "out", "old.mkv.partial"), "x")
	unlisted := mustWrite(t, filepath.Join(root, "out", "mine.mkv.partial"), "x")
	staleDir := filepath.Join(root, "state", "tmp", "dvremove-dead")
	if err := os.Mkdir(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	j := NewJournal(filepath.Join(root, "state", "journal.txt"))
	for _, p := range []string{stale, staleDir} {
		if err := j.Add(p); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := configIn(t, root, map[string]string{
		"input_dir": filepath.Join(root, "in"), "output_dir": filepath.Join(root, "out"),
		"state_dir": filepath.Join(root, "state"), "log_dir": filepath.Join(root, "state", "logs"),
		"temp_dir": filepath.Join(root, "state", "tmp"),
	})
	cmd := exec.Command(os.Args[0], "--", "-config", cfgPath, "-once", "-no-ui")
	cmd.Env = append(os.Environ(), "DVREMOVE_TEST_MAIN=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dvremove -once on an empty state folder failed: %v\n%s", err, out)
	}
	for _, gone := range []string{stale, staleDir} {
		if exists(gone) {
			t.Errorf("journalled %s survived the start-up", gone)
		}
	}
	if !exists(unlisted) {
		t.Error("an unjournalled .partial was removed")
	}
	if entries, err := j.Entries(); err != nil || len(entries) != 0 {
		t.Errorf("journal after the run = %v, %v; want empty", entries, err)
	}
}
