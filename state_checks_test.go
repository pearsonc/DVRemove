package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// A journal whose last line a killed run cut short still recovers the complete lines before it.
//
// Hazard: H9
func TestJournalCutShortStillRecovers(t *testing.T) {
	temp, out := t.TempDir(), t.TempDir()
	dir := filepath.Join(temp, "dvremove-aaaa")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jp := filepath.Join(t.TempDir(), "journal.txt")
	cut := `"` + filepath.Join(temp, "dvremove-bb")
	if err := os.WriteFile(jp, []byte(`"`+dir+"\"\n"+cut), 0o600); err != nil {
		t.Fatal(err)
	}
	err := RecoverJournal(NewJournal(jp), temp, out, zerolog.Nop())
	if err != nil || exists(dir) {
		t.Fatalf("RecoverJournal over a journal with a cut last line = %v; %s still exists = %v", err, dir, exists(dir))
	}
}

// A complete line that is not a quoted path is still an error: only the unterminated tail is
// forgiven, so a corrupted journal does not pass for a cut one.
//
// Hazard: H9
func TestJournalCorruptCompleteLineStillFails(t *testing.T) {
	jp := filepath.Join(t.TempDir(), "journal.txt")
	if err := os.WriteFile(jp, []byte("not a quoted path\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverJournal(NewJournal(jp), t.TempDir(), t.TempDir(), zerolog.Nop()); err == nil {
		t.Fatal("RecoverJournal accepted a complete line that is not a quoted path")
	}
}

// A /proc/meminfo without swap lines still guards MemAvailable.
//
// Hazard: H15
func TestMemoryGuardWithoutSwapLines(t *testing.T) {
	err := convertWithMeminfo(t, "MemTotal:       67108864 kB\nMemAvailable:    1048576 kB\n", 16*gib)
	if err == nil || !strings.Contains(err.Error(), "memory guard") {
		t.Fatalf("MemAvailable 1 GiB with no swap lines: Convert = %v, want a memory guard stop", err)
	}
}

// A reading with no MemAvailable stops the conversion rather than letting it run unguarded.
//
// Hazard: H15
func TestMemoryGuardWithoutMemAvailableStops(t *testing.T) {
	err := convertWithMeminfo(t, "MemTotal:       67108864 kB\nSwapTotal: 8388608 kB\nSwapFree: 8388608 kB\n", 16*gib)
	if err == nil || !strings.Contains(err.Error(), "memory guard") {
		t.Fatalf("no MemAvailable line: Convert = %v, want a memory guard stop", err)
	}
}

// convertWithMeminfo starts a conversion on a healthy meminfo, swaps in text once ffmpeg runs,
// and returns Convert's error.
func convertWithMeminfo(t *testing.T, text string, avail uint64) error {
	t.Helper()
	stubTools(t, "dvhe.05")
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("STUB_STARTED", started)
	stubFirst(t, map[string]string{"ffmpeg": `echo "$(basename "$0") $*" >> "$STUB_LOG"
case "$*" in *-filters*) echo ' libplacebo'; exit 0;; *-encoders*) exit 0;; esac
: > "$STUB_STARTED"
exec sleep 12
`})
	meminfo := filepath.Join(t.TempDir(), "meminfo")
	writeMeminfo(t, meminfo, avail, 0)
	c := newTestConverter(t, t.TempDir())
	c.SetTempDir(t.TempDir())
	c.freeSpace = plentyOfSpace
	c.memInfoPath = meminfo
	c.guardInterval = 5 * time.Millisecond
	go func() {
		for i := 0; i < 2000; i++ {
			if _, err := os.Stat(started); err == nil {
				os.WriteFile(meminfo+".new", []byte(text), 0o644)
				os.Rename(meminfo+".new", meminfo)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	begin := time.Now()
	err := c.Convert(writeInput(t, t.TempDir(), "film.mkv", 1024))
	if time.Since(begin) > 8*time.Second {
		t.Fatalf("Convert ran %s: the guard did not stop it", time.Since(begin))
	}
	return err
}

// A ledger whose last line has no final newline still skips that title.
//
// Hazard: H5
func TestLedgerHandEditWithoutFinalNewline(t *testing.T) {
	c, in, _, ledgerPath, _, calls := ledgerConverter(t, "dvhe.07")
	input := writeInput(t, in, "film.mkv", 2048)
	if err := os.WriteFile(ledgerPath, []byte("2048\tfilm.mkv"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Convert(input); err != nil {
		t.Fatal(err)
	}
	if n := ffmpegCalls(t, calls); n != 0 {
		t.Fatalf("film.mkv, in the ledger without a final newline, was converted again (%d ffmpeg calls)", n)
	}
}

// The next line appended after an unterminated last line does not join it.
//
// Hazard: H5
func TestLedgerAppendAfterUnterminatedLineKeepsBoth(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ledger")
	if err := os.WriteFile(p, []byte("2048\tfilm.mkv"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLedger(p)
	if err := l.Add("other.mkv", 7); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int64{"film.mkv": 2048, "other.mkv": 7} {
		if ok, err := l.Has(name, size); err != nil || !ok {
			t.Fatalf("Has(%s, %d) = %v, %v after an append past an unterminated line", name, size, ok, err)
		}
	}
}

// A final line with no newline and no complete size is cut short and names nothing.
//
// Hazard: H5
func TestLedgerCutSizeIsIgnored(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ledger")
	if err := os.WriteFile(p, []byte("20"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLedger(p)
	for _, name := range []string{"", "film.mkv"} {
		if ok, _ := l.Has(name, 20); ok {
			t.Fatalf("a cut line %q read as naming %q", "20", name)
		}
	}
}

// A file whose bytes change while its length stays the same is skipped as still being written.
//
// Hazard: H7
func TestPreallocatedFileStillBeingWrittenIsSkipped(t *testing.T) {
	calls := stubTools(t, "dvhe.07")
	in, out := t.TempDir(), t.TempDir()
	filling := writeInput(t, in, "filling.mkv", 1<<20)
	c := newTestConverter(t, out)
	c.freeSpace = plentyOfSpace
	c.SetTempDir(t.TempDir())
	w := NewWatcher(in, c, 1, nil, zerolog.Nop())
	w.stableInterval = 5 * time.Millisecond
	off := int64(0)
	w.sleep = func(time.Duration) {
		f, err := os.OpenFile(filling, os.O_WRONLY, 0)
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		f.WriteAt([]byte(strings.Repeat("v", 4096)), off)
		off += 4096
		time.Sleep(5 * time.Millisecond)
	}
	if err := w.ProcessExisting(); err != nil {
		t.Logf("ProcessExisting: %v", err)
	}
	if n := ffmpegCalls(t, calls); n != 0 || exists(filepath.Join(out, "filling.mkv")) {
		t.Fatalf("a file whose bytes were still being written was converted (%d ffmpeg calls)", n)
	}
}

// Two conversions at once do not each count the same free space.
//
// Hazard: H10
func TestTwoWorkersShareTheFreeSpace(t *testing.T) {
	const size = 4096
	calls := stubTools(t, "dvhe.07")
	in, out := t.TempDir(), t.TempDir()
	writeInput(t, in, "a.mkv", size)
	writeInput(t, in, "b.mkv", size)
	c := newTestConverter(t, out)
	c.SetTempDir(t.TempDir())
	c.freeSpace = func(string) (uint64, error) { return 3 * size, nil }
	t.Setenv("STUB_RUNNING", t.TempDir())
	stubFirst(t, map[string]string{"dovi_tool": `mkdir "$STUB_RUNNING/$$"
echo "dovi_tool running=$(ls "$STUB_RUNNING" | wc -l)" >> "$STUB_LOG"
sleep 1
rmdir "$STUB_RUNNING/$$"
prev=; for a in "$@"; do [ "$prev" = -o ] && : > "$a"; prev=$a; done; exit 0
`})
	w := NewWatcher(in, c, 2, nil, zerolog.Nop())
	w.stableInterval = time.Millisecond
	w.ProcessExisting()
	if strings.Contains(mustRead(t, calls), "running=2") {
		t.Fatalf("two conversions ran at once on free space for one:\n%s", mustRead(t, calls))
	}
}

// A finished conversion gives its claim back, so a later one is not refused for space that is free.
//
// Hazard: H10
func TestFinishedConversionReleasesItsSpaceClaim(t *testing.T) {
	const size = 4096
	stubTools(t, "dvhe.07")
	c := newTestConverter(t, t.TempDir())
	c.SetTempDir(t.TempDir())
	c.freeSpace = func(string) (uint64, error) { return 3 * size, nil }
	for _, name := range []string{"a.mkv", "b.mkv", "c.mkv"} {
		if err := c.Convert(writeInput(t, t.TempDir(), name, size)); err != nil {
			t.Fatalf("Convert(%s) one after another on space for one: %v", name, err)
		}
	}
}
