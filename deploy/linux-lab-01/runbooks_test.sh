#!/usr/bin/env bash
# Tests deploy/linux-lab-01/runbooks.sh with `ssh` replaced by a stub that runs each command locally.
# Decision: D24
set -u
here=$(cd "$(dirname "$0")" && pwd)
runner="$here/runbooks.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
cat >"$work/bin/ssh" <<'STUB'
#!/bin/sh
# Records the options and host, drops them, and runs the remote command here.
echo "$1 $2 $3" >>"$SSH_LOG"
shift 3
exec sh -c "$*"
STUB
chmod +x "$work/bin/ssh"
export PATH="$work/bin:$PATH" SSH_LOG="$work/ssh.log" RB_ROOT="$work/dvremove-test"
unset STATE UNITS DEPLOY TITLE IMAGES SC DOCKER

pass=0
fail=0
ok()  { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $*"; }

# runbook FILE TITLE: writes a runbook whose steps are read from standard input as
# `BLOCKKIND<TAB>command` lines, each followed by its Expect and If not lines.
runbook() {
  local f=$1 title=$2 kind cmd n=0
  {
    printf '# %s\n\nOwner: tester. Environment: linux-lab-01. Given: a stub host.\n\n' "$title"
    while IFS=$'\t' read -r kind cmd; do
      n=$((n + 1))
      printf '### %d. Step\n\n```%s\n%s\n```\nExpect: it exits 0.\nIf not: read the output.\n\n' "$n" "$kind" "$cmd"
    done
  } >"$f"
}

# index DIR NAME...: writes DIR/index.md naming each file.
index() {
  local d=$1; shift
  { printf '# Which runbook answers my question?\n\n'; for n; do printf -- '- [%s](%s)\n' "$n" "$n"; done; } >"$d/index.md"
}

# case LABEL DIR WANT: runs the runner over DIR; its exit status must be zero when WANT is 0, non-zero otherwise.
case_run() {
  label=$1 dir=$2 want=$3
  "$runner" -d "$dir" >"$work/out" 2>&1 </dev/null
  rc=$?
  if [ "$want" = 0 ]; then
    [ $rc -eq 0 ] && ok || { bad "$label: exited $rc, want 0"; sed 's/^/    /' "$work/out"; }
  else
    [ $rc -ne 0 ] && ok || bad "$label: exited 0, want non-zero"
  fi
}

# The clean set: a live step, and a mutating step that sees the planted fixture and not the live paths.
clean=$work/clean; mkdir -p "$clean"
runbook "$clean/a.md" 'Is it clean?' <<'EOS'
bash	[ -z "${STATE:-}" ] && true
bash mutating	case $STATE in "$RB_ROOT"/runbook-fixture/*) ;; *) exit 1 ;; esac; test -s "$STATE/ledger.txt" && test -f "$UNITS/dvremove.timer" && test -d "$DEPLOY/build" && test -n "$TITLE" && test -n "$IMAGES"
bash mutating	rm -f "$STATE/ledger.txt" && test ! -e "$STATE/ledger.txt"
EOS
runbook "$clean/b.md" 'Is the second one clean?' <<'EOS'
bash	true
EOS
printf '\nA sample, which is no step:\n\n```text\nfalse\n```\n' >>"$clean/b.md"
index "$clean" a.md b.md
: >"$SSH_LOG"
case_run clean "$clean" 0
[ -e "$RB_ROOT/runbook-fixture" ] && bad "clean: the fixture was left under $RB_ROOT" || ok
/usr/bin/grep -qx -- '-p 9999 192.168.1.3' "$SSH_LOG" && ok || bad "clean: ssh was not called with -p 9999 192.168.1.3"
n=$(wc -l <"$SSH_LOG")
[ "$n" -ge 4 ] && ok || bad "clean: only $n ssh calls for 4 steps and a fixture"

# A step that fails.
failing=$work/failing; mkdir -p "$failing"
runbook "$failing/a.md" 'Does it fail?' <<'EOS'
bash	true
bash	false
EOS
index "$failing" a.md
case_run failing-step "$failing" 1
/usr/bin/grep -q 'a.md' "$work/out" && ok || bad "failing-step: the output names no runbook"

# The first step fails while a later one passes: the status must not follow the last step.
first=$work/first; mkdir -p "$first"
runbook "$first/a.md" 'Does the first fail?' <<'EOS'
bash	exit 3
bash	true
EOS
index "$first" a.md
case_run first-step-fails "$first" 1

# A failing mutating step leaves no fixture.
mut=$work/mut; mkdir -p "$mut"
runbook "$mut/a.md" 'Does a mutating step fail?' <<'EOS'
bash mutating	false
EOS
index "$mut" a.md
case_run failing-mutating-step "$mut" 1
[ -e "$RB_ROOT/runbook-fixture" ] && bad "failing-mutating-step: the fixture was left" || ok

# A runbook missing from the index.
missing=$work/missing; mkdir -p "$missing"
runbook "$missing/a.md" 'Is a present?' <<'EOS'
bash	true
EOS
runbook "$missing/b.md" 'Is b present?' <<'EOS'
bash	true
EOS
index "$missing" a.md
case_run missing-from-index "$missing" 1
/usr/bin/grep -q 'b.md' "$work/out" && ok || bad "missing-from-index: the output does not name b.md"

# An index entry with no file.
nofile=$work/nofile; mkdir -p "$nofile"
runbook "$nofile/a.md" 'Is a present?' <<'EOS'
bash	true
EOS
index "$nofile" a.md ghost.md
case_run entry-without-file "$nofile" 1
/usr/bin/grep -q 'ghost.md' "$work/out" && ok || bad "entry-without-file: the output does not name ghost.md"

# No index at all.
noindex=$work/noindex; mkdir -p "$noindex"
runbook "$noindex/a.md" 'Is a present?' <<'EOS'
bash	true
EOS
case_run no-index "$noindex" 1

# A step with no Expect line: more blocks than expectations.
unpaired=$work/unpaired; mkdir -p "$unpaired"
runbook "$unpaired/a.md" 'Is every step paired?' <<'EOS'
bash	true
EOS
printf '```bash\ntrue\n```\n' >>"$unpaired/a.md"
index "$unpaired" a.md
case_run step-without-expect "$unpaired" 1

# A directory holding no runbook reads as a failure, not a pass.
empty=$work/empty; mkdir -p "$empty"
index "$empty"
case_run no-runbooks "$empty" 1

# A directory that does not exist.
case_run no-directory "$work/absent" 1

echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
