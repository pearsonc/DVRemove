# DVRemove

Converts Dolby Vision Profile 5/7 MKV files to Profile 8.1 for HDR10 fallback compatibility. Uses remux-only approach (no re-encoding) for lossless, fast conversion.

## Dependencies

### System Dependencies

```bash
# Ubuntu/Debian
sudo apt install mkvtoolnix mediainfo ffmpeg

# Arch
sudo pacman -S mkvtoolnix-cli mediainfo ffmpeg
```

### dovi_tool

Download latest release from GitHub:

```bash
cd /tmp
curl -sL https://api.github.com/repos/quietvoid/dovi_tool/releases/latest \
  | grep -o 'https://[^"]*x86_64-unknown-linux-musl.tar.gz' \
  | xargs wget -q
tar -xzf dovi_tool-*-x86_64-unknown-linux-musl.tar.gz
sudo mv dovi_tool /usr/local/bin/
rm dovi_tool-*.tar.gz
```

### Go (for building from source)

Go 1.23+ required. Install from https://go.dev/dl/ or:

```bash
# Ubuntu/Debian
sudo apt install golang-go

# Or use official installer
wget https://go.dev/dl/go1.23.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.5.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
```

## Build

```bash
go build -o dvremove .
```

## Usage

### Watch Mode (default)

Monitors `toConvert/` directory and automatically converts new MKV files:

```bash
./dvremove
```

### One-shot Mode

Processes existing files in `toConvert/` and exits:

```bash
./dvremove -once
```

### Custom Config

```bash
./dvremove -config /path/to/config.yaml
```

## Configuration

Edit `config.yaml`:

```yaml
# Directory to watch for new MKV files
input_dir: "./toConvert"

# Directory for converted output files
output_dir: "./Converted"

# Directory for log files
log_dir: "./logs"
```

## How It Works

1. **Detect** - Analyses MKV with mediainfo to identify DV profile (5, 7, or 8)
2. **Extract** - Uses ffmpeg to extract HEVC stream in Annex B format
3. **Convert** - Pipes to dovi_tool to convert DV metadata to Profile 8.1
4. **Remux** - Uses mkvmerge to combine converted video with original audio/subtitles

### Supported Profiles

| Input Profile | Action | HDR10 Fallback |
|---------------|--------|----------------|
| Profile 7 | Converts to 8.1 | ✅ Full support |
| Profile 5 | **Skipped** | ❌ IPT colour space |
| Profile 8 | Skipped | Already compatible |

### Profile 5 Files

Profile 5 files are **skipped with an error** because they use proprietary IPT colour space (not BT.2020). HDR10 fallback is impossible without lossy transcoding.

If you have Profile 5 content, find a Profile 7 or HDR10 source instead.

## Logs

Logs are written to `logs/dvremove.log`:

```bash
# Follow logs
tail -f logs/dvremove.log

# Check for errors
grep "error" logs/dvremove.log
```

## Verify Output

Check converted file has HDR10 fallback:

```bash
mediainfo Converted/filename.mkv | grep -i "HDR format"
# Expected: Dolby Vision, Version 1.0, dvhe.08.06, BL+RPU, HDR10 compatible
```
