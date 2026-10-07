#!/usr/bin/env bash
# Tests deploy/linux-lab-01/run.sh against a stub `docker` that prints its arguments.
# Decision: D17
set -u
here=$(cd "$(dirname "$0")" && pwd)
run="$here/run.sh"
had_fixed=0; [ -e /home/chperso/dvremove-test ] && had_fixed=1
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# W11: nothing outside the worktree and the system temporary folder is created. The test folder run.sh
# accepts is the one literal on its testroot line; the copy under test swaps that literal for $work, and
# the check below proves no other line differs.
root=$work
sed "/^testroot=/s|/home/chperso/dvremove-test|$work|" "$run" >"$work/run.sh"
chmod +x "$work/run.sh"
[ "$(diff "$run" "$work/run.sh" | /usr/bin/grep -c '^>')" = 1 ] || { echo "FAIL: copy of run.sh differs on other than the testroot line"; exit 1; }
run="$work/run.sh"
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
  has_pair --user 1000:1000 && ok || bad "$l: --user 1000:1000 missing"
  local u=0 a; for a in "${A[@]}"; do case $a in --user|--user=*|-u|-u*) u=$((u + 1)) ;; esac; done
  [ "$u" -le 1 ] || bad "$l: --user given $u times"
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

live_in=/home/chperso/dvremove/toConvert
live_out=/home/chperso/dvremove/Converted
cfg="$work/c.yaml"; echo 'input_dir: /data/toConvert' >"$cfg"

# 1. Defaults: live folders, default tag, the state folder passed explicitly so no live path is touched.
invoke defaults -c "$cfg" -s "$work/state" || bad "defaults: run.sh exited $?"
check_flags defaults; check_volumes defaults "$live_in" "$live_out" "$work/state" 0
has_arg dvremove:current || bad "defaults: image tag dvremove:current missing"
has_arg /dev/stdin || bad "defaults: config not passed"

# 2. The three default folders are the local live ones, and no NAS path is a default.
for d in toConvert Converted state; do
  /usr/bin/grep -q "^[a-z]*=/home/chperso/dvremove/$d\$" "$run" && ok || bad "default $d folder absent from run.sh"
done
/usr/bin/grep -q "^[a-z]*=/mnt/WD40MassStorage" "$run" && bad "a NAS path is a default folder in run.sh" || ok

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

# 6. U2's attacks and the rest of their class (D17, H14): no value reaches docker as an option or
# widens what a container reaches. Each is refused with exit 2 and docker never called.
mkdir -p "$work/state2"
fifo="$work/fifo"; mkfifo "$fifo"
sock="$work/sock"; python3 -I -c 'import socket,sys; s=socket.socket(socket.AF_UNIX); s.bind(sys.argv[1])' "$sock"
ln -s / "$work/link-root"
ln -s "$work/extra" "$work/link-extra"
echo x >"$work/file"
st=(-c "$cfg" -s "$work/state")
refuse tag-network "${st[@]}" -t --network=host -- /usr/local/bin/dvremove dvremove:current -once -no-ui
refuse tag-readonly "${st[@]}" -t --read-only=false -- /usr/local/bin/dvremove dvremove:current -once
refuse tag-volume "${st[@]}" -t --volume=/:/host -- /bin/sh dvremove:current -c id
refuse tag-capadd "${st[@]}" -t --cap-add=ALL -- /bin/sh dvremove:current -c id
refuse tag-default "${st[@]}" -t --privileged
refuse tag-empty "${st[@]}" -t ""
refuse tag-space "${st[@]}" -t "dvremove:x --privileged"
refuse output-root "${st[@]}" -o /
refuse output-host-path "${st[@]}" -o /etc
refuse output-dotdot "${st[@]}" -o "$work/../../../etc"
refuse output-symlink "${st[@]}" -o "$work/link-root"
refuse input-root "${st[@]}" -i /
refuse input-home "${st[@]}" -i /home/chperso
refuse input-comma "${st[@]}" -i "$work/in,x"
refuse input-colon "${st[@]}" -i "$work/in:x"
refuse input-option "${st[@]}" -i --privileged
nas=/mnt/WD40MassStorage
refuse input-nas "${st[@]}" -i "$nas/dvremove/toConvert"
refuse output-nas "${st[@]}" -o "$nas/dvremove/Converted"
refuse state-nas -c "$cfg" -s "$nas/dvremove/state"
refuse state-root -c "$cfg" -s /
refuse state-host-path -c "$cfg" -s /var/lib
refuse state-colon -c "$cfg" -s "$work/st:x"
refuse state-comma -c "$cfg" -s "$work/st,x"
refuse bind-sock "${st[@]}" -b /var/run/docker.sock:/run/docker.sock
refuse bind-root "${st[@]}" -b /:/host
refuse bind-etc "${st[@]}" -b /etc:/x
refuse bind-test-root "${st[@]}" -b "$root:/x"
refuse bind-dotdot "${st[@]}" -b "$work/extra/../../..:/x"
refuse bind-symlink-out "${st[@]}" -b "$work/link-root:/x"
refuse bind-symlink-in-to-out "${st[@]}" -b "$work/link-root/etc:/x"
refuse bind-socket "${st[@]}" -b "$sock:/x"
refuse bind-fifo "${st[@]}" -b "$fifo:/x"
refuse bind-missing "${st[@]}" -b "$work/absent:/x"
refuse bind-comma-src "${st[@]}" -b "$work/extra,x:/x"
refuse bind-comma-dst "${st[@]}" -b "$work/extra:/x,y"
refuse bind-dst-data "${st[@]}" -b "$work/extra:/data"
refuse bind-dst-data-slash "${st[@]}" -b "$work/extra:/data/"
refuse bind-dst-data-dots "${st[@]}" -b "$work/extra:/x/../data"
refuse bind-dst-state "${st[@]}" -b "$work/extra:/state"
refuse bind-dst-root "${st[@]}" -b "$work/extra:/"
refuse bind-dst-relative-dots "${st[@]}" -b "$work/extra:/.."
refuse bind-empty "${st[@]}" -b ""
refuse command-option "${st[@]}" -- --privileged
refuse command-empty "${st[@]}" -- ""
refuse command-dash-with-args "${st[@]}" -- -v /x

# W10, D17: another image's default user never runs, so the user is passed whatever the tag.
invoke user-foreign-image "${st[@]}" -t busybox:latest -- sh || bad "user-foreign-image: exited $?"
has_pair --user 1000:1000 && ok || bad "user-foreign-image: docker did not receive --user 1000:1000"
has_pair --group-add 991 && ok || bad "user-foreign-image: --group-add 991 missing"
invoke user-foreign-default "${st[@]}" -t busybox:latest || bad "user-foreign-default: exited $?"
has_pair --user 1000:1000 && ok || bad "user-foreign-default: docker did not receive --user 1000:1000"

# W11: the test touched nothing at the fixed test folder.
[ ! -e /home/chperso/dvremove-test ] || [ "$had_fixed" = 1 ] && ok || bad "run_test.sh created /home/chperso/dvremove-test"

# Accepted: a regular file as a bind source, and a source under the media disk.
invoke bind-file "${st[@]}" -b "$work/file:/x" || bad "bind-file: exited $?"
check_flags bind-file; check_volumes bind-file "$live_in" "$live_out" "$work/state" 1
invoke bind-symlink-in "${st[@]}" -b "$work/link-extra:/x" || bad "bind-symlink-in: exited $?"
check_flags bind-symlink-in

echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
