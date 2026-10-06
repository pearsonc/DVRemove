package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func mustWrite(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Hazard: H9
func TestStartClearsJournalledState(t *testing.T) {
	temp, out, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	j := NewJournal(filepath.Join(t.TempDir(), "journal.txt"))

	dir := filepath.Join(temp, "dvremove-123")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "film.hevc"), "x")
	partial := mustWrite(t, filepath.Join(out, "film.mkv.partial"), "x")

	// Each of these is journalled and must survive, as must the unjournalled partial.
	unjournalled := mustWrite(t, filepath.Join(out, "other.mkv.partial"), "x")
	wrongName := mustWrite(t, filepath.Join(temp, "keep.txt"), "x")
	wrongDir := filepath.Join(temp, "library")
	if err := os.Mkdir(wrongDir, 0o755); err != nil {
		t.Fatal(err)
	}
	finished := mustWrite(t, filepath.Join(out, "film.mkv"), "x")
	outside := filepath.Join(elsewhere, "dvremove-outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	outsidePartial := mustWrite(t, filepath.Join(elsewhere, "a.mkv.partial"), "x")
	target := t.TempDir()
	link := filepath.Join(temp, "dvremove-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	dotdot := filepath.Join(temp, "dvremove-q") + "/../keep.txt"
	partialInTemp := mustWrite(t, filepath.Join(temp, "x.mkv.partial"), "x")
	dirNamedPartial := filepath.Join(out, "dir.partial")
	if err := os.Mkdir(dirNamedPartial, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{dir, partial, wrongName, wrongDir, finished, outside, outsidePartial, link, dotdot, partialInTemp, dirNamedPartial} {
		if err := j.Add(p); err != nil {
			t.Fatal(err)
		}
	}

	if err := RecoverJournal(j, temp, out, zerolog.Nop()); err != nil {
		t.Fatalf("RecoverJournal: %v", err)
	}
	for _, gone := range []string{dir, partial} {
		if exists(gone) {
			t.Errorf("journalled %s was not removed", gone)
		}
	}
	for _, kept := range []string{unjournalled, wrongName, wrongDir, finished, outside, outsidePartial, link, target, partialInTemp, dirNamedPartial} {
		if !exists(kept) {
			t.Errorf("%s was removed", kept)
		}
	}
	entries, err := j.Entries()
	if err != nil || len(entries) != 0 {
		t.Errorf("journal after start-up = %v, %v; want empty", entries, err)
	}
}

// Hazard: H9
func TestStartWithNoJournalIsClean(t *testing.T) {
	j := NewJournal(filepath.Join(t.TempDir(), "journal.txt"))
	if err := RecoverJournal(j, t.TempDir(), t.TempDir(), zerolog.Nop()); err != nil {
		t.Fatalf("RecoverJournal with no journal file: %v", err)
	}
}

// journalledConverter returns a converter writing to out, journalling to a file in a new state folder.
func journalledConverter(t *testing.T, out string) (*Converter, *Journal) {
	t.Helper()
	c := newTestConverter(t, out)
	c.SetTempDir(t.TempDir())
	c.freeSpace = plentyOfSpace
	j := NewJournal(filepath.Join(t.TempDir(), "journal.txt"))
	c.SetJournal(j)
	return c, j
}

// Hazard: H9
func TestJournalRecordsBeforeCreation(t *testing.T) {
	for _, p := range profilePaths {
		t.Run(p.name, func(t *testing.T) {
			stubTools(t, p.stub)
			c, j := journalledConverter(t, t.TempDir())
			snap := filepath.Join(t.TempDir(), "snapshot")
			// The tools copy the journal as it stands when they start, before they create
			// the file they write, and note whether that file was already there.
			t.Setenv("STUB_JOURNAL", j.path)
			t.Setenv("STUB_SNAP", snap)
			note := "cat \"$STUB_JOURNAL\" >> \"$STUB_SNAP\" 2>/dev/null; "
			stubFirst(t, map[string]string{
				"mkvmerge": note + "prev=; for a in \"$@\"; do [ \"$prev\" = -o ] && : > \"$a\"; prev=$a; done; exit 0\n",
			})
			// The first tool to create anything in the temporary directory is dovi_tool or ffmpeg.
			stubFirst(t, map[string]string{
				"dovi_tool": note + "prev=; for a in \"$@\"; do [ \"$prev\" = -o ] && : > \"$a\"; prev=$a; done; exit 0\n",
			})
			in := writeInput(t, t.TempDir(), "film.mkv", 1024)
			if err := c.Convert(in); err != nil {
				t.Fatalf("Convert: %v", err)
			}
			data, err := os.ReadFile(snap)
			if err != nil {
				t.Fatalf("no tool ran: %v", err)
			}
			text := string(data)
			if !strings.Contains(text, partialSuffix) {
				t.Errorf("when mkvmerge started the journal did not list the .partial:\n%s", text)
			}
			if p.stub == "dvhe.07" && !strings.Contains(text, "dvremove-") {
				t.Errorf("when dovi_tool started the journal did not list the temporary directory:\n%s", text)
			}
		})
	}
}

// Hazard: H9
func TestJournalLeavesWhatDvremoveRemoves(t *testing.T) {
	for _, p := range profilePaths {
		for _, fail := range []bool{false, true} {
			name := p.name + "/ok"
			if fail {
				name = p.name + "/failed mux"
			}
			t.Run(name, func(t *testing.T) {
				stubTools(t, p.stub)
				if fail {
					stubFirst(t, map[string]string{"mkvmerge": failingMkvmerge})
				}
				out := t.TempDir()
				c, j := journalledConverter(t, out)
				err := c.Convert(writeInput(t, t.TempDir(), "film.mkv", 1024))
				if (err != nil) != fail {
					t.Fatalf("Convert error = %v, want failure %v", err, fail)
				}
				if entries, err := j.Entries(); err != nil || len(entries) != 0 {
					t.Errorf("journal = %v, %v; want empty", entries, err)
				}
			})
		}
	}
}

// Hazard: H9
func TestUnrecordablePathIsNeverCreated(t *testing.T) {
	for _, p := range profilePaths {
		t.Run(p.name, func(t *testing.T) {
			logPath := stubTools(t, p.stub)
			out := t.TempDir()
			c := newTestConverter(t, out)
			scratch := t.TempDir()
			c.SetTempDir(scratch)
			c.freeSpace = plentyOfSpace
			c.SetJournal(NewJournal(filepath.Join(t.TempDir(), "missing", "journal.txt")))

			if err := c.Convert(writeInput(t, t.TempDir(), "film.mkv", 1024)); err == nil {
				t.Fatal("Convert returned nil with a journal it cannot write")
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
	}
}
