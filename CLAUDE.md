# CLAUDE.md

> **Full Context:** See `Claude/Context/project-dvremove.md` in Obsidian vault.

## Overview

Converts Dolby Vision files for HDR10 fallback. Profile 7→8.1 remux (lossless), Profile 5→HDR10 transcode (NVDEC + libplacebo + NVENC). Written in Go.

## Commands

```bash
go build && ./dvremove       # Build and run locally (Thor)
make deploy                  # Deploy to linux-lab-01
tail -f logs/dvremove.log    # Check logs
```

## Key Files

- `converter.go` - Profile detection + libplacebo transcode pipelines
- `gpu.go` - GPU encoder + libplacebo detection
- `config.yaml` - Paths and transcode settings (CQ 28)
- `Makefile` - Build and deploy to linux-lab-01

## System Dependencies

```bash
sudo apt install mkvtoolnix mediainfo
# ffmpeg with libplacebo + vulkan + NVENC (custom build on Thor)
# dovi_tool from GitHub releases
```
