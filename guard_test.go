package main

import (
	"os"
	"strings"
	"testing"
)

// Hazard: H16
func TestRefusesAboveUHD(t *testing.T) {
	for _, p := range profilePaths {
		for _, tc := range []struct {
			w, h   string
			refuse bool
		}{
			{"7680", "4320", true},
			{"3841", "2160", true},
			{"3840", "2161", true},
			{"3840", "2160", false},
			{"1920", "1080", false},
		} {
			t.Run(p.name+"/"+tc.w+"x"+tc.h, func(t *testing.T) {
				logPath := stubTools(t, p.stub)
				stubVideoSize(t, p.stub, tc.w, tc.h)
				outDir := t.TempDir()
				c := newTestConverter(t, outDir)
				c.SetTempDir(t.TempDir())
				c.freeSpace = plentyOfSpace
				in := writeInput(t, t.TempDir(), "film.mkv", 1024)

				err := c.Convert(in)
				entries, rerr := os.ReadDir(outDir)
				if rerr != nil {
					t.Fatal(rerr)
				}
				if !tc.refuse {
					if err != nil {
						t.Fatalf("%sx%s: Convert: %v, want it to convert", tc.w, tc.h, err)
					}
					if len(entries) != 1 {
						t.Errorf("%sx%s: %d outputs, want 1", tc.w, tc.h, len(entries))
					}
					return
				}
				if err == nil {
					t.Fatalf("%sx%s: Convert returned nil, want a refusal", tc.w, tc.h)
				}
				if want := tc.w + "x" + tc.h; !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name the size %s", err, want)
				}
				if n := ffmpegCalls(t, logPath); n != 0 {
					t.Errorf("%d ffmpeg conversion calls before the refusal, want 0", n)
				}
				if len(entries) != 0 {
					t.Errorf("%d outputs after a refusal, want 0", len(entries))
				}
			})
		}
	}
}
