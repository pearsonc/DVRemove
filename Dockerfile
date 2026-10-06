# dvremove runtime image for linux-lab-01 (D17, D20). Build context: the folder `make deploy`
# fills, holding the statically built `dvremove` binary and this file.
# Base: Ubuntu 26.04, pinned by digest, read 2026-10-06.

FROM ubuntu@sha256:f144425ff09be612d6d9ad965196e9cdc23dae1f42110a8a11a3e9a8198759f7 AS dovi-tool

# dovi_tool 2.3.4, MIT, verified against the digest GitHub publishes for the release asset.
ADD --checksum=sha256:1844258e13c26607b32224bf1fa82b595d3b35949f5467405fda560daad32b3f \
    https://github.com/quietvoid/dovi_tool/releases/download/2.3.4/dovi_tool-2.3.4-x86_64-unknown-linux-musl.tar.gz \
    /tmp/dovi_tool.tar.gz
RUN tar -xzf /tmp/dovi_tool.tar.gz -C /tmp ./dovi_tool

FROM ubuntu@sha256:f144425ff09be612d6d9ad965196e9cdc23dae1f42110a8a11a3e9a8198759f7

# Direct packages pinned by version, read from the base with apt-cache policy on 2026-10-06.
# mesa-libgallium carries radeonsi_drv_video.so (VA-API); mesa-vulkan-drivers carries RADV.
# A build failing on a superseded version starts an upgrade pass in its own commit.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ffmpeg=7:8.0.1-3ubuntu2 \
      mkvtoolnix=97.0-1build1 \
      mediainfo=26.01-1 \
      vainfo=2.22.0+ds1-2build1 \
      mesa-libgallium=26.0.8-1ubuntu0.3 \
      mesa-vulkan-drivers=26.0.8-1ubuntu0.3 \
 && rm -rf /var/lib/apt/lists/*

COPY --from=dovi-tool /tmp/dovi_tool /usr/local/bin/dovi_tool
COPY dvremove /usr/local/bin/dvremove

USER 1000:1000

# Every run is bounded at 6 hours inside the container (D19); -k ends a process that ignores TERM.
ENTRYPOINT ["timeout", "-k", "60s", "6h", "dvremove"]
