package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failingMkvmerge shadows mkvmerge with one that writes whatever -o names, then exits 1.
const failingMkvmerge = "prev=; for a in \"$@\"; do [ \"$prev\" = -o ] && echo half > \"$a\"; prev=$a; done; exit 1\n"

// Hazard: H6
func TestFailedMuxLeavesNoFinalName(t *testing.T) {
	for _, p := range profilePaths {
		t.Run(p.name, func(t *testing.T) {
			stubTools(t, p.stub)
			stubFirst(t, map[string]string{"mkvmerge": failingMkvmerge})
			out := t.TempDir()
			c := newTestConverter(t, out)
			c.SetTempDir(t.TempDir())
			c.freeSpace = plentyOfSpace
			in := writeInput(t, t.TempDir(), "film.mkv", 1024)

			if err := c.Convert(in); err == nil {
				t.Fatal("Convert returned nil although mkvmerge failed")
			}
			if _, err := os.Lstat(filepath.Join(out, "film.mkv")); !os.IsNotExist(err) {
				t.Errorf("a file stands under the final name after a failed mux: %v", err)
			}
			if entries, _ := os.ReadDir(out); len(entries) != 0 {
				t.Errorf("output folder holds %d entries after a failed mux, want none", len(entries))
			}
		})
	}
}

// Hazard: H6
func TestMuxWritesPartialThenRenames(t *testing.T) {
	for _, p := range profilePaths {
		t.Run(p.name, func(t *testing.T) {
			logPath := stubTools(t, p.stub)
			out := t.TempDir()
			c := newTestConverter(t, out)
			c.SetTempDir(t.TempDir())
			c.freeSpace = plentyOfSpace
			in := writeInput(t, t.TempDir(), "film.mkv", 1024)

			if err := c.Convert(in); err != nil {
				t.Fatalf("Convert: %v", err)
			}
			calls, _ := os.ReadFile(logPath)
			want := "-o " + filepath.Join(out, "film.mkv") + ".partial"
			if !strings.Contains(string(calls), want) {
				t.Errorf("mkvmerge was not told to write %q; calls:\n%s", want, calls)
			}
			entries, _ := os.ReadDir(out)
			if len(entries) != 1 || entries[0].Name() != "film.mkv" {
				t.Errorf("output folder = %v, want only film.mkv", entries)
			}
		})
	}
}

// Hazard: H11
func TestExistingOutputNotOverwritten(t *testing.T) {
	for _, p := range profilePaths {
		t.Run(p.name+"/present before the run", func(t *testing.T) {
			stubTools(t, p.stub)
			out := t.TempDir()
			final := filepath.Join(out, "film.mkv")
			if err := os.WriteFile(final, []byte("chris"), 0o644); err != nil {
				t.Fatal(err)
			}
			c := newTestConverter(t, out)
			c.SetTempDir(t.TempDir())
			c.freeSpace = plentyOfSpace

			err := c.Convert(writeInput(t, t.TempDir(), "film.mkv", 1024))
			assertOutputKept(t, err, out, final)
		})
		t.Run(p.name+"/appears during the mux", func(t *testing.T) {
			stubTools(t, p.stub)
			stubFirst(t, map[string]string{"mkvmerge": "prev=; for a in \"$@\"; do [ \"$prev\" = -o ] && " +
				"{ echo half > \"$a\"; echo chris > \"${a%.partial}\"; }; prev=$a; done; exit 0\n"})
			out := t.TempDir()
			final := filepath.Join(out, "film.mkv")
			c := newTestConverter(t, out)
			c.SetTempDir(t.TempDir())
			c.freeSpace = plentyOfSpace

			err := c.Convert(writeInput(t, t.TempDir(), "film.mkv", 1024))
			assertOutputKept(t, err, out, final)
		})
	}
}

// assertOutputKept checks that Convert failed naming final, that final still holds the file
// someone else put there, and that nothing else is left in out.
func assertOutputKept(t *testing.T, err error, out, final string) {
	t.Helper()
	if err == nil {
		t.Fatal("Convert returned nil with a file at the final name")
	}
	if !strings.Contains(err.Error(), final) {
		t.Errorf("error %q does not name %s", err, final)
	}
	data, rerr := os.ReadFile(final)
	if rerr != nil || !strings.HasPrefix(string(data), "chris") {
		t.Errorf("the file at the final name was changed: %q, %v", data, rerr)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 1 {
		t.Errorf("output folder holds %d entries, want only the existing file", len(entries))
	}
}
