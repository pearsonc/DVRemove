package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig makes the three folders and a config file holding extra, and returns its path.
func writeConfig(t *testing.T, extra string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"in", "out", "logs"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	body := "input_dir: " + filepath.Join(root, "in") + "\n" +
		"output_dir: " + filepath.Join(root, "out") + "\n" +
		"log_dir: " + filepath.Join(root, "logs") + "\n" + extra
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Hazard: D23
func TestLoadConfigRejectsUnknownKey(t *testing.T) {
	for _, extra := range []string{"tmp_dir: /var/tmp\n", "transcode:\n  encodr: nvenc\n"} {
		_, err := LoadConfig(writeConfig(t, extra))
		if err == nil {
			t.Fatalf("LoadConfig accepted %q", extra)
		}
		key := "tmp_dir"
		if strings.HasPrefix(extra, "transcode") {
			key = "encodr"
		}
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %q does not name the key %q", err, key)
		}
	}
}

// Hazard: D23
func TestRepositoryConfigStillLoads(t *testing.T) {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []string{"toConvert", "Converted", "logs"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if _, err := LoadConfig("config.yaml"); err != nil {
		t.Fatalf("repository config.yaml no longer loads: %v", err)
	}
}
