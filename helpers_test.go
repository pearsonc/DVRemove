package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubTools puts stand-ins for ffmpeg, ffprobe, mediainfo, mkvmerge and dovi_tool first on
// PATH, so no test runs a real one. profile is the mediainfo answer, such as "dvhe.05".
// Every call is appended to the returned log file as "name args...", and each stub creates the
// file named after -o, or ffmpeg's last argument unless it is "-".
func stubTools(t *testing.T, profile string) (logPath string) {
	t.Helper()
	bin := t.TempDir()
	logPath = filepath.Join(t.TempDir(), "calls.log")
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	const record = "echo \"$(basename \"$0\") $*\" >> \"$STUB_LOG\"\n"
	const touchO = "prev=; for a in \"$@\"; do [ \"$prev\" = -o ] && : > \"$a\"; prev=$a; done; exit 0\n"
	scripts := map[string]string{
		"mediainfo": record + "echo '{\"media\":{\"track\":[{\"@type\":\"Video\",\"HDR_Format\":\"Dolby Vision\",\"HDR_Format_Profile\":\"'\"$STUB_PROFILE\"'\"}]}}'\n",
		"ffprobe":   record + "echo 60\n",
		"ffmpeg": record +
			"case \"$*\" in *-filters*) echo ' libplacebo';; *-encoders*) ;; esac\n" +
			"for a in \"$@\"; do last=$a; done\n" +
			"case \"$*\" in *-filters*|*-encoders*) ;; *) [ \"$last\" != - ] && : > \"$last\";; esac; exit 0\n",
		"dovi_tool": record + touchO,
		"mkvmerge":  record + touchO,
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STUB_LOG", logPath)
	t.Setenv("STUB_PROFILE", profile)
	return logPath
}

// tempDirsIn returns the distinct dvremove-* directories named in the stub call log.
func tempDirsIn(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var dirs []string
	for _, field := range strings.Fields(string(data)) {
		i := strings.Index(field, "dvremove-")
		if i < 0 {
			continue
		}
		rest := field[i:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			rest = rest[:j]
		}
		dir := filepath.Join(field[:i], rest)
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// writeInput creates a file of the given size and returns its path.
func writeInput(t *testing.T, dir, name string, size int64) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// stubFirst writes executables named in scripts into a new directory placed first on PATH, so
// they shadow the ones stubTools wrote.
func stubFirst(t *testing.T, scripts map[string]string) {
	t.Helper()
	bin := t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// stubVideoSize shadows mediainfo with one that reports a Dolby Vision video track of the given
// size, as mediainfo does: Width and Height are strings.
func stubVideoSize(t *testing.T, profile, width, height string) {
	t.Helper()
	stubFirst(t, map[string]string{
		"mediainfo": "echo \"$(basename \"$0\") $*\" >> \"$STUB_LOG\"\n" +
			"echo '{\"media\":{\"track\":[{\"@type\":\"Video\",\"Width\":\"" + width + "\",\"Height\":\"" + height +
			"\",\"HDR_Format\":\"Dolby Vision\",\"HDR_Format_Profile\":\"" + profile + "\"}]}}'\n",
	})
}

// ffmpegCalls counts the ffmpeg calls in the stub log other than the capability probes.
func ffmpegCalls(t *testing.T, logPath string) int {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ffmpeg ") && !strings.Contains(line, "-filters") && !strings.Contains(line, "-encoders") {
			n++
		}
	}
	return n
}
