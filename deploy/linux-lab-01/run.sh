#!/usr/bin/env bash
# The one launcher for every dvremove container (D17, D19). The isolation flags sit in
# one array that no option reaches, so no option drops or weakens one.
# Every container runs as 1000:1000 with group 991 added, whatever image -t names.
#
# Usage: run.sh [-c config] [-t tag] [-i input] [-o output] [-s state] [-b src:dst]... [-- command [args]]
#   -c  config file, passed to dvremove on standard input (default: config.yaml beside this script)
#   -t  image tag (default dvremove:current), an image reference that never starts with -
#   -i  input folder, bound read-only at /data/toConvert
#   -o  output folder, bound at /data/Converted
#   -s  state folder, bound at /state
#       -i, -o and -s each take the live folder above or a folder under /home/chperso/dvremove-test/
#   -b  extra bind, an absolute src:dst with no mode, always mounted read-only. src is an existing
#       regular file or folder under /mnt/WD40MassStorage/ or /home/chperso/dvremove-test/, after
#       symlinks resolve; dst is not /, /data or /state. No path holds a : or a , or a .. component
#   --  a command run in place of dvremove, as the container's entrypoint; it does not start with -
set -eu

here=$(cd "$(dirname "$0")" && pwd)
config=$here/config.yaml
tag=dvremove:current
input=/mnt/WD40MassStorage/dvremove/toConvert
output=/mnt/WD40MassStorage/dvremove/Converted
state=/home/chperso/dvremove/state
extra=()

die() { echo "run.sh: $*" >&2; exit 2; }
absolute() { case $2 in /*) ;; *) die "$1 must be an absolute path: $2" ;; esac; }
# plain NAME PATH: absolute, with no character docker's -v parser reads and no .. component.
plain() {
  absolute "$1" "$2"
  case $2 in *[:,]*) die "$1 path holds a : or a ,: $2" ;; esac
  case "$2/" in */../*) die "$1 path holds a .. component: $2" ;; esac
}
# under PATH ROOT: PATH lies strictly below ROOT.
under() { case $1 in "$2"/?*) return 0 ;; *) return 1 ;; esac; }

media=$(realpath -m -- /mnt/WD40MassStorage)
testroot=$(realpath -m -- /home/chperso/dvremove-test)
live_input=$input; live_output=$output; live_state=$state

while [ $# -gt 0 ]; do
  case $1 in
    -c|-t|-i|-o|-s|-b) [ $# -ge 2 ] || die "$1 needs a value" ;;
  esac
  case $1 in
    -c) config=$2; shift 2 ;;
    -t) tag=$2; shift 2 ;;
    -i) input=$2; shift 2 ;;
    -o) output=$2; shift 2 ;;
    -s) state=$2; shift 2 ;;
    -b) extra+=("$2"); shift 2 ;;
    --) shift; break ;;
    *) die "unknown option: $1" ;;
  esac
done

[[ $tag =~ ^[A-Za-z0-9][A-Za-z0-9._/:@-]*$ ]] || die "-t is not an image reference: $tag"

# folder NAME VALUE LIVE: the live folder as written, or one under the test folder once symlinks resolve.
folder() {
  plain "$1" "$2"
  [ "$2" = "$3" ] && { resolved=$2; return; }
  resolved=$(realpath -m -- "$2")
  under "$resolved" "$testroot" || die "$1 must be $3 or lie under $testroot/: $2"
}
folder -i "$input" "$live_input"; input=$resolved
folder -o "$output" "$live_output"; output=$resolved
folder -s "$state" "$live_state"; state=$resolved

binds=()
for b in "${extra[@]+"${extra[@]}"}"; do
  src=${b%%:*}; rest=${b#*:}
  [ "$rest" != "$b" ] && [ "${rest#*:}" = "$rest" ] || die "-b takes src:dst with no mode: $b"
  plain -b "$src"; plain -b "$rest"
  real=$(realpath -e -- "$src" 2>/dev/null) || die "-b source does not exist: $src"
  under "$real" "$media" || under "$real" "$testroot" || die "-b source must lie under $media/ or $testroot/: $src"
  [ -f "$real" ] || [ -d "$real" ] || die "-b source is not a regular file or a folder: $src"
  dst=$(realpath -m -s -- "$rest")
  case $dst in /|/data|/state) die "-b destination covers $dst: $rest" ;; esac
  binds+=("$real:$dst")
done

safe=(
  --rm --name dvremove
  --user 1000:1000
  --group-add 991
  --network none
  --cap-drop ALL
  --security-opt no-new-privileges
  --read-only
  --tmpfs /tmp:size=64m
  --ulimit core=0
  --device /dev/dri/renderD128
  --memory 8g --memory-swap 8g
  --cpus 8
  --pids-limit 512
  --device-write-bps /dev/nvme0n1:200mb
  -e HOME=/state -e XDG_CACHE_HOME=/state/cache
  -v "$input:/data/toConvert:ro"
  -v "$output:/data/Converted"
  -v "$state:/state"
)
for b in "${binds[@]+"${binds[@]}"}"; do safe+=(-v "$b:ro"); done

if [ $# -gt 0 ]; then
  case $1 in ''|-*) die "the command must be a program name, not an option: $1" ;; esac
  exec docker run "${safe[@]}" --entrypoint "$1" "$tag" "${@:2}"
fi
[ -r "$config" ] || die "config file not readable: $config"
exec docker run -i "${safe[@]}" "$tag" -config /dev/stdin -once -no-ui <"$config"
