# Did last night's run convert everything, and if not, why?

Owner: the operator of linux-lab-01. Environment: linux-lab-01. Given: the nightly unit `dvremove.service` has run at least once, and you read it as `chperso` on the host.

The run's log is `/home/chperso/dvremove/state/logs/dvremove.log`; the unit is a user unit, so every `systemctl` below carries `--user`.

### 1. Read the run's exit status

```bash
st=$(systemctl --user show dvremove.service -p ExecMainStatus --value); echo "ExecMainStatus=$st"; [ "$st" = 0 ]
```
Expect: `ExecMainStatus=0`, which means every file was converted, already converted, not a Profile 5 or 7 file, or still growing.
If not: read the number. 1 is a file left unconverted for another reason, so go on to step 3. 124 is the 6-hour bound ending the run, so go on to step 2 and the log's last lines. 137 is a kill, almost always memory, so go on to step 4. 2 is `run.sh` refusing its arguments or its config. 125 to 127 is Docker failing to start the container, so read `journalctl --user -u dvremove.service -n 50`.

### 2. Read when the run started and ended

```bash
f() { systemctl --user show dvremove.service -p "$1" --value; }
echo "start: $(f ExecMainStartTimestamp)"; echo "end: $(f ExecMainExitTimestamp)"
[ -n "$(f ExecMainStartTimestamp)" ] && [ -n "$(f ExecMainExitTimestamp)" ]
```
Expect: two non-empty times, the start near 01:30 and the end before 07:00.
If not: an empty end means the run is still going, so wait and ask again. An end after 07:00 is the R1 trigger: go on to `service-health.md`, step 3. No start at all means the unit has never run: check the timer in `service-health.md`, step 1.

### 3. Look for errors in the log

```bash
log=/home/chperso/dvremove/state/logs/dvremove.log
test -r "$log" && ! tail -n 200 "$log" | /usr/bin/grep -iE '"level": ?"(error|fatal)"|\b(ERR|FTL)\b'
```
Expect: exit 0 and no line printed.
If not: the printed lines name the file and the tool that failed. Fix the cause, then reconvert the title by `reconvert-title.md`. If the log is unreadable, the state folder is not mounted where the unit expects it: `ls -ld /home/chperso/dvremove/state`.

### 4. Look for the memory guard

```bash
log=/home/chperso/dvremove/state/logs/dvremove.log
test -r "$log" && ! tail -n 2000 "$log" | /usr/bin/grep -F 'memory guard'
```
Expect: exit 0 and no line printed.
If not: the guard stopped a conversion because host memory fell below 4 GiB available or swap grew more than 1 GiB. That is the R4 trigger. Stop the timer by `stop-and-undo.md`, step 1, and lower the limits in `run.sh` before it runs again.

### 5. Check the disk and the staging folders

```bash
test -d /home/chperso/dvremove/toConvert && test -w /home/chperso/dvremove/Converted && [ "$(df --output=avail -B1G / | tail -1 | tr -d ' ')" -ge 150 ]
```
Expect: exit 0: both staging folders exist, `Converted` is writable, and the root disk has at least 150 GB free.
If not: a missing folder is the R2 trigger: recreate it with `mkdir -p` under `/home/chperso/dvremove`, and rerun the unit by `systemctl --user start dvremove.service`. Under 150 GB free is the R3 trigger: find what filled the disk with `du -xh --max-depth=2 /home/chperso | sort -h | tail`.

### 6. List what is still waiting

```bash
in=/home/chperso/dvremove/toConvert; led=/home/chperso/dvremove/state/ledger.txt
test -r "$led" && find "$in" -type f -name '*.mkv' -printf '%f\n' | while IFS= read -r n; do /usr/bin/grep -qF -- "$n" "$led" || echo "waiting: $n"; done; true
```
Expect: exit 0, printing one `waiting:` line per file the ledger does not hold, and none if everything is converted.
If not: an unreadable ledger means no conversion has ever completed on this host, so read step 3. A waiting file is one still growing, not a Profile 5 or 7 file, refused as larger than 3840x2160, or failed: the log names it.
