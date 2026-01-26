package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	inputDir   string
	outputDir  string
	transcode  TranscodeConfig
	gpuDetect  *GPUDetector
	log        zerolog.Logger
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
	}
}

// MediaInfoOutput represents the JSON output from mediainfo.
type MediaInfoOutput struct {
	Media struct {
		Track []struct {
			Type             string `json:"@type"`
			HDRFormat        string `json:"HDR_Format"`
			HDRFormatProfile string `json:"HDR_Format_Profile"`
		} `json:"track"`
	} `json:"media"`
}

// DetectProfile analyses an MKV file and returns its DV profile.
func (c *Converter) DetectProfile(filePath string) (DVProfile, error) {
	c.log.Debug().Str("file", filePath).Msg("detecting DV profile")

	cmd := exec.Command("mediainfo", "--Output=JSON", filePath)
	output, err := cmd.Output()
	if err != nil {
		return ProfileUnknown, fmt.Errorf("failed to run mediainfo on %s: %w", filePath, err)
	}

	var info MediaInfoOutput
	if err := json.Unmarshal(output, &info); err != nil {
		return ProfileUnknown, fmt.Errorf("failed to parse mediainfo output for %s: %w", filePath, err)
	}

	for _, track := range info.Media.Track {
		if track.Type != "Video" {
			continue
		}

		if !strings.Contains(track.HDRFormat, "Dolby Vision") {
			continue
		}

		profile := track.HDRFormatProfile
		c.log.Debug().Str("file", filePath).Str("profile", profile).Msg("found DV profile string")

		if strings.Contains(profile, "dvhe.05") || strings.Contains(profile, "Profile 5") {
			return Profile5, nil
		}
		if strings.Contains(profile, "dvhe.07") || strings.Contains(profile, "Profile 7") {
			return Profile7, nil
		}
		if strings.Contains(profile, "dvhe.08") || strings.Contains(profile, "Profile 8") {
			return Profile8, nil
		}
	}

	return ProfileUnknown, nil
}

// Convert processes a single file and converts it to Profile 8.1.
func (c *Converter) Convert(inputPath string) error {
	filename := filepath.Base(inputPath)
	c.log.Info().Str("file", filename).Msg("starting conversion")

	profile, err := c.DetectProfile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to detect profile for %s: %w", filename, err)
	}

	c.log.Info().Str("file", filename).Str("profile", profile.String()).Msg("detected profile")

	switch profile {
	case Profile5:
		return c.transcodeProfile5(inputPath)
	case Profile7:
		// Profile 7 has BT.2020 base layer, HDR10 fallback works
	case Profile8:
		c.log.Info().Str("file", filename).Msg("already Profile 8, skipping")
		return nil
	default:
		c.log.Warn().Str("file", filename).Msg("not a DV file or unknown profile, skipping")
		return nil
	}

	// Create temp files for conversion pipeline
	tempDir := os.TempDir()
	baseName := strings.TrimSuffix(filename, filepath.Ext(filename))
	tempHEVC := filepath.Join(tempDir, baseName+".hevc")
	tempHEVCWithMeta := filepath.Join(tempDir, baseName+".hdr10.hevc")
	defer os.Remove(tempHEVC)
	defer os.Remove(tempHEVCWithMeta)

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
	if err := c.remux(inputPath, tempHEVCWithMeta, outputPath); err != nil {
		return fmt.Errorf("failed to remux %s: %w", filename, err)
	}

	c.log.Info().Str("file", filename).Str("output", outputPath).Msg("conversion complete")
	return nil
}

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

	// Pipe ffmpeg output to dovi_tool input
	pipe, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create ffmpeg stdout pipe: %w", err)
	}
	doviCmd.Stdin = pipe

	// Capture stderr for error messages
	ffmpegStderr, _ := ffmpegCmd.StderrPipe()
	doviStderr, _ := doviCmd.StderrPipe()

	// Start both commands
	if err := ffmpegCmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}
	if err := doviCmd.Start(); err != nil {
		ffmpegCmd.Process.Kill()
		return fmt.Errorf("failed to start dovi_tool: %w", err)
	}

	// Read stderr in background
	go func() {
		stderrBytes, _ := io.ReadAll(ffmpegStderr)
		if len(stderrBytes) > 0 {
			c.log.Debug().Str("source", "ffmpeg").Msg(string(stderrBytes))
		}
	}()
	go func() {
		stderrBytes, _ := io.ReadAll(doviStderr)
		if len(stderrBytes) > 0 {
			c.log.Debug().Str("source", "dovi_tool").Msg(string(stderrBytes))
		}
	}()

	// Wait for both to complete
	ffmpegErr := ffmpegCmd.Wait()
	doviErr := doviCmd.Wait()

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
func (c *Converter) transcodeProfile5(inputPath string) error {
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

	// Create temp file for transcoded video
	tempDir := os.TempDir()
	transcodedVideo := filepath.Join(tempDir, baseName+".transcoded.mkv")
	defer os.Remove(transcodedVideo)

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
	output, err := cmd.CombinedOutput()
	if err != nil {
		c.log.Error().Str("output", string(output)).Msg("libplacebo transcode failed")
		return fmt.Errorf("ffmpeg libplacebo transcode failed: %w", err)
	}

	// Step 2: Mux with original audio/subtitles
	if err := c.muxWithAudio(inputPath, transcodedVideo, outputPath); err != nil {
		return fmt.Errorf("failed to mux audio/subtitles: %w", err)
	}

	c.log.Info().Str("file", filename).Str("output", outputPath).Msg("transcode complete")
	return nil
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

// libplaceboNvencArgs returns ffmpeg arguments for NVDEC + libplacebo + NVENC pipeline.
// Uses NVDEC hardware decode which preserves DV RPU metadata, libplacebo for IPT→BT.2020
// colour conversion via Vulkan, and NVENC for hardware encode.
func (c *Converter) libplaceboNvencArgs(input, output string) []string {
	preset := c.nvencPreset()
	vf := "hwdownload,format=p010,libplacebo=apply_dolbyvision=true:colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084"
	return []string{
		"-y",
		"-hwaccel", "cuda",
		"-hwaccel_output_format", "cuda",
		"-init_hw_device", "vulkan=vk",
		"-filter_hw_device", "vk",
		"-i", input,
		"-vf", vf,
		"-c:v", "hevc_nvenc",
		"-preset", preset,
		"-profile:v", "main10",
		"-cq", fmt.Sprintf("%d", c.transcode.Quality),
		"-b:v", "0",
		"-rc", "vbr",
		"-an",
		output,
	}
}

// libplaceboVaapiArgs returns ffmpeg arguments for software decode + libplacebo + VAAPI encode.
// VAAPI doesn't support the same CUDA→Vulkan interop, so uses software decode.
func (c *Converter) libplaceboVaapiArgs(input, output string) []string {
	vf := "libplacebo=apply_dolbyvision=true:colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084,format=nv12,hwupload"
	return []string{
		"-y",
		"-init_hw_device", "vulkan=vk",
		"-filter_hw_device", "vk",
		"-init_hw_device", fmt.Sprintf("vaapi=va:%s", c.transcode.VAAPIDevice),
		"-i", input,
		"-vf", vf,
		"-c:v", "hevc_vaapi",
		"-qp", fmt.Sprintf("%d", c.transcode.Quality),
		"-an",
		output,
	}
}

// libplaceboSoftwareArgs returns ffmpeg arguments for software decode + libplacebo + libx265.
func (c *Converter) libplaceboSoftwareArgs(input, output string) []string {
	preset := c.transcode.Preset
	vf := "libplacebo=apply_dolbyvision=true:colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084"
	return []string{
		"-y",
		"-init_hw_device", "vulkan=vk",
		"-filter_hw_device", "vk",
		"-i", input,
		"-vf", vf,
		"-c:v", "libx265",
		"-preset", preset,
		"-crf", fmt.Sprintf("%d", c.transcode.Quality),
		"-pix_fmt", "yuv420p10le",
		"-x265-params", "hdr-opt=1:repeat-headers=1:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc",
		"-an",
		output,
	}
}

// nvencPreset maps generic preset names to NVENC preset values.
func (c *Converter) nvencPreset() string {
	switch c.transcode.Preset {
	case "slow":
		return "p7"
	case "medium":
		return "p5"
	case "fast":
		return "p3"
	default:
		return "p7"
	}
}

