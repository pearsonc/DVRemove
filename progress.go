package main

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Progress represents ffmpeg encoding progress.
type Progress struct {
	Percent int
	Speed   string
	ETA     string
	Frame   int
	Fps     float64
	Time    time.Duration
	Bitrate string
}

// ProgressCallback is called with progress updates during encoding.
type ProgressCallback func(Progress)

// ffmpegProgress parses ffmpeg stderr for progress information.
// Returns channel that receives progress updates.
func ffmpegProgress(stderr io.Reader, totalDuration time.Duration, callback ProgressCallback) {
	reader := bufio.NewReader(stderr)

	// Regex patterns for ffmpeg output
	timeRe := regexp.MustCompile(`time=(\d+):(\d+):(\d+)\.(\d+)`)
	speedRe := regexp.MustCompile(`speed=\s*([0-9.]+)x`)
	frameRe := regexp.MustCompile(`frame=\s*(\d+)`)
	fpsRe := regexp.MustCompile(`fps=\s*([0-9.]+)`)
	bitrateRe := regexp.MustCompile(`bitrate=\s*([0-9.]+\s*[kMG]?bits/s)`)

	for {
		line, ok := readFFmpegLine(reader)
		if !ok {
			return
		}
		if !strings.Contains(line, "time=") {
			continue
		}

		var progress Progress

		// Parse time
		if matches := timeRe.FindStringSubmatch(line); len(matches) == 5 {
			hours, _ := strconv.Atoi(matches[1])
			mins, _ := strconv.Atoi(matches[2])
			secs, _ := strconv.Atoi(matches[3])
			ms, _ := strconv.Atoi(matches[4])
			progress.Time = time.Duration(hours)*time.Hour +
				time.Duration(mins)*time.Minute +
				time.Duration(secs)*time.Second +
				time.Duration(ms*10)*time.Millisecond
		}

		// Calculate percent
		if totalDuration > 0 && progress.Time > 0 {
			progress.Percent = int(float64(progress.Time) / float64(totalDuration) * 100)
			if progress.Percent > 100 {
				progress.Percent = 100
			}
		}

		// Parse speed
		if matches := speedRe.FindStringSubmatch(line); len(matches) == 2 {
			progress.Speed = matches[1] + "x"
		}

		// Calculate ETA
		if progress.Speed != "" && totalDuration > 0 {
			speedVal, _ := strconv.ParseFloat(strings.TrimSuffix(progress.Speed, "x"), 64)
			if speedVal > 0 {
				remaining := totalDuration - progress.Time
				eta := time.Duration(float64(remaining) / speedVal)
				progress.ETA = formatETA(eta)
			}
		}

		// Parse frame
		if matches := frameRe.FindStringSubmatch(line); len(matches) == 2 {
			progress.Frame, _ = strconv.Atoi(matches[1])
		}

		// Parse fps
		if matches := fpsRe.FindStringSubmatch(line); len(matches) == 2 {
			progress.Fps, _ = strconv.ParseFloat(matches[1], 64)
		}

		// Parse bitrate
		if matches := bitrateRe.FindStringSubmatch(line); len(matches) == 2 {
			progress.Bitrate = matches[1]
		}

		callback(progress)
	}
}

// maxProgressLine caps what is held of one stderr line; the rest of a longer line is read and dropped.
const maxProgressLine = 64 << 10

// readFFmpegLine returns the next line of ffmpeg's stderr, ending at \r or \n, holding at most
// maxProgressLine bytes of it. It reads a longer line to its end, so the writer never blocks on
// an unread pipe. ok is false once the input is exhausted and nothing is left.
func readFFmpegLine(r *bufio.Reader) (line string, ok bool) {
	var buf []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return string(buf), len(buf) > 0
		}
		if b == '\r' || b == '\n' {
			return string(buf), true
		}
		if len(buf) < maxProgressLine {
			buf = append(buf, b)
		}
	}
}

// tailCapture reads r to its end and returns the last max bytes of it. The whole stream is
// drained, so the writer never blocks, and what is held in memory stays bounded.
func tailCapture(r io.Reader, max int) []byte {
	var tail []byte
	chunk := make([]byte, 32<<10)
	for {
		n, err := r.Read(chunk)
		tail = append(tail, chunk[:n]...)
		if len(tail) > 2*max {
			tail = append(tail[:0], tail[len(tail)-max:]...)
		}
		if err != nil {
			break
		}
	}
	if len(tail) > max {
		tail = tail[len(tail)-max:]
	}
	return tail
}

// formatETA formats a duration as MM:SS or HH:MM:SS.
func formatETA(d time.Duration) string {
	if d < 0 {
		return "00:00"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// getVideoDuration uses ffprobe to get the video duration.
func getVideoDuration(filePath string) (time.Duration, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		filePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w", err)
	}

	durationStr := strings.TrimSpace(string(output))
	durationSec, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration %q: %w", durationStr, err)
	}

	return time.Duration(durationSec * float64(time.Second)), nil
}
