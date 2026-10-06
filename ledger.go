package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Ledger records the inputs dvremove has converted (H5, D15), one line each: the input's size in
// bytes, a tab, then its file name verbatim. A person deletes a line to have a title converted
// again. A line counts when it ends in a newline, or is the last and names a complete .mkv, so
// a hand edit that leaves no final newline still skips its title. A line cut short before its tab
// or inside its name names nothing, because inputs are .mkv files and a cut name does not end
// so. The one name that slips through is a cut that lands exactly on a shorter .mkv name. Add
// appends a newline first so the next line never joins a cut line.
type Ledger struct {
	path string
	mu   sync.Mutex
}

// NewLedger returns the ledger kept in the file at path, which need not exist yet.
func NewLedger(path string) *Ledger { return &Ledger{path: path} }

// Has reports whether the ledger carries a line for a file of this name and size.
func (l *Ledger) Has(name string, size int64) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read ledger %s: %w", l.path, err)
	}
	defer f.Close()
	want := strconv.FormatInt(size, 10) + "\t" + name
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if errors.Is(err, io.EOF) {
			// A last line with no newline counts when it is a whole line for an .mkv, which is
			// how an editor leaves a hand-trimmed ledger (H5). A cut anywhere before the
			// extension, or before the tab, is not.
			return line == want && strings.HasSuffix(strings.ToLower(name), ".mkv"), nil
		}
		if err != nil {
			return false, fmt.Errorf("failed to read ledger %s: %w", l.path, err)
		}
		if strings.TrimSuffix(line, "\n") == want {
			return true, nil
		}
	}
}

// Add appends a line for a converted input, flushed to disk before it returns.
func (l *Ledger) Add(name string, size int64) error {
	if strings.ContainsAny(name, "\n\r") {
		return fmt.Errorf("file name %q has a line break, which a ledger line cannot carry", name)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	line := strconv.FormatInt(size, 10) + "\t" + name + "\n"
	if cut, err := endsMidLine(l.path); err != nil {
		return err
	} else if cut {
		line = "\n" + line
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open ledger %s: %w", l.path, err)
	}
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		return fmt.Errorf("failed to write ledger %s: %w", l.path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("failed to sync ledger %s: %w", l.path, err)
	}
	return f.Close()
}

// endsMidLine reports whether the file at path ends without a newline.
func endsMidLine(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read ledger %s: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return false, err
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil {
		return false, err
	}
	return last[0] != '\n', nil
}

// SetLedger sets the ledger of converted inputs; a converter without one skips nothing.
func (c *Converter) SetLedger(l *Ledger) { c.ledger = l }

// alreadyConverted reports whether inputPath's name and size are in the ledger, and logs the
// skip. A ledger that cannot be read is no reason to convert again, so it fails the file.
func (c *Converter) alreadyConverted(inputPath string) (bool, error) {
	if c.ledger == nil {
		return false, nil
	}
	info, err := os.Stat(inputPath)
	if err != nil {
		return false, nil // the probe that follows reports it
	}
	name := filepath.Base(inputPath)
	ok, err := c.ledger.Has(name, info.Size())
	if err != nil {
		return false, err
	}
	if ok {
		c.log.Info().Str("file", name).Msg("already converted, skipping")
	}
	return ok, nil
}

// markConverted records an input whose output now has its final name.
func (c *Converter) markConverted(inputPath string) error {
	if c.ledger == nil {
		return nil
	}
	info, err := os.Stat(inputPath)
	if err != nil {
		return fmt.Errorf("failed to stat input %s for the ledger: %w", inputPath, err)
	}
	return c.ledger.Add(filepath.Base(inputPath), info.Size())
}
