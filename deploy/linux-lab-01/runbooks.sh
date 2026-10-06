#!/usr/bin/env bash
# Runs every step of every runbook in docs/runbooks/ on linux-lab-01 over ssh (D24).
#
# Usage: runbooks.sh [-d runbook-folder]
#   Host and port come from RB_HOST and RB_PORT (192.168.1.3 and 9999). RB_ROOT is the folder the
#   fixture is planted under (/home/chperso/dvremove-test).
#
# A step is a fenced block tagged `bash` or `bash mutating`, followed by its Expect line. The
# block passes when it exits 0, so a block ends in the test its Expect line describes. A
# `bash` block runs against the live host and changes nothing. A `bash mutating` block runs
# against a fixture planted under RB_ROOT/runbook-fixture, which is removed after its runbook,
# with STATE, UNITS, DEPLOY, TITLE, IMAGES, SC and DOCKER pointing into it.
# Exits 1 when a step fails, a runbook is missing from index.md, an index entry has no file, or a
# runbook's blocks and Expect lines differ in number.
set -u

host=${RB_HOST:-192.168.1.3}
port=${RB_PORT:-9999}
root=${RB_ROOT:-/home/chperso/dvremove-test}
dir=$(cd "$(dirname "$0")/../.." && pwd)/docs/runbooks

while [ $# -gt 0 ]; do
  case $1 in
    -d) [ $# -ge 2 ] || { echo "runbooks.sh: -d needs a value" >&2; exit 2; }; dir=$2; shift 2 ;;
    *) echo "runbooks.sh: unknown option: $1" >&2; exit 2 ;;
  esac
done

fix=$root/runbook-fixture
tmp=$(mktemp -d)
failures=0
planted=0

# The script travels base64-encoded, so no remote shell parses its quoting.
remote() { ssh -p "$port" "$host" "timeout 600 bash -c \"\$(printf %s $(printf %s "$1" | base64 -w0) | base64 -d)\""; }
fail() { failures=$((failures + 1)); echo "FAIL: $*"; }

unplant() {
  [ "$planted" = 1 ] || return 0
  remote "case '$fix' in */runbook-fixture) rm -rf -- '$fix' ;; esac" || fail "could not remove the fixture $fix"
  planted=0
}
cleanup() { unplant; rm -rf "$tmp"; }
trap cleanup EXIT
trap 'exit 130' INT TERM

plant() {
  remote "case '$fix' in */runbook-fixture) ;; *) exit 1 ;; esac
rm -rf -- '$fix' && mkdir -p '$fix/state/logs' '$fix/units' '$fix/deploy/build' &&
printf '%s\n' 'Fixture Title One.mkv 111' 'Fixture Title Two.mkv 222' 'Fixture Title Three.mkv 333' >'$fix/state/ledger.txt' &&
: >'$fix/units/dvremove.service' && : >'$fix/units/dvremove.timer' && : >'$fix/deploy/build/Dockerfile'" || return 1
  planted=1
}

# The preamble every step starts with: a user manager reachable over a non-interactive ssh,
# and, for a mutating step, the fixture in place of the live paths.
preamble() {
  echo 'export XDG_RUNTIME_DIR=/run/user/$(id -u)'
  if [ "$1" = mutating ]; then
    printf "export RB_ROOT='%s' STATE='%s/state' UNITS='%s/units' DEPLOY='%s/deploy'\n" "$root" "$fix" "$fix" "$fix"
    echo "export TITLE='Fixture Title Two.mkv' IMAGES='dvremove:current dvremove:fixture' SC='echo systemctl --user' DOCKER='echo docker'"
  fi
}

[ -d "$dir" ] || { echo "FAIL: no runbook folder: $dir"; exit 1; }

# Every runbook is named by the index, and every index entry has a file.
index=$dir/index.md
if [ -f "$index" ]; then
  /usr/bin/grep -oE '\]\([^)]+\.md\)' "$index" | sed -E 's/^\]\(//; s/\)$//' | sort -u >"$tmp/indexed"
else
  fail "no index: $index"; : >"$tmp/indexed"
fi
( cd "$dir" && for f in *.md; do [ -e "$f" ] && [ "$f" != index.md ] && echo "$f"; done ) | sort -u >"$tmp/present" || true
while IFS= read -r f; do fail "$f is missing from index.md"; done < <(comm -13 "$tmp/indexed" "$tmp/present")
while IFS= read -r f; do fail "index.md names $f, which has no file"; done < <(comm -23 "$tmp/indexed" "$tmp/present")
[ -s "$tmp/present" ] || fail "no runbook in $dir"

steps=0
while IFS= read -r name; do
  rm -rf "$tmp/blocks"; mkdir "$tmp/blocks"
  awk -v out="$tmp/blocks" '
    /^```/ { if (inb) { inb = 0; close(file); next }
             if (skip) { skip = 0; next }
             if ($0 ~ /^```bash( mutating)?[ \t]*$/) { n++; inb = 1; kind = ($0 ~ /mutating/) ? "mutating" : "live"
               print kind > (out "/" n ".kind"); close(out "/" n ".kind"); file = out "/" n ".sh"; printf "" > file }
             else skip = 1
             next }
    inb { print > file }
  ' "$dir/$name"
  blocks=$(find "$tmp/blocks" -name '*.sh' | wc -l)
  expects=$(/usr/bin/grep -c '^ *Expect:' "$dir/$name")
  [ "$blocks" = "$expects" ] || fail "$name: $blocks command blocks against $expects Expect lines"
  [ "$blocks" -gt 0 ] || fail "$name: no command block"
  n=1
  while [ "$n" -le "$blocks" ]; do
    kind=$(cat "$tmp/blocks/$n.kind")
    if [ "$kind" = mutating ] && [ "$planted" = 0 ] && ! plant; then
      fail "$name step $n: could not plant the fixture under $root"
      n=$((n + 1)); continue
    fi
    steps=$((steps + 1))
    script=$({ preamble "$kind"; cat "$tmp/blocks/$n.sh"; })
    if out=$(remote "$script" </dev/null 2>&1); then
      echo "ok: $name step $n"
    else
      rc=$?
      fail "$name step $n ($kind) exited $rc"
      printf '%s\n' "$out" | sed 's/^/    /'
    fi
    n=$((n + 1))
  done
  unplant
done <"$tmp/present"

echo "$steps steps run, $failures failed"
[ "$failures" -eq 0 ]
