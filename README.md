# DVRemove

Converts Dolby Vision MKV files for HDR10 fallback compatibility.
- **Profile 7→8.1**: Lossless remux (metadata conversion only)
- **Profile 5→HDR10**: GPU transcode with libplacebo colour conversion

## Dependencies

### System

```bash
sudo apt install mkvtoolnix mediainfo
```

### ffmpeg with libplacebo

Requires ffmpeg built with libplacebo and Vulkan support. For NVIDIA:
```bash
# Custom build required for libplacebo + NVENC
# See: https://trac.ffmpeg.org/wiki/CompilationGuide/Ubuntu
```

### dovi_tool

```bash
cd /tmp && curl -sL https://api.github.com/repos/quietvoid/dovi_tool/releases/latest | grep -o 'https://[^"]*x86_64-unknown-linux-musl.tar.gz' | xargs wget -q && tar -xzf dovi_tool-*-x86_64-unknown-linux-musl.tar.gz && sudo mv dovi_tool /usr/local/bin/ && rm dovi_tool-*.tar.gz
```

### Go 1.23+

```bash
sudo apt install golang-go
```

## Build

```bash
go build
```

## Usage

```bash
./dvremove              # Watch mode with TUI
./dvremove -once        # Process existing files and exit
./dvremove -once -no-ui # Headless mode (for scripts/cron)
./dvremove -config /path/to/config.yaml
```

## Configuration

Edit `config.yaml`:

```yaml
input_dir: "./toConvert"
output_dir: "./Converted"
log_dir: "./logs"

transcode:
  encoder: "auto"      # auto, nvenc, vaapi, software
  quality: 28          # CRF/CQ value (0-51)
  preset: "fast"       # slow, medium, fast

parallel:
  max_workers: 2       # Concurrent conversions (1-8)
```

## Supported Profiles

| Input | Method | Output |
|-------|--------|--------|
| Profile 7 | Remux (lossless) | Profile 8.1 with HDR10 |
| Profile 5 | Transcode (libplacebo) | HDR10 |
| Profile 8 | Skipped | Already compatible |

## Pipelines

**Profile 7** (lossless): ffmpeg extract → dovi_tool convert → mkvmerge remux

**Profile 5** (transcode):
- NVENC: NVDEC → libplacebo → NVENC (~3x speed)
- VAAPI: Software → libplacebo → VAAPI (~2.8x speed)

## Logs

```bash
tail -f logs/dvremove.log
```

## Verify Output

```bash
mediainfo Converted/filename.mkv | grep -i "HDR format"
# Profile 7→8.1: Dolby Vision, dvhe.08.06, HDR10 compatible
# Profile 5→HDR10: SMPTE ST 2086, HDR10
```
