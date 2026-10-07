# How do I stop the nightly runs and undo the deployment?

Owner: the operator of linux-lab-01. Environment: linux-lab-01. Given: the units, the image and the build folder from `make install-units`, `make deploy` and `make image` are in place.

This follows the rollback of waves 2 and 5. It leaves `/home/chperso/dvremove/state` and `toConvert` and `Converted` beside it in place: a title Chris put in `toConvert` or `Converted` is his, and nothing here removes it. The steps read `UNITS`, `DEPLOY`, `IMAGES`, `SC` and `DOCKER`, which you leave unset; the runner points them at a fixture.

### 1. Stop the timer and any run in progress

```bash mutating
${SC:-systemctl --user} disable --now dvremove.timer && ${SC:-systemctl --user} stop dvremove.service && { ${DOCKER:-docker} rm -f dvremove >/dev/null 2>&1 || true; }
```
Expect: exit 0. A run that was converting ends at once; its half-written files are in the journal, and the next start removes them.
If not: read the error. An unknown unit means it is already gone, so go on to step 3.

### 2. Remove the unit files and reload

```bash mutating
UNITS=${UNITS:-$HOME/.config/systemd/user}
rm -f -- "$UNITS/dvremove.service" "$UNITS/dvremove.timer" && ${SC:-systemctl --user} daemon-reload && test ! -e "$UNITS/dvremove.service" && test ! -e "$UNITS/dvremove.timer"
```
Expect: exit 0, with neither unit file left.
If not: check you own `~/.config/systemd/user` with `ls -l`.

### 3. Remove the images

```bash mutating
IMAGES=${IMAGES-$(docker image ls --format '{{.Repository}}:{{.Tag}}' dvremove)}
[ -z "$IMAGES" ] || ${DOCKER:-docker} image rm $IMAGES
```
Expect: exit 0, each image `dvremove:current` and `dvremove:<head>` untagged or deleted.
If not: an image in use means a container still runs from it: `docker ps -a`, then `docker rm -f` that container and repeat.

### 4. Remove the build folder

```bash mutating
DEPLOY=${DEPLOY:-/home/chperso/dvremove}
rm -r -- "$DEPLOY/build" && test ! -e "$DEPLOY/build"
```
Expect: exit 0.
If not: the folder is already gone, so `rm -r` fails: nothing remains to undo.

### 5. Check what stays

```bash mutating
STATE=${STATE:-/home/chperso/dvremove/state}
test -d "$STATE" && test -s "$STATE/ledger.txt"
```
Expect: exit 0: the state folder and its ledger are in place, so a later deployment skips what was converted.
If not: the state folder is gone. A later deployment reconverts every title in `toConvert`; restore it from a backup if the ledger matters.

To remove the state folder too, run `rm -r /home/chperso/dvremove/state` by hand. To remove the local staging folders, run `rmdir` on `toConvert`, `Converted` and then `dvremove` under `/home/chperso`, each only while empty.
