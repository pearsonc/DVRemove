# Is the nightly service healthy?

Owner: the operator of linux-lab-01. Environment: linux-lab-01. Given: `make install-units` has installed `dvremove.service` and `dvremove.timer`, and the image `dvremove:current` is built.

One step per trigger in the deployment plan's Risks table, R1 to R6. R7 and R8 fire at build time and in wave 4, so no step here reads them.

### 1. The timer is enabled and shows 01:30

```bash
systemctl --user is-enabled dvremove.timer && systemctl --user is-active dvremove.timer && systemctl --user list-timers dvremove.timer --no-pager | /usr/bin/grep -F 01:30
```
Expect: `enabled`, `active`, and a timer line whose next time is 01:30.
If not: `systemctl --user enable --now dvremove.timer`. If the unit is unknown, run `make install-units` from the repository, then `systemctl --user daemon-reload`.

### 2. R1: the last run ended before 07:00

```bash
sv() { systemctl --user show dvremove.service -p "$1" --value; }
s=$(sv ExecMainStartTimestampMonotonic); e=$(sv ExecMainExitTimestampMonotonic)
if [ -z "$s" ] || [ "$s" = 0 ] || [ -z "$e" ] || [ "$e" = 0 ]; then echo "no finished run to read"; exit 0; fi
now=$(date +%s); up=$(cut -d. -f1 /proc/uptime)
se=$((now - up + s / 1000000)); ee=$((now - up + e / 1000000))
cut=$(date -d "$(date -d "@$se" +%F) 07:00" +%s)
echo "started $(date -d "@$se" '+%F %T'), ended $(date -d "@$ee" '+%F %T')"
[ "$se" -ge "$cut" ] || [ "$ee" -le "$cut" ]
```
Expect: exit 0. A run that began after 07:00 was started by hand, and counts as no nightly run. A nightly run that began before 07:00 ended by 07:00.
If not: the run overlapped household viewing, the R1 trigger. Ask Chris about buffering; move the timer by editing `OnCalendar` in `dvremove.timer`, running `make install-units` and `systemctl --user daemon-reload`, or stop it by `stop-and-undo.md`.

### 3. R2: the last run exited 0 and the staging folders exist

```bash
[ "$(systemctl --user show dvremove.service -p ExecMainStatus --value)" = 0 ] && test -d /home/chperso/dvremove/toConvert && test -w /home/chperso/dvremove/Converted
```
Expect: exit 0.
If not: a non-zero status is the R2 trigger when a staging folder was missing at 01:30: recreate it, then `systemctl --user start dvremove.service` to rerun. Any other non-zero status goes to `last-night.md`.

### 4. R3: the root disk has 150 GB free

```bash
free=$(df --output=avail -B1G / | tail -1 | tr -d ' '); echo "free: ${free}G"; [ "$free" -ge 150 ]
```
Expect: `free:` at least 150G. The run's own log line for the free space at its start reads the same disk.
If not: the VM images share this disk. Clear the paths in `/home/chperso/dvremove/state/journal.txt` that are left over, then find the leak with `du -xh --max-depth=2 /home/chperso | sort -h | tail`.

### 5. R4: no kill, no memory guard, memory.peak at most 7 GB

```bash
log=/home/chperso/dvremove/state/logs/dvremove.log
[ "$(systemctl --user show dvremove.service -p ExecMainStatus --value)" != 137 ] || exit 1
[ ! -e "$log" ] && { echo "no log yet"; exit 0; }
! tail -n 2000 "$log" | /usr/bin/grep -F 'memory guard' || exit 1
peak=$(/usr/bin/grep -ioE 'memory[._ -]?peak"?[=: ]+"?[0-9]+' "$log" | tail -1 | /usr/bin/grep -oE '[0-9]+$')
echo "memory.peak: ${peak:-not logged}"
[ -z "$peak" ] || [ "$peak" -le 7516192768 ]
```
Expect: exit 0, printing `memory.peak:` and either a figure at most 7516192768 bytes or `not logged`.
If not: exit status 137, a `memory guard` line or a peak above 7 GB is the R4 trigger. Stop the timer by `stop-and-undo.md`, step 1, then lower `--memory` in `run.sh`. Check the VMs with `virsh list`.

### 6. R5: the one-frame check through the launcher still passes

```bash
if systemctl --user is-active --quiet dvremove.service; then echo "a run is in progress; skipped"; exit 0; fi
timeout 120 bash /home/chperso/dvremove/build/deploy/linux-lab-01/run.sh -- ffmpeg -v error -y -init_hw_device vaapi=va:/dev/dri/renderD128 -init_hw_device vulkan=vk@va -filter_hw_device vk -f lavfi -i 'testsrc=size=1920x1080:rate=1:duration=1,format=yuv420p10le' -frames:v 1 -vf 'libplacebo=apply_dolbyvision=true:colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084,format=p010,hwupload=derive_device=vaapi' -c:v hevc_vaapi -profile:v main10 -f null -
```
Expect: exit 0 and no output, the Vulkan to VAAPI chain encoding one Main 10 frame; the step is skipped while the nightly unit is running, because the launcher names its container `dvremove`.
If not: the chain fails on the 780M, the R5 trigger. Read the printed error, check `vainfo --display drm --device /dev/dri/renderD128` on the host, and stop the timer by `stop-and-undo.md` until the plan is amended.

### 7. R6: the log is at most 100 MB

```bash
log=/home/chperso/dvremove/state/logs/dvremove.log
[ ! -e "$log" ] || [ "$(stat -c %s "$log")" -le 104857600 ]
```
Expect: exit 0.
If not: the log passed 100 MB, the R6 trigger. Add rotation as a work item; until then `mv` the file aside when no run is active.

### 8. No container is left over

```bash
systemctl --user is-active --quiet dvremove.service || [ -z "$(docker ps -aq --filter name=^dvremove$)" ]
```
Expect: exit 0: either a run is in progress, or no container named `dvremove` exists.
If not: `ExecStopPost` did not remove it. Run `docker rm -f dvremove`, then read `journalctl --user -u dvremove.service -n 50` for why.
