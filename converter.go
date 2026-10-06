package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/rs/zerolog"
)

// DVProfile represents a Dolby Vision profile.
type DVProfile int

const (
	ProfileUnknown DVProfile = iota
	Profile5
	Profile7
	Profile8
)

func (p DVProfile) String() string {
	switch p {
	case Profile5:
		return "Profile 5"
	case Profile7:
		return "Profile 7"
	case Profile8:
		return "Profile 8"
	default:
		return "Unknown"
	}
}

// Converter handles DV profile conversion.
type Converter struct {
	inputDir  string
	outputDir string
	transcode TranscodeConfig
	gpuDetect *GPUDetector
	log       zerolog.Logger
	tempDir   string // where temporary files go; empty means the OS default
	// freeSpace reports the bytes available to a non-root user on dir's filesystem.
	freeSpace func(dir string) (uint64, error)
	journal   *Journal // paths recorded before they are created; nil records nothing
	ledger    *Ledger  // converted inputs, skipped by name and size; nil skips nothing
	guardSettings
}

// SetTempDir sets the directory temporary files are created in; empty means the OS default.
func (c *Converter) SetTempDir(dir string) { c.tempDir = dir }

func diskFree(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// NewConverter creates a new Converter instance.
func NewConverter(inputDir, outputDir string, transcode TranscodeConfig, log zerolog.Logger) *Converter {
	componentLog := log.With().Str("component", "converter").Logger()
	return &Converter{
		inputDir:  inputDir,
		outputDir: outputDir,
		transcode: transcode,
		gpuDetect: NewGPUDetector(transcode.VAAPIDevice, log),
		log:       componentLog,
		freeSpace: diskFree,

		guardSettings: defaultGuardSettings(),
	}
}

// MediaInfoOutput represents the JSON output from mediainfo.
type MediaInfoOutput struct {
	Media struct {
		Track []struct {
			Type             string `json:"@type"`
			Width            string `json:"Width"`
			Height           string `json:"Height"`
			HDRFormat        string `json:"HDR_Format"`
			HDRFormatProfile string `json:"HDR_Format_Profile"`
		} `json:"track"`
	} `json:"media"`
}

// DetectProfile analyses an MKV file and returns its DV profile.
func (c *Converter) DetectProfile(filePath string) (DVProfile, error) {
	profile, _, _, err := c.probeVideo(filePath)
	return profile, err
}

// probeVideo returns a file's DV profile and the largest width and height of its video tracks,
// which are 0 where mediainfo reports none.
func (c *Converter) probeVideo(filePath string) (profile DVProfile, width, height int, err error) {
	c.log.Debug().Str("file", filePath).Msg("detecting DV profile")

	cmd := exec.Command("mediainfo", "--Output=JSON", filePath)
	output, err := cmd.Output()
	if err != nil {
		return ProfileUnknown, 0, 0, fmt.Errorf("failed to run mediainfo on %s: %w", filePath, err)
	}

	var info MediaInfoOutput
	if err := json.Unmarshal(output, &info); err != nil {
		return ProfileUnknown, 0, 0, fmt.Errorf("failed to parse mediainfo output for %s: %w", filePath, err)
	}

	// ffmpeg's default selection takes the largest video track, whichever one carries the
	// Dolby Vision, so the size is the largest over every video track (H16).
	profile = ProfileUnknown
	for _, track := range info.Media.Track {
		if track.Type != "Video" {
			continue
		}
		w, _ := strconv.Atoi(track.Width)
		h, _ := strconv.Atoi(track.Height)
		width, height = max(width, w), max(height, h)

		if profile != ProfileUnknown || !strings.Contains(track.HDRFormat, "Dolby Vision") {
			continue
		}
		profileStr := track.HDRFormatProfile
		c.log.Debug().Str("file", filePath).Str("profile", profileStr).Msg("found DV profile string")

		switch {
		case strings.Contains(profileStr, "dvhe.05") || strings.Contains(profileStr, "Profile 5"):
			profile = Profile5
		case strings.Contains(profileStr, "dvhe.07") || strings.Contains(profileStr, "Profile 7"):
			profile = Profile7
		case strings.Contains(profileStr, "dvhe.08") || strings.Contains(profileStr, "Profile 8"):
			profile = Profile8
		}
	}

	return profile, width, height, nil
}

// Convert processes a single file and converts it to Profile 8.1.
// The optional progress callback receives encoding progress updates.
func (c *Converter) Convert(inputPath string, progressCb ...ProgressCallback) error {
	filename := filepath.Base(inputPath)
	c.log.Info().Str("file", filename).Msg("starting conversion")

	var cb ProgressCallback
	if len(progressCb) > 0 {
		cb = progressCb[0]
	}

	if done, err := c.alreadyConverted(inputPath); err != nil || done {
		return err
	}

	profile, width, height, err := c.probeVideo(inputPath)
	if err != nil {
		return fmt.Errorf("failed to detect profile for %s: %w", filename, err)
	}

	c.log.Info().Str("file", filename).Str("profile", profile.String()).Msg("detected profile")

	if profile == Profile5 || profile == Profile7 {
		// A size mediainfo does not report is a refusal, not a pass (H16, D21).
		if width == 0 || height == 0 {
			return fmt.Errorf("refusing %s: mediainfo reported no video size (%dx%d)", filename, width, height)
		}
		if err := refuseAboveUHD(filename, width, height); err != nil {
			return err
		}
		// A name already taken is refused before any work (H11); publishing checks again,
		// since the name can be taken during the run.
		if err := outputNamesFree(filepath.Join(c.outputDir, filename)); err != nil {
			return err
		}
	}

	switch profile {
	case Profile5:
		return c.transcodeProfile5(inputPath, cb)
	case Profile7:
		// Profile 7 has BT.2020 base layer, HDR10 fallback works
	case Profile8:
		c.log.Info().Str("file", filename).Msg("already Profile 8, skipping")
		return nil
	default:
		c.log.Warn().Str("file", filename).Msg("not a DV file or unknown profile, skipping")
		return nil
	}

	// Create unique temp directory for this conversion
	tempDir, err := c.makeTempDir(inputPath)
	if err != nil {
		return err
	}
	defer c.removeTemp(tempDir)

	baseName := strings.TrimSuffix(filename, filepath.Ext(filename))
	tempHEVC := filepath.Join(tempDir, baseName+".hevc")
	tempHEVCWithMeta := filepath.Join(tempDir, baseName+".hdr10.hevc")

	// Step 1: Extract and convert DV metadata
	if err := c.extractAndConvert(inputPath, tempHEVC, profile); err != nil {
		return fmt.Errorf("failed to extract and convert %s: %w", filename, err)
	}

	// Step 2: Inject HDR10 colour metadata for non-DV player fallback
	if err := c.injectHDR10Metadata(tempHEVC, tempHEVCWithMeta); err != nil {
		return fmt.Errorf("failed to inject HDR10 metadata for %s: %w", filename, err)
	}

	// Step 3: Remux with original audio/subtitles
	outputPath := filepath.Join(c.outputDir, filename)
	if err := c.writeOutput(outputPath, func(partial string) error {
		return c.remux(inputPath, tempHEVCWithMeta, partial)
	}); err != nil {
		return fmt.Errorf("failed to remux %s: %w", filename, err)
	}

	c.log.Info().Str("file", filename).Str("output", outputPath).Msg("conversion complete")
	return c.markConverted(inputPath)
}

// stderrLogLimit bounds the stderr of one tool held in memory and written to the log.
const stderrLogLimit = 64 << 10

// extractAndConvert extracts the HEVC stream and converts DV metadata.
func (c *Converter) extractAndConvert(inputPath, outputPath string, profile DVProfile) error {
	c.log.Debug().Str("input", inputPath).Str("output", outputPath).Msg("extracting and converting HEVC")

	// Only Profile 7 is supported (Profile 5 skipped in Convert due to IPT colour space)
	if profile != Profile7 {
		return fmt.Errorf("unsupported profile for conversion: %s", profile)
	}

	// ffmpeg command to extract HEVC in annexb format
	ffmpegCmd := exec.Command("ffmpeg",
		"-i", inputPath,
		"-c:v", "copy",
		"-bsf:v", "hevc_mp4toannexb",
		"-f", "hevc",
		"-",
	)

	// dovi_tool command to convert Profile 7 to Profile 8.1
	// -m 2: Convert to profile 8.1 compatible, removes mapping
	// --discard: Remove enhancement layer if present
	doviArgs := []string{"-m", "2", "convert", "-", "--discard", "-o", outputPath}

	doviCmd := exec.Command("dovi_tool", doviArgs...)

	// The pipe between them is made here, not by StdoutPipe, so the parent can close both of its
	// ends once the children hold theirs. A parent that kept the read end would leave ffmpeg
	// blocked on a full pipe after dovi_tool exited, instead of failing on the closed one (H1).
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create the ffmpeg to dovi_tool pipe: %w", err)
	}
	defer pipeR.Close()
	defer pipeW.Close()
	ffmpegCmd.Stdout = pipeW
	doviCmd.Stdin = pipeR

	// Capture stderr for error messages
	ffmpegStderr, err := ffmpegCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create ffmpeg stderr pipe: %w", err)
	}
	doviStderr, err := doviCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create dovi_tool stderr pipe: %w", err)
	}

	// Start both commands
	guard := c.newMemGuard()
	if err := ffmpegCmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}
	if err := doviCmd.Start(); err != nil {
		ffmpegCmd.Process.Kill()
		ffmpegCmd.Wait()
		return fmt.Errorf("failed to start dovi_tool: %w", err)
	}
	pipeR.Close()
	pipeW.Close()

	stopGuard := guard.watch(ffmpegCmd.Process, doviCmd.Process)

	// Each command's stderr is drained to its end, keeping only a bounded tail for the log,
	// before its Wait, which closes the pipe. The first command to fail ends the other, so no
	// process of the pipeline outlives the failure.
	var finished sync.WaitGroup
	var ffmpegErr, doviErr error
	var failOnce sync.Once
	var failedFirst string
	for _, p := range []struct {
		name   string
		cmd    *exec.Cmd
		other  *exec.Cmd
		stderr io.Reader
		result *error
	}{
		{"ffmpeg", ffmpegCmd, doviCmd, ffmpegStderr, &ffmpegErr},
		{"dovi_tool", doviCmd, ffmpegCmd, doviStderr, &doviErr},
	} {
		finished.Add(1)
		go func() {
			defer finished.Done()
			if tail := tailCapture(p.stderr, stderrLogLimit); len(tail) > 0 {
				c.log.Debug().Str("source", p.name).Msg(string(tail))
			}
			if *p.result = p.cmd.Wait(); *p.result != nil {
				failOnce.Do(func() { failedFirst = p.name })
				p.other.Process.Kill()
			}
		}()
	}
	finished.Wait()
	if err := stopGuard(); err != nil {
		return err
	}

	// Report the command that failed first: the other may only have been killed because of it.
	if failedFirst == "dovi_tool" && doviErr != nil {
		return fmt.Errorf("dovi_tool failed: %w", doviErr)
	}
	if ffmpegErr != nil {
		return fmt.Errorf("ffmpeg failed: %w", ffmpegErr)
	}
	if doviErr != nil {
		return fmt.Errorf("dovi_tool failed: %w", doviErr)
	}

	return nil
}

// injectHDR10Metadata adds HDR10 colour metadata to the HEVC stream for non-DV player fallback.
// Uses ffmpeg's hevc_metadata bitstream filter to set BT.2020 PQ colour space.
func (c *Converter) injectHDR10Metadata(inputPath, outputPath string) error {
	c.log.Debug().Str("input", inputPath).Str("output", outputPath).Msg("injecting HDR10 metadata")

	// HDR10 colour parameters:
	// colour_primaries=9 (BT.2020)
	// transfer_characteristics=16 (SMPTE ST 2084 / PQ)
	// matrix_coefficients=9 (BT.2020 non-constant luminance)
	cmd := exec.Command("ffmpeg",
		"-y",
		"-i", inputPath,
		"-c:v", "copy",
		"-bsf:v", "hevc_metadata=colour_primaries=9:transfer_characteristics=16:matrix_coefficients=9",
		"-f", "hevc",
		outputPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		c.log.Error().Str("output", string(output)).Msg("ffmpeg metadata injection failed")
		return fmt.Errorf("ffmpeg metadata injection failed: %w", err)
	}

	c.log.Debug().Msg("HDR10 metadata injected successfully")
	return nil
}

// remux combines the converted video with original audio and subtitles.
func (c *Converter) remux(originalPath, videoPath, outputPath string) error {
	c.log.Debug().Str("original", originalPath).Str("video", videoPath).Str("output", outputPath).Msg("remuxing")

	// mkvmerge -o output.mkv converted.hevc -D input.mkv
	// -D: don't copy video from input (we're using converted video)
	cmd := exec.Command("mkvmerge",
		"-o", outputPath,
		videoPath,
		"-D", originalPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		c.log.Error().Str("output", string(output)).Msg("mkvmerge failed")
		return fmt.Errorf("mkvmerge failed: %w", err)
	}

	c.log.Debug().Str("output", string(output)).Msg("mkvmerge output")
	return nil
}

// transcodeProfile5 transcodes a Profile 5 file using NVDEC + libplacebo + NVENC.
// Pipeline: NVDEC decode (preserves DV RPU) -> libplacebo (IPT→BT.2020) -> NVENC encode -> mux with audio.
// libplacebo performs the colour space conversion from DV's IPT to standard BT.2020.
func (c *Converter) transcodeProfile5(inputPath string, progressCb ProgressCallback) error {
	filename := filepath.Base(inputPath)
	outputPath := filepath.Join(c.outputDir, filename)
	baseName := strings.TrimSuffix(filename, filepath.Ext(filename))

	encoder := c.gpuDetect.DetectEncoder(c.transcode.Encoder)
	hasLibplacebo := c.gpuDetect.HasLibplacebo()

	c.log.Info().
		Str("file", filename).
		Str("encoder", string(encoder)).
		Bool("libplacebo", hasLibplacebo).
		Int("quality", c.transcode.Quality).
		Str("preset", c.transcode.Preset).
		Msg("transcoding Profile 5 file")

	if !hasLibplacebo {
		return fmt.Errorf("libplacebo not available - required for Profile 5 colour conversion")
	}

	// Create unique temp directory for this conversion
	tempDir, err := c.makeTempDir(inputPath)
	if err != nil {
		return err
	}
	defer c.removeTemp(tempDir)

	transcodedVideo := filepath.Join(tempDir, baseName+".transcoded.mkv")

	// Get video duration for progress calculation
	duration, err := getVideoDuration(inputPath)
	if err != nil {
		c.log.Warn().Err(err).Msg("could not get video duration, progress will be estimated")
		duration = 0
	}

	// Step 1: Transcode with NVDEC + libplacebo + hardware encode
	var args []string
	switch encoder {
	case EncoderNVENC:
		args = c.libplaceboNvencArgs(inputPath, transcodedVideo)
	case EncoderVAAPI:
		args = c.libplaceboVaapiArgs(inputPath, transcodedVideo)
	default:
		args = c.libplaceboSoftwareArgs(inputPath, transcodedVideo)
	}

	cmd := exec.Command("ffmpeg", args...)
	guard := c.newMemGuard()
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	stopGuard := guard.watch(cmd.Process)

	// Parse progress from stderr
	if progressCb != nil {
		ffmpegProgress(stderr, duration, progressCb)
	} else {
		io.Copy(io.Discard, stderr)
	}

	waitErr := cmd.Wait()
	if err := stopGuard(); err != nil {
		return err
	}
	if err := waitErr; err != nil {
		c.log.Error().Msg("libplacebo transcode failed")
		return fmt.Errorf("ffmpeg libplacebo transcode failed: %w", err)
	}

	// Step 2: Mux with original audio/subtitles
	if err := c.writeOutput(outputPath, func(partial string) error {
		return c.muxWithAudio(inputPath, transcodedVideo, partial)
	}); err != nil {
		return fmt.Errorf("failed to mux audio/subtitles: %w", err)
	}

	c.log.Info().Str("file", filename).Str("output", outputPath).Msg("transcode complete")
	return c.markConverted(inputPath)
}

// muxWithAudio combines transcoded video with original audio and subtitles.
func (c *Converter) muxWithAudio(originalPath, videoPath, outputPath string) error {
	c.log.Debug().Str("original", originalPath).Str("video", videoPath).Msg("muxing audio/subs")

	cmd := exec.Command("mkvmerge",
		"-o", outputPath,
		videoPath,
		"-D", originalPath, // -D: no video from original (we use transcoded)
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		c.log.Error().Str("output", string(output)).Msg("mkvmerge failed")
		return fmt.Errorf("mkvmerge failed: %w", err)
	}

	return nil
}
