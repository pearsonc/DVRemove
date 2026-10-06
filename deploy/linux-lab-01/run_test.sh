#!/usr/bin/env bash
# Tests deploy/linux-lab-01/run.sh against a stub `docker` that prints its arguments.
# Decision: D17
set -u
here=$(cd "$(dirname "$0")" && pwd)
run="$here/run.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/in" "$work/out" "$work/state" "$work/extra"
cat >"$work/bin/docker" <<'STUB'
#!/bin/sh
printf '%s\n' "$@" >"$STUB_OUT"
STUB
chmod +x "$work/bin/docker"
export PATH="$work/bin:$PATH" STUB_OUT="$work/args"

pass=0
fail=0
ok()  { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $*"; }

# invoke LABEL ARGS...: runs run.sh, loads the stub's arguments into the array A.
invoke() {
  label=$1; shift
  rm -f "$STUB_OUT"
  "$run" "$@" >"$work/stdout" 2>"$work/stderr" </dev/null
  rc=$?
  A=()
  [ -f "$STUB_OUT" ] && mapfile -t A <"$STUB_OUT"
  return $rc
}

has_pair() { # FLAG VALUE: the argument FLAG is followed directly by VALUE
  local i
  for ((i = 0; i + 1 < ${#A[@]}; i++)); do
    [ "${A[i]}" = "$1" ] && [ "${A[i + 1]}" = "$2" ] && return 0
  done
  return 1
}
has_arg() { local a; for a in "${A[@]}"; do [ "$a" = "$1" ] && return 0; done; return 1; }

# volumes: every value following -v, one per line.
volumes() {
  local i
  for ((i = 0; i + 1 < ${#A[@]}; i++)); do [ "${A[i]}" = "-v" ] && echo "${A[i + 1]}"; done
}

check_flags() { # LABEL: every invariant flag is present
  local l=$1 f
  [ "${A[0]:-}" = run ] || bad "$l: first argument is not run"
  has_arg --read-only || bad "$l: --read-only missing"
  for f in "--group-add 991" "--network none" "--cap-drop ALL" \
           "--security-opt no-new-privileges" "--tmpfs /tmp:size=64m" \
           "--ulimit core=0" "--device /dev/dri/renderD128" "--memory 8g" \
           "--memory-swap 8g" "--cpus 8" "--pids-limit 512" \
           "--device-write-bps /dev/nvme0n1:200mb" "--name dvremove" \
           "-e HOME=/state" "-e XDG_CACHE_HOME=/state/cache"; do
    has_pair "${f% *}" "${f#* }" && ok || bad "$l: ${f} missing"
  done
  for f in --privileged --network=host --cap-add --pid=host --ipc=host --device=/dev/mem; do
    has_arg "$f" && bad "$l: forbidden $f present"
  done
  return 0
}

check_volumes() { # LABEL IN OUT STATE EXTRA_COUNT: exactly the three binds plus extras, extras read-only
  local l=$1 in=$2 out=$3 st=$4 n=$5 v count=0 seen_in=0 seen_out=0 seen_st=0
  while IFS= read -r v; do
    [ -n "$v" ] || continue
    count=$((count + 1))
    case "$v" in
      "$in:/data/toConvert:ro") seen_in=1 ;;
      "$out:/data/Converted")   seen_out=1 ;;
      "$st:/state")             seen_st=1 ;;
      *:ro) ;;
      *) bad "$l: bind not read-only: $v" ;;
    esac
  done < <(volumes)
  [ $seen_in = 1 ] || bad "$l: input bind missing or not :ro"
  [ $seen_out = 1 ] || bad "$l: output bind missing"
  [ $seen_st = 1 ] || bad "$l: state bind missing"
  [ "$count" -eq $((3 + n)) ] || bad "$l: $count binds, want $((3 + n))"
  for v in --mount --volume --volumes-from; do has_arg "$v" && bad "$l: forbidden $v"; done
  return 0
}

live_in=/mnt/WD40MassStorage/dvremove/toConvert
live_out=/mnt/WD40MassStorage/dvremove/Converted
cfg="$work/c.yaml"; echo 'input_dir: /data/toConvert' >"$cfg"

# 1. Defaults: live folders, default tag, the state folder passed explicitly so no live path is touched.
invoke defaults -c "$cfg" -s "$work/state" || bad "defaults: run.sh exited $?"
check_flags defaults; check_volumes defaults "$live_in" "$live_out" "$work/state" 0
has_arg dvremove:current || bad "defaults: image tag dvremove:current missing"
has_arg /dev/stdin || bad "defaults: config not passed"

# 2. The default state folder is the live one.
/usr/bin/grep -q '/home/chperso/dvremove/state' "$run" && ok || bad "default state folder absent from run.sh"

# 3. Each option.
invoke folders -c "$cfg" -i "$work/in" -o "$work/out" -s "$work/state" || bad "folders: exited $?"
check_flags folders; check_volumes folders "$work/in" "$work/out" "$work/state" 0

invoke tag -c "$cfg" -s "$work/state" -t dvremove:abc123 || bad "tag: exited $?"
check_flags tag; has_arg dvremove:abc123 && ok || bad "tag: dvremove:abc123 missing"

invoke bind -c "$cfg" -s "$work/state" -b "$work/extra:/data/media" -b "$work/in:/data/Plex" || bad "bind: exited $?"
check_flags bind; check_volumes bind "$live_in" "$live_out" "$work/state" 2
volumes | /usr/bin/grep -qx "$work/extra:/data/media:ro" && ok || bad "bind: extra bind not :ro"

invoke command -s "$work/state" -t img:t -- sh -c 'echo hi' || bad "command: exited $?"
check_flags command; check_volumes command "$live_in" "$live_out" "$work/state" 0
has_pair --entrypoint sh && ok || bad "command: --entrypoint sh missing"
n=${#A[@]}
[ "$n" -ge 3 ] && [ "${A[n - 3]}" = img:t ] && [ "${A[n - 2]}" = -c ] && [ "${A[n - 1]}" = "echo hi" ] && ok || bad "command: image or arguments misplaced"

invoke commandbind -s "$work/state" -b "$work/extra:/mnt/x" -- true || bad "commandbind: exited $?"
check_flags commandbind; check_volumes commandbind "$live_in" "$live_out" "$work/state" 1

# 4. A first run on an empty state folder starts; so does one on a folder not yet created.
rm -rf "$work/fresh"; mkdir "$work/fresh"
invoke empty-state -c "$cfg" -s "$work/fresh" || bad "empty state: exited $?"
check_flags empty-state

# 5. Nothing weakens a flag: these must be refused, with docker never called.
refuse() {
  local label=$1; shift
  local rc
  invoke "$label" "$@"; rc=$?
  case $rc in 0) bad "$label: accepted" ;; 2) ok ;; *) bad "$label: exited $rc, want 2" ;; esac
  [ ${#A[@]} -eq 0 ] && ok || bad "$label: docker was called"
}
refuse rw-bind -c "$cfg" -s "$work/state" -b "$work/extra:/x:rw"
refuse mode-bind -c "$cfg" -s "$work/state" -b "$work/extra:/x:z"
refuse relative-bind -c "$cfg" -s "$work/state" -b "extra:/x"
refuse network -c "$cfg" -s "$work/state" --network host
refuse privileged -c "$cfg" -s "$work/state" --privileged
refuse unknown -c "$cfg" -s "$work/state" -z
refuse missing-config -c "$work/absent.yaml" -s "$work/state"
refuse relative-state -c "$cfg" -s rel/state

echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
