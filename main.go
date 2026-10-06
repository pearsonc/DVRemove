package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	oneShot := flag.Bool("once", false, "process existing files and exit (no watching)")
	noUI := flag.Bool("no-ui", false, "disable terminal UI (headless mode)")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	log, err := setupLogger(cfg.LogDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup logger: %v\n", err)
		os.Exit(1)
	}

	log.Info().
		Str("input_dir", cfg.InputDir).
		Str("output_dir", cfg.OutputDir).
		Int("max_workers", cfg.Parallel.MaxWorkers).
		Bool("one_shot", *oneShot).
		Msg("dvremove starting")

	// Create UI unless disabled
	var ui *UI
	if !*noUI {
		ui = NewUI(cfg.Parallel.MaxWorkers)
	}

	converter := NewConverter(cfg.InputDir, cfg.OutputDir, cfg.Transcode, log)
	converter.SetTempDir(cfg.TempDir)
	watcher := NewWatcher(cfg.InputDir, converter, cfg.Parallel.MaxWorkers, ui, log)

	// Process any existing files
	if err := watcher.ProcessExisting(); err != nil {
		log.Error().Err(err).Msg("failed to process existing files")
	}

	if *oneShot {
		log.Info().Msg("one-shot mode, exiting")
		return
	}

	// Start watching for new files
	log.Info().Msg("starting file watcher")
	if err := watcher.Watch(); err != nil {
		log.Fatal().Err(err).Msg("watcher failed")
	}
}

func setupLogger(logDir string) (zerolog.Logger, error) {
	logPath := filepath.Join(logDir, "dvremove.log")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return zerolog.Logger{}, fmt.Errorf("failed to open log file %s: %w", logPath, err)
	}

	zerolog.TimeFieldFormat = time.RFC3339

	log := zerolog.New(file).
		With().
		Timestamp().
		Logger()

	return log, nil
}
