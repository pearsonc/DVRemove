package main

import "fmt"

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
// Software decode is faster than VAAPI hwaccel for this pipeline due to hwdownload overhead.
// Uses derive_device to properly chain Vulkan (libplacebo) to VAAPI (encode).
func (c *Converter) libplaceboVaapiArgs(input, output string) []string {
	vf := "libplacebo=apply_dolbyvision=true:colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084,format=nv12,hwupload=derive_device=vaapi"
	return []string{
		"-y",
		"-init_hw_device", fmt.Sprintf("vaapi=va:%s", c.transcode.VAAPIDevice),
		"-init_hw_device", "vulkan=vk@va",
		"-filter_hw_device", "vk",
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
