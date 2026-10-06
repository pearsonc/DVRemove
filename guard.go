package main

import "fmt"

// Largest video dimensions dvremove converts (H16, D19): above these, decoding exhausts the
// host's memory, which the GPU shares.
const (
	maxVideoWidth  = 3840
	maxVideoHeight = 2160
)

// refuseAboveUHD returns an error naming the size when a video is wider than 3840 or taller than
// 2160, so the file is refused before any ffmpeg starts. A size of 0 means mediainfo reported none.
func refuseAboveUHD(filename string, width, height int) error {
	if width > maxVideoWidth || height > maxVideoHeight {
		return fmt.Errorf("refusing %s: video is %dx%d, above the %dx%d limit",
			filename, width, height, maxVideoWidth, maxVideoHeight)
	}
	return nil
}
