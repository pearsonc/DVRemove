package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

var profilePaths = []struct{ name, stub string }{
	{"Profile 5", "dvhe.05"},
	{"Profile 7", "dvhe.07"},
}

func newTestConverter(t *testing.T, outDir string) *Converter {
	t.Helper()
	tc := TranscodeConfig{Encoder: "software", Quality: 28, Preset: "fast", VAAPIDevice: "/dev/dri/renderD128"}
	return NewConverter(t.TempDir(), outDir, tc, zerolog.Nop())
}

func plentyOfSpace(string) (uint64, error) { return 1 << 40, nil }

// Hazard: H3
func TestTempDirUsesConfig(t *testing.T) {
	for _, p := range profilePaths {
		for _, set := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/set=%v", p.name, set), func(t *testing.T) {
				logPath := stubTools(t, p.stub)
				osDefault := t.TempDir()
				t.Setenv("TMPDIR", osDefault)
				scratch := t.TempDir()
				want := osDefault
				c := newTestConverter(t, t.TempDir())
				if set {
					c.SetTempDir(scratch)
					want = scratch
				}
				c.freeSpace = plentyOfSpace
				in := writeInput(t, t.TempDir(), "film.mkv", 1024)

				if err := c.Convert(in); err != nil {
					t.Fatalf("Convert: %v", err)
				}
				dirs := tempDirsIn(t, logPath)
				if len(dirs) == 0 {
					t.Fatal("no temporary directory was used")
				}
				for _, d := range dirs {
					if filepath.Dir(d) != want {
						t.Errorf("temporary directory %s, want it directly under %s", d, want)
					}
				}
			})
		}
	}
}

// Hazard: H10
func TestRefusesWhenSpaceShort(t *testing.T) {
	const size = 4096
	for _, p := range profilePaths {
		t.Run(p.name+"/short", func(t *testing.T) {
			logPath := stubTools(t, p.stub)
			scratch, out := t.TempDir(), t.TempDir()
			c := newTestConverter(t, out)
			c.SetTempDir(scratch)
			var asked string
			c.freeSpace = func(dir string) (uint64, error) { asked = dir; return 2*size - 1, nil }
			in := writeInput(t, t.TempDir(), "film.mkv", size)

			err := c.Convert(in)
			if err == nil {
				t.Fatal("Convert returned nil with free space below twice the input")
			}
			for _, fig := range []string{fmt.Sprint(2*size - 1), fmt.Sprint(2 * size)} {
				if !strings.Contains(err.Error(), fig) {
					t.Errorf("error %q does not name %s", err, fig)
				}
			}
			if asked != scratch {
				t.Errorf("free space read on %q, want temp_dir %q", asked, scratch)
			}
			for _, d := range []string{scratch, out} {
				if entries, _ := os.ReadDir(d); len(entries) != 0 {
					t.Errorf("%s holds %d entries, want none", d, len(entries))
				}
			}
			if dirs := tempDirsIn(t, logPath); len(dirs) != 0 {
				t.Errorf("a tool used a temporary directory: %v", dirs)
			}
		})
		t.Run(p.name+"/exactly twice", func(t *testing.T) {
			stubTools(t, p.stub)
			c := newTestConverter(t, t.TempDir())
			c.SetTempDir(t.TempDir())
			c.freeSpace = func(string) (uint64, error) { return 2 * size, nil }
			if err := c.Convert(writeInput(t, t.TempDir(), "film.mkv", size)); err != nil {
				t.Errorf("Convert refused at exactly twice the input: %v", err)
			}
		})
	}
}
