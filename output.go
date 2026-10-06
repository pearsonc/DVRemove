package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// tempPrefix starts the name of every temporary directory dvremove makes.
const tempPrefix = "dvremove-"

// SetJournal sets the journal every temporary directory and .partial is recorded in before it is
// created (H9). main always sets one; a converter without one records nothing.
func (c *Converter) SetJournal(j *Journal) { c.journal = j }

// record journals path ahead of its creation.
func (c *Converter) record(path string) error {
	if c.journal == nil {
		return nil
	}
	return c.journal.Add(path)
}

// forget drops path from the journal once dvremove has removed it itself.
func (c *Converter) forget(path string) {
	if c.journal == nil {
		return
	}
	if err := c.journal.Remove(path); err != nil {
		c.log.Warn().Err(err).Str("path", path).Msg("failed to drop a removed path from the journal")
	}
}

// removeTemp removes a temporary directory and, once it is gone, its journal entry.
func (c *Converter) removeTemp(dir string) {
	defer spaceClaims.release(dir)
	if err := os.RemoveAll(dir); err != nil {
		c.log.Warn().Err(err).Str("path", dir).Msg("failed to remove temporary directory, the journal keeps it")
		return
	}
	c.forget(dir)
}

// discardPartial removes a partial, or finds it already renamed away, and drops its journal entry.
func (c *Converter) discardPartial(partial string) {
	if err := os.Remove(partial); err != nil && !errors.Is(err, fs.ErrNotExist) {
		c.log.Warn().Err(err).Str("path", partial).Msg("failed to remove partial output, the journal keeps it")
		return
	}
	c.forget(partial)
}

// partialSuffix ends the name of an output while it is being written. dvremove renames it away
// only once the mux has succeeded, so a failed or killed conversion leaves no final name (H6).
const partialSuffix = ".partial"

// linkFile makes a second name for a file. It is a variable so a test can stand in for a
// filesystem, such as a CIFS mount, where hard links are unsupported.
var linkFile = os.Link

// writeOutput has mux write the output under final's name plus ".partial" and publishes it as
// final only when mux succeeds. A file already at final is never replaced (H11): the call fails
// naming it, and the partial this call made is removed.
func (c *Converter) writeOutput(final string, mux func(partial string) error) error {
	if err := outputNamesFree(final); err != nil {
		return err
	}
	partial := final + partialSuffix
	if err := c.record(partial); err != nil {
		return err
	}
	defer c.discardPartial(partial)
	if err := mux(partial); err != nil {
		return err
	}
	return publishOutput(partial, final)
}

// outputNamesFree returns an error if anything stands at final or at its partial's name, the two
// names a conversion writes. Convert asks it before any work, and writeOutput asks again.
func outputNamesFree(final string) error {
	if err := outputFree(final); err != nil {
		return err
	}
	partial := final + partialSuffix
	if _, err := os.Lstat(partial); err == nil {
		return fmt.Errorf("a file already stands at %s, not overwriting it", partial)
	}
	return nil
}

// outputFree returns an error naming final if anything stands at that name.
func outputFree(final string) error {
	_, err := os.Lstat(final)
	switch {
	case err == nil:
		return fmt.Errorf("output %s already exists, not overwriting it", final)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("failed to check output %s: %w", final, err)
	}
}

// publishOutput gives partial the name final, failing if final is taken. A hard link fails with
// EEXIST where final exists, which no check-then-rename can promise; where the filesystem has no
// hard links, the link error is not EEXIST and the fallback checks and then renames. That leaves a
// window between the check and the rename, which holds here because the run lock keeps every
// other writer out and Chris moves files by hand only between runs.
func publishOutput(partial, final string) error {
	err := linkFile(partial, final)
	switch {
	case err == nil:
		os.Remove(partial)
		return nil
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("output %s already exists, not overwriting it", final)
	}
	if err := outputFree(final); err != nil {
		return err
	}
	if err := os.Rename(partial, final); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", partial, final, err)
	}
	return nil
}

// makeTempDir creates a conversion's temporary directory under temp_dir, or the OS default
// when it is unset, after checking that the filesystem holds twice the input's size.
func (c *Converter) makeTempDir(inputPath string) (string, error) {
	parent := c.tempDir
	if parent == "" {
		parent = os.TempDir()
	}
	info, err := os.Stat(inputPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat input %s: %w", inputPath, err)
	}
	needed := uint64(info.Size()) * 2
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("failed to name temp directory: %w", err)
	}
	dir := filepath.Join(parent, tempPrefix+hex.EncodeToString(suffix[:]))
	// The claim is held from the check until removeTemp, so a conversion that starts meanwhile
	// sees the free space less what this one will write (H10).
	if err := spaceClaims.claim(dir, parent, needed, c.freeSpace); err != nil {
		return "", err
	}
	if err := c.record(dir); err != nil {
		spaceClaims.release(dir)
		return "", err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		spaceClaims.release(dir)
		c.forget(dir)
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	return dir, nil
}

// spaceClaims is the free space the running conversions have claimed, so two checks that read the
// same free space do not both pass for space only one can use (H10). Claims are keyed by the
// filesystem's device, so two paths on one filesystem share a ledger of claims. It is process-wide
// because the run lock admits one dvremove process, and its conversions share the filesystem.
var spaceClaims = &claimBook{byDir: map[string]claim{}}

type claim struct {
	fs    string
	bytes uint64
}

type claimBook struct {
	mu    sync.Mutex
	byDir map[string]claim
}

// fsKey names the filesystem holding dir by its device number, or by its path if that fails.
func fsKey(dir string) string {
	var st syscall.Stat_t
	if err := syscall.Stat(dir, &st); err != nil {
		return dir
	}
	return fmt.Sprintf("dev:%d", st.Dev)
}

// claim reserves needed bytes under dir on parent's filesystem, or fails naming the shortfall.
// Reading the free space and recording the claim happen under one lock.
func (b *claimBook) claim(dir, parent string, needed uint64, free func(string) (uint64, error)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	avail, err := free(parent)
	if err != nil {
		return fmt.Errorf("failed to read free space on %s: %w", parent, err)
	}
	key := fsKey(parent)
	var claimed uint64
	for _, c := range b.byDir {
		if c.fs == key {
			claimed += c.bytes
		}
	}
	if claimed > avail || avail-claimed < needed {
		return fmt.Errorf("not enough free space on %s: %d bytes free, %d already claimed by running conversions, %d bytes needed (twice the input size)",
			parent, avail, claimed, needed)
	}
	b.byDir[dir] = claim{fs: key, bytes: needed}
	return nil
}

// release gives back dir's claim, whether its conversion finished or failed.
func (b *claimBook) release(dir string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.byDir, dir)
}
