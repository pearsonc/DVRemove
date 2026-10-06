package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// Watcher monitors a directory for new MKV files.
type Watcher struct {
	inputDir   string
	converter  *Converter
	maxWorkers int
	ui         *UI
	log        zerolog.Logger
	processing sync.Map // Track files currently being processed

	// stableInterval is the wait between size checks, and sleep performs it; a test injects both
	// so the check does not wait real seconds.
	stableInterval time.Duration
	sleep          func(time.Duration)
}

const (
	defaultStableInterval = time.Second
	stableChecks          = 3 // size checks, one interval apart, a file must survive unchanged
)

// NewWatcher creates a new Watcher instance.
func NewWatcher(inputDir string, converter *Converter, maxWorkers int, ui *UI, log zerolog.Logger) *Watcher {
	return &Watcher{
		inputDir:   inputDir,
		converter:  converter,
		maxWorkers: maxWorkers,
		ui:         ui,
		log:        log.With().Str("component", "watcher").Logger(),

		stableInterval: defaultStableInterval,
		sleep:          time.Sleep,
	}
}

// BatchError reports a batch in which some files were left unconverted.
type BatchError struct {
	Failed, Total int
}

func (e *BatchError) Error() string {
	return fmt.Sprintf("%d of %d files were not converted", e.Failed, e.Total)
}

// ProcessExisting converts any MKV files already in the input directory.
// Reads directory once, submits all jobs to worker pool, waits for completion.
// It returns a *BatchError when any conversion failed.
func (w *Watcher) ProcessExisting() error {
	entries, err := os.ReadDir(w.inputDir)
	if err != nil {
		return fmt.Errorf("failed to read input directory %s: %w", w.inputDir, err)
	}

	// Collect MKV files
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".mkv") {
			continue
		}
		files = append(files, filepath.Join(w.inputDir, entry.Name()))
	}

	files = w.dropGrowing(files)

	if len(files) == 0 {
		w.log.Info().Msg("no files to process")
		if w.ui != nil {
			w.ui.PrintInfo("No files to process")
		}
		return nil
	}

	w.log.Info().Int("files", len(files)).Int("workers", w.maxWorkers).Msg("starting batch processing")

	// Initialize UI
	if w.ui != nil {
		w.ui.Initialize(len(files))
	}

	// Create and start worker pool
	pool := NewWorkerPool(w.maxWorkers, w.converter, w.ui, w.log)
	pool.Start()

	// Submit all jobs
	go func() {
		for _, filePath := range files {
			w.log.Info().Str("file", filepath.Base(filePath)).Msg("queued")
			pool.Submit(Job{FilePath: filePath})
		}
		pool.Close()
	}()

	// Close the results channel once the workers finish, while Drain reads it: a worker
	// blocks on a full results channel, so Wait before Drain never returns.
	go pool.Wait()
	processed, failed := pool.Drain()

	// Print summary
	if w.ui != nil {
		w.ui.PrintSummary()
	}

	w.log.Info().Int("processed", processed).Int("failed", failed).Msg("batch complete")
	if failed > 0 {
		return &BatchError{Failed: failed, Total: processed}
	}
	return nil
}

// dropGrowing removes from files each whose size changes during the stability check (H7). A
// skipped file stays in the input folder for the next run, and is not a failure.
func (w *Watcher) dropGrowing(files []string) []string {
	growing := make([]bool, len(files))
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		go func(i int, f string) {
			defer wg.Done()
			grown, err := w.isGrowing(f)
			if err != nil {
				w.log.Warn().Err(err).Str("file", filepath.Base(f)).Msg("could not check the file's size, skipping it")
				grown = true
			} else if grown {
				w.log.Info().Str("file", filepath.Base(f)).Msg("file is still growing, skipping it until the next run")
			}
			growing[i] = grown
		}(i, f)
	}
	wg.Wait()
	var steady []string
	for i, f := range files {
		if !growing[i] {
			steady = append(steady, f)
		}
	}
	return steady
}

// isGrowing reports whether the file's size changes across stableChecks waits.
func (w *Watcher) isGrowing(filePath string) (bool, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to stat file: %w", err)
	}
	size := info.Size()
	for i := 0; i < stableChecks; i++ {
		w.sleep(w.stableInterval)
		info, err := os.Stat(filePath)
		if err != nil {
			return false, fmt.Errorf("failed to stat file: %w", err)
		}
		if info.Size() != size {
			return true, nil
		}
	}
	return false, nil
}

// Watch starts watching the input directory for new files.
func (w *Watcher) Watch() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(w.inputDir); err != nil {
		return fmt.Errorf("failed to watch directory %s: %w", w.inputDir, err)
	}

	w.log.Info().Str("dir", w.inputDir).Msg("watching directory for new files")

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			w.handleEvent(event)

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			w.log.Error().Err(err).Msg("watcher error")
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	// Only care about Write events (file created or modified)
	if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
		return
	}

	// Only process MKV files
	if !strings.HasSuffix(strings.ToLower(event.Name), ".mkv") {
		return
	}

	// Check if already processing this file
	if _, loaded := w.processing.LoadOrStore(event.Name, true); loaded {
		return
	}

	// Wait for file to finish writing
	go func() {
		defer w.processing.Delete(event.Name)

		if err := w.waitForStable(event.Name); err != nil {
			w.log.Error().Err(err).Str("file", event.Name).Msg("file not stable")
			return
		}

		if err := w.processFile(event.Name); err != nil {
			w.log.Error().Err(err).Str("file", event.Name).Msg("failed to process file")
		}
	}()
}

// waitForStable waits until a file stops changing size.
func (w *Watcher) waitForStable(filePath string) error {
	var lastSize int64 = -1
	stableCount := 0
	maxWait := 60 // Maximum wait iterations

	for i := 0; i < maxWait; i++ {
		info, err := os.Stat(filePath)
		if err != nil {
			return fmt.Errorf("failed to stat file: %w", err)
		}

		currentSize := info.Size()
		if currentSize == lastSize {
			stableCount++
			if stableCount >= 3 {
				// File size stable for 3 checks (3 seconds)
				return nil
			}
		} else {
			stableCount = 0
			lastSize = currentSize
		}

		w.sleep(w.stableInterval)
	}

	return fmt.Errorf("file did not stabilise within timeout")
}

func (w *Watcher) processFile(filePath string) error {
	filename := filepath.Base(filePath)
	w.log.Info().Str("file", filename).Msg("processing file")

	if err := w.converter.Convert(filePath); err != nil {
		return fmt.Errorf("conversion failed for %s: %w", filename, err)
	}

	return nil
}
