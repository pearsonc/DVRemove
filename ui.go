package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// ANSI escape codes
const (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Cyan      = "\033[36m"
	Green     = "\033[32m"
	Yellow    = "\033[33m"
	Red       = "\033[31m"
	Blue      = "\033[34m"
	Magenta   = "\033[35m"
	Gray      = "\033[90m"
	White     = "\033[97m"
	ClearLine = "\033[2K"
	CursorUp  = "\033[%dA"
	CursorTo  = "\033[%d;%dH"
)

// WorkerStatus represents the current state of a worker.
type WorkerStatus struct {
	Active   bool
	FilePath string
	Profile  string
	Progress int       // 0-100
	Speed    string    // e.g., "2.5x"
	ETA      string    // e.g., "00:01:23"
	Started  time.Time
}

// UI handles terminal output with real-time progress display.
type UI struct {
	mu           sync.Mutex
	workers      []WorkerStatus
	maxWorkers   int
	totalFiles   int
	completed    int
	failed       int
	headerLines  int
	initialized  bool
	colorEnabled bool
}

// NewUI creates a new UI instance.
func NewUI(maxWorkers int) *UI {
	return &UI{
		workers:      make([]WorkerStatus, maxWorkers),
		maxWorkers:   maxWorkers,
		colorEnabled: true,
		headerLines:  0,
	}
}

// c wraps text in color codes.
func (u *UI) c(color, text string) string {
	if !u.colorEnabled {
		return text
	}
	return color + text + Reset
}

// getTerminalWidth returns terminal width or default.
func (u *UI) getTerminalWidth() int {
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
		return width
	}
	return 80
}

// Initialize sets up the UI and draws the header.
func (u *UI) Initialize(totalFiles int) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.totalFiles = totalFiles

	// Clear screen
	fmt.Print("\033[2J\033[H")

	// Draw header
	width := 60
	fmt.Println(u.c(Cyan, "╔"+strings.Repeat("═", width-2)+"╗"))

	title := "D V R E M O V E"
	padding := (width - 2 - len(title)) / 2
	fmt.Printf("%s║%s%s%s║%s\n",
		u.c(Cyan, ""),
		strings.Repeat(" ", padding),
		u.c(Cyan+Bold, title),
		strings.Repeat(" ", width-2-padding-len(title)),
		Reset)

	subtitle := "Dolby Vision Converter"
	padding = (width - 2 - len(subtitle)) / 2
	fmt.Printf("%s║%s%s%s║%s\n",
		u.c(Cyan, ""),
		strings.Repeat(" ", padding),
		u.c(Gray, subtitle),
		strings.Repeat(" ", width-2-padding-len(subtitle)),
		Reset)

	fmt.Println(u.c(Cyan, "╚"+strings.Repeat("═", width-2)+"╝"))
	fmt.Println()

	// Status line
	fmt.Printf("  %s %d file(s) queued, %d worker(s)\n\n",
		u.c(Blue, "◆"),
		totalFiles,
		u.maxWorkers)

	u.headerLines = 7

	// Draw worker slots
	for i := 0; i < u.maxWorkers; i++ {
		fmt.Printf("  %s Worker %d: %s\n",
			u.c(Gray, "○"),
			i+1,
			u.c(Gray, "idle"))
	}

	fmt.Println()
	fmt.Println(u.c(Gray, strings.Repeat("─", 60)))
	fmt.Println()

	u.initialized = true
}

// UpdateWorker updates a worker's status.
func (u *UI) UpdateWorker(workerID int, status WorkerStatus) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if workerID < 0 || workerID >= u.maxWorkers {
		return
	}

	u.workers[workerID] = status
	u.redrawWorker(workerID)
}

// redrawWorker redraws a single worker line (must hold mutex).
func (u *UI) redrawWorker(workerID int) {
	if !u.initialized {
		return
	}

	// Calculate line position (header + blank + status + blank + worker lines)
	line := u.headerLines + workerID

	// Move cursor to worker line
	fmt.Printf("\033[%d;1H%s", line, ClearLine)

	status := u.workers[workerID]
	if !status.Active {
		fmt.Printf("  %s Worker %d: %s",
			u.c(Gray, "○"),
			workerID+1,
			u.c(Gray, "idle"))
	} else {
		// Truncate filename if too long
		filename := truncateFilename(status.FilePath, 25)
		progressBar := u.makeProgressBar(status.Progress, 20)

		var speedInfo string
		if status.Speed != "" {
			speedInfo = fmt.Sprintf(" %s", status.Speed)
		}
		var etaInfo string
		if status.ETA != "" {
			etaInfo = fmt.Sprintf(" ETA %s", status.ETA)
		}

		profileColor := Magenta
		if status.Profile == "Profile 7" {
			profileColor = Blue
		}

		fmt.Printf("  %s Worker %d: [%s] %3d%% %s %s%s%s",
			u.c(Green, "●"),
			workerID+1,
			progressBar,
			status.Progress,
			u.c(profileColor, status.Profile),
			u.c(White, filename),
			u.c(Yellow, speedInfo),
			u.c(Gray, etaInfo))
	}

	// Move cursor back to bottom
	fmt.Printf("\033[%d;1H", u.headerLines+u.maxWorkers+4)
}

// WorkerComplete marks a worker as completed and logs result.
func (u *UI) WorkerComplete(workerID int, filePath string, success bool, duration time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if workerID >= 0 && workerID < u.maxWorkers {
		u.workers[workerID] = WorkerStatus{Active: false}
	}

	if success {
		u.completed++
	} else {
		u.failed++
	}

	// Redraw worker as idle
	u.redrawWorker(workerID)

	// Move to log area and print result
	fmt.Printf("\033[%d;1H", u.headerLines+u.maxWorkers+4)

	filename := truncateFilename(filePath, 40)
	if success {
		fmt.Printf("  %s %s %s\n",
			u.c(Green, "✓"),
			filename,
			u.c(Gray, fmt.Sprintf("(%s)", formatDuration(duration))))
	} else {
		fmt.Printf("  %s %s %s\n",
			u.c(Red, "✗"),
			filename,
			u.c(Red, "failed"))
	}
}

// PrintSummary prints the final summary.
func (u *UI) PrintSummary() {
	u.mu.Lock()
	defer u.mu.Unlock()

	fmt.Println()
	fmt.Println(u.c(Gray, strings.Repeat("─", 60)))
	fmt.Printf("\n  %s Complete: %s%d%s processed, %s%d%s failed\n\n",
		u.c(Cyan, "◆"),
		Green, u.completed, Reset,
		Red, u.failed, Reset)
}

// PrintError prints an error message.
func (u *UI) PrintError(msg string) {
	u.mu.Lock()
	defer u.mu.Unlock()

	fmt.Printf("  %s %s\n", u.c(Red, "✗"), msg)
}

// PrintInfo prints an info message.
func (u *UI) PrintInfo(msg string) {
	u.mu.Lock()
	defer u.mu.Unlock()

	fmt.Printf("  %s %s\n", u.c(Gray, "○"), msg)
}

// makeProgressBar creates a text progress bar.
func (u *UI) makeProgressBar(percent int, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := (percent * width) / 100
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	if percent == 100 {
		return u.c(Green, bar)
	}
	return u.c(Cyan, bar)
}

// truncateFilename shortens a filename for display.
func truncateFilename(path string, maxLen int) string {
	// Get just the filename
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]

	if len(name) <= maxLen {
		return name
	}
	return name[:maxLen-3] + "..."
}

// formatDuration formats a duration for display.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}
