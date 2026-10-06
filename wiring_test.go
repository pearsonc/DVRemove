package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mainRun is one run of main over a config file and stub tools.
type mainRun struct {
	root, calls, snapshot string
	code                  int
	log, output           string
}

// runMainWired runs `dvremove -once -no-ui` as a child over one Profile 7 input of the given
// size, with extra config keys. The stub mkvmerge copies the journal as it stands while the
// output is being made, into the snapshot.
func runMainWired(t *testing.T, size int64, extra map[string]string) *mainRun {
	t.Helper()
	r := &mainRun{root: lockRoot(t), snapshot: filepath.Join(t.TempDir(), "journal.snapshot")}
	r.calls = stubTools(t, "dvhe.07")
	t.Setenv("STUB_STATE", filepath.Join(r.root, "state"))
	t.Setenv("STUB_SNAPSHOT", r.snapshot)
	stubFirst(t, map[string]string{
		"mkvmerge": `echo "$(basename "$0") $*" >> "$STUB_LOG"
cat "$STUB_STATE/journal.txt" > "$STUB_SNAPSHOT" 2>/dev/null
prev=; for a in "$@"; do [ "$prev" = -o ] && : > "$a"; prev=$a; done; exit 0
`,
		"ffmpeg": `echo "$(basename "$0") $*" >> "$STUB_LOG"
case "$*" in *-filters*) echo ' libplacebo'; exit 0;; *-encoders*) exit 0;; esac
[ -n "$STUB_FFMPEG_SLEEP" ] && sleep "$STUB_FFMPEG_SLEEP"
for a in "$@"; do last=$a; done
[ "$last" != - ] && : > "$last"; exit 0
`,
	})
	writeInput(t, filepath.Join(r.root, "in"), "film.mkv", size)
	r.run(t, extra)
	return r
}

// run runs main again over the same folders.
func (r *mainRun) run(t *testing.T, extra map[string]string) {
	t.Helper()
	keys := map[string]string{
		"input_dir": filepath.Join(r.root, "in"), "output_dir": filepath.Join(r.root, "out"),
		"state_dir": filepath.Join(r.root, "state"), "log_dir": filepath.Join(r.root, "logs"),
		"temp_dir": filepath.Join(r.root, "tmp"),
	}
	for k, v := range extra {
		keys[k] = v
	}
	cmd := exec.Command(os.Args[0], "--", "-config", configIn(t, r.root, keys), "-once", "-no-ui")
	cmd.Env = append(os.Environ(), "DVREMOVE_TEST_MAIN=1")
	out, _ := cmd.CombinedOutput()
	r.output = string(out)
	r.code = cmd.ProcessState.ExitCode()
	log, _ := os.ReadFile(filepath.Join(r.root, "logs", "dvremove.log"))
	r.log = string(log)
}

// Hazard: H3
func TestMainWiresTempDir(t *testing.T) {
	r := runMainWired(t, 1024, nil)
	dirs := tempDirsIn(t, r.calls)
	if r.code != 0 || len(dirs) == 0 {
		t.Fatalf("main exited %d with %d temporary directories used:\n%s%s", r.code, len(dirs), r.output, r.log)
	}
	for _, d := range dirs {
		if filepath.Dir(d) != filepath.Join(r.root, "tmp") {
			t.Errorf("main put temporary files in %s, not under temp_dir %s", d, filepath.Join(r.root, "tmp"))
		}
	}
}

// Hazard: H9
func TestMainWiresJournal(t *testing.T) {
	r := runMainWired(t, 1024, nil)
	snap, _ := os.ReadFile(r.snapshot)
	if r.code != 0 || !strings.Contains(string(snap), tempPrefix) || !strings.Contains(string(snap), partialSuffix) {
		t.Fatalf("main exited %d; the journal while the output was made held %q, want a temp directory and a %s",
			r.code, snap, partialSuffix)
	}
}

// Hazard: H5
func TestMainWiresLedger(t *testing.T) {
	r := runMainWired(t, 1024, nil)
	ledger, _ := os.ReadFile(filepath.Join(r.root, "state", "ledger.txt"))
	if r.code != 0 || !strings.Contains(string(ledger), "film.mkv") {
		t.Fatalf("main exited %d; the ledger holds %q, want film.mkv", r.code, ledger)
	}
	first := ffmpegCalls(t, r.calls)
	if err := os.Remove(filepath.Join(r.root, "out", "film.mkv")); err != nil {
		t.Fatal(err)
	}
	r.run(t, nil)
	if n := ffmpegCalls(t, r.calls); r.code != 0 || n != first || !strings.Contains(r.log, "already converted") {
		t.Errorf("a second main run exited %d with %d ffmpeg calls after %d: it reconverted\n%s", r.code, n, first, r.log)
	}
}

// Hazard: H15
func TestMainWiresGuardLimits(t *testing.T) {
	t.Setenv("STUB_FFMPEG_SLEEP", "4")
	r := runMainWired(t, 1024, map[string]string{"guard_min_available_mib": "1000000000"})
	if r.code == 0 || !strings.Contains(r.log, "memory guard") {
		t.Fatalf("main with guard_min_available_mib above any host's memory exited %d, log:\n%s", r.code, r.log)
	}
}

// Hazard: H10
func TestMainWiresDiskFree(t *testing.T) {
	r := runMainWired(t, 1<<43, nil)
	if r.code == 0 || !strings.Contains(r.log, "not enough free space") || strings.Contains(r.output, "panic") {
		t.Fatalf("main over an 8 TiB input exited %d, want a refusal for free space:\n%s%s", r.code, r.output, r.log)
	}
}
