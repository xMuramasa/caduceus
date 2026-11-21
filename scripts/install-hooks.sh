#!/usr/bin/env bash
# Link hooks/pre-push into the main checkouts of gitflow repos (caduceus#11); worktrees share their checkout's hooks.
#   scripts/install-hooks.sh [checkout ...]   default: every ~/Projects/* checkout whose origin has a develop branch
#   scripts/install-hooks.sh --mac PATH ...   copy the hook into checkouts on the Mac (ssh mac)
# Run it from the main caduceus checkout: the links point at this checkout's hooks/pre-push, which follows develop.
set -euo pipefail

root="$(cd "$(dirname "$(readlink -f "$0")")/.." && pwd)"
hook="$root/hooks/pre-push"
case "$root" in *-wt/*) echo "run this from the main caduceus checkout, not a worktree ($root)" >&2; exit 1 ;; esac

if [ "${1:-}" = "--mac" ]; then
  shift
  for co in "$@"; do
    scp -q "$hook" "mac:$co/.git/hooks/pre-push"
    ssh mac "chmod 755 '$co/.git/hooks/pre-push'"
    echo "copied to mac:$co"
  done
  exit 0
fi

if [ $# -eq 0 ]; then
  set --
  for d in "${CADUCEUS_PROJECTS:-$HOME/Projects}"/*/; do
    d="${d%/}"
    [ -d "$d/.git" ] && git -C "$d" rev-parse -q --verify refs/remotes/origin/develop >/dev/null && set -- "$@" "$d"
  done
fi

for co in "$@"; do
  target="$co/.git/hooks/pre-push"
  if [ -e "$target" ] && [ ! -L "$target" ] && ! grep -q "Caduceus gitflow guard" "$target"; then
    echo "skipped $co: it has its own pre-push hook" >&2
    continue
  fi
  mkdir -p "$co/.git/hooks"
  ln -sfn "$hook" "$target"
  echo "linked $co"
done
