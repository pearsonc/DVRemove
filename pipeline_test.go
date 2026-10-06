package main

import (
	"testing"
	"time"
)

// Hazard: H1
// H1 and H8's class: a child of the Profile 7 pipe blocks on a pipe nobody drains. dovi_tool
// fails at once without reading its input, so ffmpeg's stdout fills; the parent still holds the
// pipe's read end, so ffmpeg blocks, its stderr never ends, and Convert never returns.
func TestDoviToolFailingEarlyDoesNotHang(t *testing.T) {
	stubTools(t, "dvhe.07")
	stubFirst(t, map[string]string{
		"ffmpeg": `echo "$(basename "$0") $*" >> "$STUB_LOG"
case "$*" in *-filters*) echo ' libplacebo'; exit 0;; *-encoders*) exit 0;; esac
for a in "$@"; do last=$a; done
if [ "$last" = - ]; then head -c 8388608 /dev/zero; exit 0; fi
: > "$last"; exit 0
`,
		"dovi_tool": "echo 'dovi_tool: invalid RPU' >&2; exit 1\n",
	})
	c := newTestConverter(t, t.TempDir())
	c.SetTempDir(t.TempDir())
	c.freeSpace = plentyOfSpace
	in := writeInput(t, t.TempDir(), "film.mkv", 1024)
	done := make(chan error, 1)
	go func() { done <- c.Convert(in) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Convert returned nil although dovi_tool failed")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Convert did not return within 20s after dovi_tool failed: ffmpeg is blocked on its stdout pipe")
	}
}

// Hazard: H1
// The other early exit: ffmpeg fails before writing anything. dovi_tool reads end of file, and
// both children must be gone when Convert returns.
func TestFfmpegFailingEarlyEndsThePipeline(t *testing.T) {
	stubTools(t, "dvhe.07")
	stubFirst(t, map[string]string{
		"ffmpeg": `echo "$(basename "$0") $*" >> "$STUB_LOG"
case "$*" in *-filters*) echo ' libplacebo'; exit 0;; *-encoders*) exit 0;; esac
echo 'ffmpeg: no such stream' >&2; exit 1
`,
		"dovi_tool": "cat > /dev/null; exit 0\n",
	})
	c := newTestConverter(t, t.TempDir())
	c.SetTempDir(t.TempDir())
	c.freeSpace = plentyOfSpace
	in := writeInput(t, t.TempDir(), "film.mkv", 1024)
	done := make(chan error, 1)
	go func() { done <- c.Convert(in) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Convert returned nil although ffmpeg failed")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Convert did not return within 20s after ffmpeg failed")
	}
}
