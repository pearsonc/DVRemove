package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

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
	if err := outputFree(final); err != nil {
		return err
	}
	partial := final + partialSuffix
	if _, err := os.Lstat(partial); err == nil {
		return fmt.Errorf("a file already stands at %s, not overwriting it", partial)
	}
	if err := mux(partial); err != nil {
		os.Remove(partial)
		return err
	}
	if err := publishOutput(partial, final); err != nil {
		os.Remove(partial)
		return err
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
	free, err := c.freeSpace(parent)
	if err != nil {
		return "", fmt.Errorf("failed to read free space on %s: %w", parent, err)
	}
	if free < needed {
		return "", fmt.Errorf("not enough free space on %s: %d bytes free, %d bytes needed (twice the input size %d)",
			parent, free, needed, info.Size())
	}
	dir, err := os.MkdirTemp(parent, "dvremove-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	return dir, nil
}
