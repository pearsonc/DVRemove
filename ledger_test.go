package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// ledgerConverter returns a software-encoding converter over a fresh input and output folder,
// with a ledger in a state folder and its log in the returned buffer.
func ledgerConverter(t *testing.T, profile string) (c *Converter, in, out, ledgerPath string, logs *lockedBuffer, calls string) {
	t.Helper()
	calls = stubTools(t, profile)
	in, out = t.TempDir(), t.TempDir()
	ledgerPath = filepath.Join(t.TempDir(), "ledger.txt")
	logs = &lockedBuffer{}
	tc := TranscodeConfig{Encoder: "software", Quality: 28, Preset: "fast", VAAPIDevice: "/dev/dri/renderD128"}
	c = NewConverter(in, out, tc, zerolog.New(logs))
	c.freeSpace = plentyOfSpace
	c.SetTempDir(t.TempDir())
	c.SetLedger(NewLedger(ledgerPath))
	return c, in, out, ledgerPath, logs, calls
}

// Hazard: H5
func TestLedgerSkipsConvertedInput(t *testing.T) {
	for _, p := range profilePaths {
		t.Run(p.name, func(t *testing.T) {
			c, in, out, ledgerPath, logs, calls := ledgerConverter(t, p.stub)
			input := writeInput(t, in, "film.mkv", 2048)
			if err := c.Convert(input); err != nil {
				t.Fatalf("first Convert: %v", err)
			}
			data, err := os.ReadFile(ledgerPath)
			if err != nil {
				t.Fatalf("no ledger after a conversion: %v", err)
			}
			if got := strings.Count(string(data), "film.mkv"); got != 1 || !strings.Contains(string(data), "2048") {
				t.Fatalf("ledger = %q, want one line naming film.mkv and its size 2048", data)
			}
			before := ffmpegCalls(t, calls)
			mkvBefore := strings.Count(mustRead(t, calls), "mkvmerge ")
			if err := os.Remove(filepath.Join(out, "film.mkv")); err != nil {
				t.Fatal(err)
			}
			if err := c.Convert(input); err != nil {
				t.Fatalf("second Convert: %v", err)
			}
			if got := ffmpegCalls(t, calls); got != before {
				t.Errorf("ffmpeg ran %d more times for an input already converted", got-before)
			}
			if got := strings.Count(mustRead(t, calls), "mkvmerge "); got != mkvBefore {
				t.Errorf("mkvmerge ran %d more times for an input already converted", got-mkvBefore)
			}
			if exists(filepath.Join(out, "film.mkv")) {
				t.Error("the output was made again")
			}
			if !strings.Contains(logs.String(), "already converted") {
				t.Errorf("no log message carries %q:\n%s", "already converted", logs.String())
			}
		})
	}
}

// Hazard: H5
func TestLedgerKeysOnNameAndSizeAndEditsByHand(t *testing.T) {
	c, in, out, ledgerPath, _, calls := ledgerConverter(t, "dvhe.07")
	input := writeInput(t, in, "film.mkv", 2048)
	if err := c.Convert(input); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(out, "film.mkv"))

	// The same name at another size is another file: it converts.
	writeInput(t, in, "film.mkv", 4096)
	n := ffmpegCalls(t, calls)
	if err := c.Convert(input); err != nil {
		t.Fatal(err)
	}
	if ffmpegCalls(t, calls) == n {
		t.Error("a file of the same name and another size was skipped")
	}
	os.Remove(filepath.Join(out, "film.mkv"))

	// Deleting the line reconverts the title.
	if err := os.WriteFile(ledgerPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	writeInput(t, in, "film.mkv", 2048)
	n = ffmpegCalls(t, calls)
	if err := c.Convert(input); err != nil {
		t.Fatal(err)
	}
	if ffmpegCalls(t, calls) == n {
		t.Error("a title whose ledger line was deleted was not converted again")
	}
}

// Hazard: H5
func TestLedgerSurvivesLineCutShortByKill(t *testing.T) {
	c, in, out, ledgerPath, _, calls := ledgerConverter(t, "dvhe.07")
	// A killed run left "2048\tfilm.m" with no newline: it names another file, and our next line
	// must not be glued to it.
	if err := os.WriteFile(ledgerPath, []byte("1024\tother.mkv\n2048\tfilm.m"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeInput(t, in, "film.m", 2048)
	n := ffmpegCalls(t, calls)
	// film.m is not an .mkv, so Convert is called on it directly: the cut line must not match it.
	if err := c.Convert(filepath.Join(in, "film.m")); err != nil {
		t.Fatal(err)
	}
	if ffmpegCalls(t, calls) == n {
		t.Error("a line without its newline was taken as a converted input")
	}
	os.Remove(filepath.Join(out, "film.m"))
	input := writeInput(t, in, "film.mkv", 512)
	if err := c.Convert(input); err != nil {
		t.Fatal(err)
	}
	l := NewLedger(ledgerPath)
	for name, size := range map[string]int64{"other.mkv": 1024, "film.m": 2048, "film.mkv": 512} {
		if name == "film.m" {
			continue // the cut line stays cut
		}
		if ok, err := l.Has(name, size); err != nil || !ok {
			t.Errorf("Has(%s, %d) = %v, %v; want true", name, size, ok, err)
		}
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
