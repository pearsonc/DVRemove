package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func argsConverter() *Converter {
	return &Converter{
		transcode: TranscodeConfig{Quality: 28, Preset: "fast", VAAPIDevice: "/dev/dri/renderD128"},
		log:       zerolog.Nop(),
	}
}

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// Hazard: H4
func TestVaapiArgsAreMain10(t *testing.T) {
	args := argsConverter().libplaceboVaapiArgs("in.mkv", "out.mkv")

	vf := argAfter(args, "-vf")
	want := "libplacebo=apply_dolbyvision=true:colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084,format=p010,hwupload=derive_device=vaapi"
	if vf != want {
		t.Errorf("vaapi filter chain = %q, want %q", vf, want)
	}
	if strings.Contains(vf, "nv12") {
		t.Errorf("vaapi filter chain still converts to 8-bit nv12: %q", vf)
	}
	if got := argAfter(args, "-profile:v"); got != "main10" {
		t.Errorf("vaapi -profile:v = %q, want main10", got)
	}
}

// Hazard: H4
func TestNvencAndSoftwareArgsUnchanged(t *testing.T) {
	c := argsConverter()
	nv := c.libplaceboNvencArgs("in.mkv", "out.mkv")
	wantNv := []string{
		"-y", "-hwaccel", "cuda", "-hwaccel_output_format", "cuda",
		"-init_hw_device", "vulkan=vk", "-filter_hw_device", "vk",
		"-i", "in.mkv",
		"-vf", "hwdownload,format=p010,libplacebo=apply_dolbyvision=true:colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084",
		"-c:v", "hevc_nvenc", "-preset", "p3", "-profile:v", "main10",
		"-cq", "28", "-b:v", "0", "-rc", "vbr", "-an", "out.mkv",
	}
	if !reflect.DeepEqual(nv, wantNv) {
		t.Errorf("nvenc args changed:\n got %q\nwant %q", nv, wantNv)
	}
	sw := c.libplaceboSoftwareArgs("in.mkv", "out.mkv")
	wantSw := []string{
		"-y", "-init_hw_device", "vulkan=vk", "-filter_hw_device", "vk",
		"-i", "in.mkv",
		"-vf", "libplacebo=apply_dolbyvision=true:colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084",
		"-c:v", "libx265", "-preset", "fast", "-crf", "28", "-pix_fmt", "yuv420p10le",
		"-x265-params", "hdr-opt=1:repeat-headers=1:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc",
		"-an", "out.mkv",
	}
	if !reflect.DeepEqual(sw, wantSw) {
		t.Errorf("software args changed:\n got %q\nwant %q", sw, wantSw)
	}
}
