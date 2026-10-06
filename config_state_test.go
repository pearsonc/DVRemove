package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configIn writes a config file in root naming the given folders, and returns its path.
func configIn(t *testing.T, root string, keys map[string]string) string {
	t.Helper()
	var b strings.Builder
	for k, v := range keys {
		b.WriteString(k + ": " + v + "\n")
	}
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Hazard: D21
func TestLoadConfigReadsStateDir(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, "state_dir: "+t.TempDir()+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir == "" {
		t.Error("StateDir is empty although state_dir is set")
	}
}

// Hazard: D21
func TestUnsetStateDirFallsBackToLogDir(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir != cfg.LogDir {
		t.Errorf("StateDir = %q, want the log_dir %q", cfg.StateDir, cfg.LogDir)
	}
}

// Hazard: D21
func TestFirstRunCreatesStateLogAndTempDirs(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"in", "out"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(root, "state")
	if err := os.Mkdir(state, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := configIn(t, root, map[string]string{
		"input_dir": filepath.Join(root, "in"), "output_dir": filepath.Join(root, "out"),
		"state_dir": filepath.Join(state, "run"), "log_dir": filepath.Join(state, "logs"),
		"temp_dir": filepath.Join(state, "tmp"),
	})
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("a first run with absent state, log and temp folders did not load: %v", err)
	}
	for _, d := range []string{cfg.StateDir, cfg.LogDir, cfg.TempDir} {
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			t.Errorf("%s was not created: %v", d, err)
		}
	}
}

// Hazard: R2
func TestMissingInputOrOutputDirFailsTheLoad(t *testing.T) {
	for _, missing := range []string{"input_dir", "output_dir"} {
		t.Run(missing, func(t *testing.T) {
			root := t.TempDir()
			keys := map[string]string{
				"input_dir": filepath.Join(root, "in"), "output_dir": filepath.Join(root, "out"),
				"log_dir": filepath.Join(root, "logs"), "state_dir": filepath.Join(root, "state"),
			}
			for _, k := range []string{"input_dir", "output_dir"} {
				if k != missing {
					if err := os.Mkdir(keys[k], 0o755); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := LoadConfig(configIn(t, root, keys)); err == nil {
				t.Fatalf("LoadConfig accepted a missing %s", missing)
			}
			for _, d := range []string{"logs", "state"} {
				if _, err := os.Lstat(filepath.Join(root, d)); err == nil {
					t.Errorf("%s was created although the load failed", d)
				}
			}
		})
	}
}
