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
	Percent  int
	Speed    string
	ETA      string
	Frame    int
	Fps      float64
	Time     time.Duration
	Bitrate  string
}

// ProgressCallback is called with progress updates during encoding.
type ProgressCallback func(Progress)

// ffmpegProgress parses ffmpeg stderr for progress information.
// Returns channel that receives progress updates.
func ffmpegProgress(stderr io.Reader, totalDuration time.Duration, callback ProgressCallback) {
	scanner := bufio.NewScanner(stderr)
	scanner.Split(scanFFmpegLines)

	// Regex patterns for ffmpeg output
	timeRe := regexp.MustCompile(`time=(\d+):(\d+):(\d+)\.(\d+)`)
	speedRe := regexp.MustCompile(`speed=\s*([0-9.]+)x`)
	frameRe := regexp.MustCompile(`frame=\s*(\d+)`)
	fpsRe := regexp.MustCompile(`fps=\s*([0-9.]+)`)
	bitrateRe := regexp.MustCompile(`bitrate=\s*([0-9.]+\s*[kMG]?bits/s)`)

	for scanner.Scan() {
		line := scanner.Text()
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

// scanFFmpegLines is a split function for bufio.Scanner that handles ffmpeg's carriage return output.
func scanFFmpegLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	// Look for \r or \n
	for i := 0; i < len(data); i++ {
		if data[i] == '\r' || data[i] == '\n' {
			return i + 1, data[:i], nil
		}
	}

	// If at EOF, return what we have
	if atEOF {
		return len(data), data, nil
	}

	// Request more data
	return 0, nil, nil
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
