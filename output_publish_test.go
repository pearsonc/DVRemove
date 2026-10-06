package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Hazard: H11
func TestPublishFallsBackWhereLinksFail(t *testing.T) {
	old := linkFile
	t.Cleanup(func() { linkFile = old })
	linkFile = func(string, string) error { return os.ErrPermission }

	dir := t.TempDir()
	partial, final := filepath.Join(dir, "a.mkv.partial"), filepath.Join(dir, "a.mkv")
	if err := os.WriteFile(partial, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := publishOutput(partial, final); err != nil {
		t.Fatalf("publish with links failing: %v", err)
	}
	if data, _ := os.ReadFile(final); string(data) != "new" {
		t.Errorf("final = %q, want new", data)
	}
	if _, err := os.Lstat(partial); !os.IsNotExist(err) {
		t.Errorf("partial remains: %v", err)
	}

	if err := os.WriteFile(partial, []byte("newer"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := publishOutput(partial, final)
	if err == nil || !strings.Contains(err.Error(), final) {
		t.Fatalf("publish over an existing file: %v, want an error naming %s", err, final)
	}
	if data, _ := os.ReadFile(final); string(data) != "new" {
		t.Errorf("final overwritten: %q", data)
	}
}
