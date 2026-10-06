package main

import (
	"io"
	"strings"
	"testing"
	"time"
)

// Hazard: H8
func TestProgressDrainsAfterLongLine(t *testing.T) {
	pr, pw := io.Pipe()
	var got []Progress
	parsed := make(chan struct{})
	go func() {
		ffmpegProgress(pr, time.Minute, func(p Progress) { got = append(got, p) })
		close(parsed)
	}()

	written := make(chan error, 1)
	go func() {
		chunk := strings.Repeat("x", 1<<20)
		for i := 0; i < 16; i++ {
			if _, err := io.WriteString(pw, chunk); err != nil {
				written <- err
				return
			}
		}
		_, err := io.WriteString(pw, "\nframe=  10 fps=5.0 time=00:00:30.00 bitrate=100kbits/s speed=2.0x\n"+
			strings.Repeat("y", 1<<20)+"\n")
		pw.Close()
		written <- err
	}()

	select {
	case err := <-written:
		if err != nil {
			t.Fatalf("writer: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the writer is blocked: nothing drains stderr after a 16 MiB line")
	}
	select {
	case <-parsed:
	case <-time.After(10 * time.Second):
		t.Fatal("ffmpegProgress did not return at end of input")
	}
	if len(got) != 1 || got[0].Percent != 50 {
		t.Errorf("progress after the long line = %+v, want one update at 50%%", got)
	}
}
