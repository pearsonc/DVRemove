# How do I reconvert a title?

Owner: the operator of linux-lab-01. Environment: linux-lab-01. Given: `TITLE` holds the title's file name as it sits in `toConvert`, exported in your shell, and the original is still in `/mnt/WD40MassStorage/dvremove/toConvert`.

The ledger, `/home/chperso/dvremove/state/ledger.txt`, holds one line per converted input, and a title whose line is gone is converted again at the next run. Nothing here touches a file Chris owns. The steps read `STATE` for the state folder, which you leave unset; the runner sets it to a fixture.

### 1. Make sure no run is in progress

```bash
! systemctl --user is-active --quiet dvremove.service
```
Expect: exit 0.
If not: a run is in progress and holds the ledger. Wait for it, or stop it by `stop-and-undo.md`, step 1.

### 2. Find exactly one ledger line for the title

```bash mutating
STATE=${STATE:-/home/chperso/dvremove/state}; n=$(/usr/bin/grep -cF -- "${TITLE:?set TITLE to the file name}" "$STATE/ledger.txt"); echo "$n"; [ "$n" = 1 ]
```
Expect: prints `1`.
If not: `0` means the title was never converted or `TITLE` is spelt differently, so compare it with `cat` of the ledger. A number above 1 means `TITLE` is a part of several names: give more of the name.

### 3. Delete that line, keeping a copy

```bash mutating
STATE=${STATE:-/home/chperso/dvremove/state}; before=$(wc -l <"$STATE/ledger.txt")
cp -- "$STATE/ledger.txt" "$STATE/ledger.txt.bak" && { /usr/bin/grep -vF -- "$TITLE" "$STATE/ledger.txt.bak" || true; } >"$STATE/ledger.txt"
[ "$(/usr/bin/grep -cF -- "$TITLE" "$STATE/ledger.txt")" = 0 ] && [ "$(wc -l <"$STATE/ledger.txt")" = $((before - 1)) ]
```
Expect: exit 0: the title's line is gone and every other line is kept.
If not: restore with `cp "$STATE/ledger.txt.bak" "$STATE/ledger.txt"` and read the ledger by hand.

### 4. Start a run now, or wait for 01:30

```bash mutating
${SC:-systemctl --user} start --no-block dvremove.service
```
Expect: exit 0. The run converts the title and logs `conversion complete`, and it takes hours for a large title.
If not: read `journalctl --user -u dvremove.service -n 50`. To wait, skip this step: the timer starts the run at 01:30, and `last-night.md` reads the result.
