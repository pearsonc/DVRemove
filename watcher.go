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
	log        zerolog.Logger
	processing sync.Map // Track files currently being processed
}

// NewWatcher creates a new Watcher instance.
func NewWatcher(inputDir string, converter *Converter, log zerolog.Logger) *Watcher {
	return &Watcher{
		inputDir:  inputDir,
		converter: converter,
		log:       log.With().Str("component", "watcher").Logger(),
	}
}

// ProcessExisting converts any MKV files already in the input directory.
func (w *Watcher) ProcessExisting() error {
	entries, err := os.ReadDir(w.inputDir)
	if err != nil {
		return fmt.Errorf("failed to read input directory %s: %w", w.inputDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".mkv") {
			continue
		}

		filePath := filepath.Join(w.inputDir, entry.Name())
		w.log.Info().Str("file", entry.Name()).Msg("found existing file")

		if err := w.processFile(filePath); err != nil {
			w.log.Error().Err(err).Str("file", entry.Name()).Msg("failed to process existing file")
		}
	}

	return nil
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

		time.Sleep(1 * time.Second)
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
