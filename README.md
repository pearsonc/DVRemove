# DVRemove

Converts Dolby Vision MKV files for HDR10 fallback compatibility.
- **Profile 7→8.1**: Lossless remux (metadata conversion only)
- **Profile 5→HDR10**: GPU transcode with libplacebo colour conversion

## Deployment on linux-lab-01

dvremove runs nightly in a locked-down container on linux-lab-01, not installed on the host.

- The image, `Dockerfile`, carries ffmpeg, mkvtoolnix, mediainfo, vainfo, Mesa's VA and Vulkan drivers and dovi_tool 2.3.4, whose checksum the build verifies. It runs as user 1000.
- `deploy/linux-lab-01/run.sh` starts every container. It drops all capabilities, runs read-only with no network, sets memory, CPU, process and write-rate limits, and binds only three folders: `/mnt/WD40MassStorage/dvremove/toConvert` read-only, `.../Converted`, and the state folder `/home/chperso/dvremove/state`. The configuration file goes in on standard input. Its options take only what the container may reach: `-t` an image reference, `-i`, `-o` and `-s` the live folders or a folder under `/home/chperso/dvremove-test/`, `-b` an existing regular file or folder under `/mnt/WD40MassStorage/` or `/home/chperso/dvremove-test/` bound read-only at a destination other than `/`, `/data` or `/state`, and `--` a program name not starting with `-`. A path holds no `:`, `,` or `..`. Anything else is refused with exit 2 before Docker runs.
- `dvremove.timer` starts `dvremove.service` at 01:30 each night. The service runs `-once -no-ui`, for at most 6 hours, and exits 1 when a file is left unconverted.
- Chris moves a title into `toConvert`; the next run converts it into `Converted`; he moves the result into Plex by hand.
- `deploy/linux-lab-01/config.yaml` is the live configuration and `deploy/linux-lab-01/test.yaml` the test one.

To operate it, read `docs/runbooks/index.md`: whether last night's run converted everything, whether the service is healthy, how to reconvert a title, and how to stop the runs and undo the deployment.

## Build

Requires Go 1.23+.

```bash
make build
```

## Makefile targets

| Target | What it does |
|---|---|
| `build` | Builds `dvremove` for this machine |
| `build-linux` | Builds a static linux/amd64 binary, `CGO_ENABLED=0` and `-trimpath` |
| `test` | Runs the Go tests with `-race`, then the shell tests of the launcher and the runbook runner |
| `runbooks` | Runs every step of every runbook in `docs/runbooks/` on linux-lab-01 over ssh; a step that changes state runs against a fixture it plants and removes |
| `deploy` | Builds the binary and copies it, `Dockerfile` and `deploy/linux-lab-01/` to the host's build folder |
| `image` | Builds `dvremove:<head>` on the host from a clean tree and tags it `dvremove:current` |
| `install-units` | Copies `dvremove.service` and `dvremove.timer` to the host's user unit folder |
| `clean` | Removes the binary |

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
temp_dir: "/state/tmp"   # Where temporary files go; unset means the operating system's default
state_dir: "/state"      # Holds ledger.txt, journal.txt and dvremove.lock

transcode:
  encoder: "auto"      # auto, nvenc, vaapi, software
  quality: 28          # CRF/CQ value (0-51)
  preset: "fast"       # slow, medium, fast

parallel:
  max_workers: 2       # Concurrent conversions (1-8)
```

- `temp_dir` keeps large temporary files off a small `/tmp`. In the container `/tmp` is a 64 MB tmpfs, so the deployed configuration sets it into the state folder.
- `state_dir` holds `ledger.txt`, one line per converted input, so a title already converted is skipped even after its output has moved. Delete a title's line to reconvert it.
- A key dvremove does not know fails the load.

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

On linux-lab-01 the log is `/home/chperso/dvremove/state/logs/dvremove.log`.

## Verify Output

```bash
mediainfo Converted/filename.mkv | grep -i "HDR format"
# Profile 7→8.1: Dolby Vision, dvhe.08.06, HDR10 compatible
# Profile 5→HDR10: SMPTE ST 2086, HDR10
```
