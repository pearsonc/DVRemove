package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

// Journal is the state folder's journal.txt: the temporary directories and .partial files a run
// has made or is about to make, one Go-quoted path per line. A run that is killed leaves its
// entries behind, and the next run's start-up removes exactly those paths (H9).
type Journal struct {
	path string
	mu   sync.Mutex
}

// NewJournal returns a journal kept at path. The file is created by the first Add.
func NewJournal(path string) *Journal { return &Journal{path: path} }

// Add records path, and returns only once the line is on disk, so the caller creates path after.
func (j *Journal) Add(path string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open journal %s: %w", j.path, err)
	}
	if _, err := f.WriteString(strconv.Quote(path) + "\n"); err != nil {
		f.Close()
		return fmt.Errorf("failed to write journal %s: %w", j.path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("failed to sync journal %s: %w", j.path, err)
	}
	return f.Close()
}

// Remove drops every line naming path. A path dvremove removes itself leaves the journal.
func (j *Journal) Remove(path string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	entries, err := j.read()
	if err != nil {
		return err
	}
	var kept []string
	for _, e := range entries {
		if e != path {
			kept = append(kept, e)
		}
	}
	return j.write(kept)
}

// Entries returns the journalled paths in order. A missing file is an empty journal.
func (j *Journal) Entries() ([]string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.read()
}

func (j *Journal) read() ([]string, error) {
	data, err := os.ReadFile(j.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read journal %s: %w", j.path, err)
	}
	var entries []string
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		p, err := strconv.Unquote(line)
		if err != nil {
			return nil, fmt.Errorf("journal %s holds a line that is not a quoted path: %s", j.path, line)
		}
		entries = append(entries, p)
	}
	return entries, nil
}

// write replaces the journal with entries, through a rename so a kill leaves the old or the new.
func (j *Journal) write(entries []string) error {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(strconv.Quote(e) + "\n")
	}
	tmp := j.path + ".new"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("failed to write journal %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, j.path); err != nil {
		return fmt.Errorf("failed to replace journal %s: %w", j.path, err)
	}
	return nil
}

// RecoverJournal is run at start-up. It removes each journalled path that passes journalledKind
// for tempRoot and outputDir, logs and leaves any other, and then clears the journal.
func RecoverJournal(j *Journal, tempRoot, outputDir string, log zerolog.Logger) error {
	entries, err := j.Entries()
	if err != nil {
		return err
	}
	for _, p := range entries {
		kind := journalledKind(p, tempRoot, outputDir)
		var rerr error
		switch kind {
		case "tempdir":
			rerr = os.RemoveAll(p)
		case "partial":
			rerr = os.Remove(p)
		default:
			log.Warn().Str("path", p).Msg("journalled path is not one dvremove makes, leaving it")
			continue
		}
		if rerr != nil {
			return fmt.Errorf("failed to remove journalled %s %s: %w", kind, p, rerr)
		}
		log.Info().Str("path", p).Str("kind", kind).Msg("removed state left by an earlier run")
	}
	return j.write(nil)
}

// journalledKind returns "tempdir" for a directory directly inside tempRoot named dvremove-*, and
// "partial" for a regular file directly inside outputDir whose name ends .partial, and "" for
// anything else, including a path that is not clean and absolute, a symlink, or one whose parent
// resolves elsewhere.
func journalledKind(path, tempRoot, outputDir string) string {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ""
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return ""
	}
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}
	base := filepath.Base(path)
	if sameDir(parent, tempRoot) && strings.HasPrefix(base, tempPrefix) && info.IsDir() {
		return "tempdir"
	}
	if sameDir(parent, outputDir) && strings.HasSuffix(base, partialSuffix) && info.Mode().IsRegular() {
		return "partial"
	}
	return ""
}

func sameDir(resolved, dir string) bool {
	real, err := filepath.EvalSymlinks(dir)
	return err == nil && real == resolved
}
