#!/usr/bin/env bash
# The one launcher for every dvremove container (D17, D19). The isolation flags sit in
# one array that no option reaches, so no option drops or weakens one.
#
# Usage: run.sh [-c config] [-t tag] [-i input] [-o output] [-s state] [-b src:dst]... [-- command [args]]
#   -c  config file, passed to dvremove on standard input (default: config.yaml beside this script)
#   -t  image tag (default dvremove:current)
#   -i  input folder, bound read-only at /data/toConvert
#   -o  output folder, bound at /data/Converted
#   -s  state folder, bound at /state
#   -b  extra bind, an absolute src:dst with no mode, always mounted read-only
#   --  a command run in place of dvremove, as the container's entrypoint
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

absolute -i "$input"; absolute -o "$output"; absolute -s "$state"
for b in "${extra[@]+"${extra[@]}"}"; do
  src=${b%%:*}; rest=${b#*:}
  [ "$rest" != "$b" ] && [ "${rest#*:}" = "$rest" ] || die "-b takes src:dst with no mode: $b"
  absolute -b "$src"; absolute -b "$rest"
done

safe=(
  --rm --name dvremove
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
for b in "${extra[@]+"${extra[@]}"}"; do safe+=(-v "$b:ro"); done

if [ $# -gt 0 ]; then
  exec docker run "${safe[@]}" --entrypoint "$1" "$tag" "${@:2}"
fi
[ -r "$config" ] || die "config file not readable: $config"
exec docker run -i "${safe[@]}" "$tag" -config /dev/stdin -once -no-ui <"$config"
