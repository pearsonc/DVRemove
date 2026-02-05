package main

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Job represents a file to be converted.
type Job struct {
	FilePath string
}

// Result represents the outcome of a conversion job.
type Result struct {
	FilePath string
	Duration time.Duration
	Err      error
}

// WorkerPool manages concurrent conversion workers.
type WorkerPool struct {
	workers   int
	jobs      chan Job
	results   chan Result
	wg        sync.WaitGroup
	converter *Converter
	ui        *UI
	log       zerolog.Logger
}

// NewWorkerPool creates a new worker pool.
func NewWorkerPool(workers int, converter *Converter, ui *UI, log zerolog.Logger) *WorkerPool {
	return &WorkerPool{
		workers:   workers,
		jobs:      make(chan Job, workers*2),
		results:   make(chan Result, workers*2),
		converter: converter,
		ui:        ui,
		log:       log.With().Str("component", "pool").Logger(),
	}
}

// Start launches the worker goroutines.
func (p *WorkerPool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// worker processes jobs from the channel.
func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()

	for job := range p.jobs {
		started := time.Now()
		p.log.Debug().Int("worker", id).Str("file", job.FilePath).Msg("processing")

		// Detect profile first for UI display
		profile, _ := p.converter.DetectProfile(job.FilePath)
		profileStr := profile.String()

		// Update UI to show active
		if p.ui != nil {
			p.ui.UpdateWorker(id, WorkerStatus{
				Active:   true,
				FilePath: job.FilePath,
				Profile:  profileStr,
				Progress: 0,
				Started:  started,
			})
		}

		// Set up progress callback for this worker
		progressCb := func(prog Progress) {
			if p.ui != nil {
				p.ui.UpdateWorker(id, WorkerStatus{
					Active:   true,
					FilePath: job.FilePath,
					Profile:  profileStr,
					Progress: prog.Percent,
					Speed:    prog.Speed,
					ETA:      prog.ETA,
					Started:  started,
				})
			}
		}

		// Run conversion with progress callback
		err := p.converter.Convert(job.FilePath, progressCb)
		duration := time.Since(started)

		// Update UI with completion
		if p.ui != nil {
			p.ui.WorkerComplete(id, job.FilePath, err == nil, duration)
		}

		p.results <- Result{FilePath: job.FilePath, Duration: duration, Err: err}
	}
}

// Submit adds a job to the pool.
func (p *WorkerPool) Submit(job Job) {
	p.jobs <- job
}

// Close signals no more jobs will be submitted.
func (p *WorkerPool) Close() {
	close(p.jobs)
}

// Wait blocks until all workers complete.
func (p *WorkerPool) Wait() {
	p.wg.Wait()
	close(p.results)
}

// Results returns the results channel for reading.
func (p *WorkerPool) Results() <-chan Result {
	return p.results
}

// Drain reads and discards all results (use when UI handles completion).
func (p *WorkerPool) Drain() (processed, failed int) {
	for result := range p.results {
		processed++
		if result.Err != nil {
			failed++
			p.log.Error().Err(result.Err).Str("file", filepath.Base(result.FilePath)).Msg("conversion failed")
		}
	}
	return processed, failed
}
