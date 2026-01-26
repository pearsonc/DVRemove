package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/rs/zerolog"
)

// GPUEncoder represents an available GPU encoder.
type GPUEncoder string

const (
	EncoderNVENC    GPUEncoder = "nvenc"
	EncoderVAAPI    GPUEncoder = "vaapi"
	EncoderSoftware GPUEncoder = "software"
)

// GPUDetector detects available GPU encoders on the system.
type GPUDetector struct {
	log         zerolog.Logger
	vaapiDevice string
}

// NewGPUDetector creates a new GPUDetector instance.
func NewGPUDetector(vaapiDevice string, log zerolog.Logger) *GPUDetector {
	return &GPUDetector{
		log:         log.With().Str("component", "gpu").Logger(),
		vaapiDevice: vaapiDevice,
	}
}

// DetectEncoder returns the best available encoder based on preference.
// Priority: NVENC > VAAPI > Software
func (g *GPUDetector) DetectEncoder(preferred string) GPUEncoder {
	switch preferred {
	case "nvenc":
		if g.hasNVENC() {
			return EncoderNVENC
		}
		g.log.Warn().Msg("NVENC requested but not available, falling back")
	case "vaapi":
		if g.hasVAAPI() {
			return EncoderVAAPI
		}
		g.log.Warn().Msg("VAAPI requested but not available, falling back")
	case "software":
		return EncoderSoftware
	}

	// Auto-detect: NVENC > VAAPI > Software
	if g.hasNVENC() {
		g.log.Info().Msg("detected NVENC encoder")
		return EncoderNVENC
	}
	if g.hasVAAPI() {
		g.log.Info().Msg("detected VAAPI encoder")
		return EncoderVAAPI
	}

	g.log.Info().Msg("no hardware encoder found, using software")
	return EncoderSoftware
}

// hasNVENC checks if NVIDIA NVENC is available.
func (g *GPUDetector) hasNVENC() bool {
	// Check nvidia-smi exists and works
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		g.log.Debug().Msg("nvidia-smi not found")
		return false
	}

	cmd := exec.Command("nvidia-smi", "-L")
	if err := cmd.Run(); err != nil {
		g.log.Debug().Err(err).Msg("nvidia-smi failed")
		return false
	}

	// Check ffmpeg has hevc_nvenc
	if !g.ffmpegHasEncoder("hevc_nvenc") {
		g.log.Debug().Msg("ffmpeg does not have hevc_nvenc")
		return false
	}

	return true
}

// hasVAAPI checks if VAAPI is available.
func (g *GPUDetector) hasVAAPI() bool {
	// Check if render device exists
	if _, err := os.Stat(g.vaapiDevice); err != nil {
		g.log.Debug().Str("device", g.vaapiDevice).Msg("VAAPI device not found")
		return false
	}

	// Check ffmpeg has hevc_vaapi
	if !g.ffmpegHasEncoder("hevc_vaapi") {
		g.log.Debug().Msg("ffmpeg does not have hevc_vaapi")
		return false
	}

	return true
}

// ffmpegHasEncoder checks if ffmpeg supports a specific encoder.
func (g *GPUDetector) ffmpegHasEncoder(encoder string) bool {
	cmd := exec.Command("ffmpeg", "-hide_banner", "-encoders")
	output, err := cmd.Output()
	if err != nil {
		g.log.Debug().Err(err).Msg("failed to list ffmpeg encoders")
		return false
	}

	return strings.Contains(string(output), encoder)
}

// HasLibplacebo checks if ffmpeg has libplacebo filter support.
func (g *GPUDetector) HasLibplacebo() bool {
	cmd := exec.Command("ffmpeg", "-hide_banner", "-filters")
	output, err := cmd.Output()
	if err != nil {
		g.log.Debug().Err(err).Msg("failed to list ffmpeg filters")
		return false
	}

	return strings.Contains(string(output), "libplacebo")
}
