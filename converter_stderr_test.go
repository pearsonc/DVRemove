package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Hazard: H8
func TestProfile7StderrIsDrainedAndBounded(t *testing.T) {
	bin := t.TempDir()
	const stderrBytes = 32 << 20
	scripts := map[string]string{
		"ffmpeg":    "#!/bin/sh\nhead -c 32000000 /dev/zero | tr '\\0' x >&2\nprintf ENDMARK >&2\necho data\nexit 0\n",
		"dovi_tool": "#!/bin/sh\ncat > /dev/null\nhead -c 32000000 /dev/zero | tr '\\0' z >&2\nprintf ENDMARK >&2\n: > \"$7\"\nexit 0\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var logged bytes.Buffer
	tc := TranscodeConfig{Encoder: "software", Quality: 28, Preset: "fast", VAAPIDevice: "/dev/dri/renderD128"}
	c := NewConverter(t.TempDir(), t.TempDir(), tc, zerolog.New(zerolog.SyncWriter(&logged)))
	out := filepath.Join(t.TempDir(), "out.hevc")

	done := make(chan error, 1)
	go func() { done <- c.extractAndConvert("in.mkv", out, Profile7) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("extractAndConvert: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("extractAndConvert did not finish: stderr is not drained")
	}
	if n := bytes.Count(logged.Bytes(), []byte("ENDMARK")); n != 2 {
		t.Errorf("the end of stderr reached the log %d times, want once from each of ffmpeg and dovi_tool", n)
	}
	if n := logged.Len(); n > 1<<20 {
		t.Errorf("log holds %d bytes after %d bytes of stderr from two tools, want at most 1 MiB", n, 2*stderrBytes)
	}
}
