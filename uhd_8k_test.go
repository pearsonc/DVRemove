package main

import "testing"

// Hazard: H16
// A Profile 5 file whose first video track is a non-DV 8K stream. ffmpeg's default stream
// selection takes the largest, and the libplacebo args carry no -map.
func TestRefusesFileHoldingAn8KTrack(t *testing.T) {
	logPath := stubTools(t, "dvhe.05")
	stubFirst(t, map[string]string{
		"mediainfo": `echo "$(basename "$0") $*" >> "$STUB_LOG"
echo '{"media":{"track":[{"@type":"Video","Width":"7680","Height":"4320"},{"@type":"Video","Width":"3840","Height":"2160","HDR_Format":"Dolby Vision","HDR_Format_Profile":"dvhe.05"}]}}'
`})
	c := newTestConverter(t, t.TempDir())
	c.SetTempDir(t.TempDir())
	c.freeSpace = plentyOfSpace
	err := c.Convert(writeInput(t, t.TempDir(), "film.mkv", 1024))
	if err == nil || ffmpegCalls(t, logPath) != 0 {
		t.Fatalf("a file holding a 7680x4320 video track was converted: err=%v, ffmpeg calls=%d", err, ffmpegCalls(t, logPath))
	}
}
