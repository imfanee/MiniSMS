#!/usr/bin/env bash
# Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
#
# Standing guard: refuse to commit/push if any tracked/staged file contains
# sensitive or business-identifying tokens. The repo is public on GitHub; keep
# real carrier/client/gateway/vendor/destination names, MSISDNs, IPs, hostnames,
# and credentials out of it (see CLAUDE.md "Sensitive / business data ...").
#
# This script is committed, so it deliberately contains NO business names. The
# project-specific denylist (the actual names/IPs/numbers) lives in a GITIGNORED
# file at .claude/leak-denylist.local, one POSIX-ERE pattern per line ('#'
# comments and blank lines ignored). Generic high-confidence patterns are built
# in below. Wired as a Claude Code PreToolUse(Bash) hook for git commit/push and
# also usable as a git pre-commit hook. Exit 2 blocks; exit 0 allows.
set -u

# Claude Code PreToolUse(Bash) passes the tool-call JSON on stdin. Only act on
# git commit/push; allow everything else. Standalone (tty) runs skip this gate.
if [ ! -t 0 ]; then
  INPUT="$(cat 2>/dev/null || true)"
  if [ -n "$INPUT" ] && ! printf '%s' "$INPUT" | grep -qE '"command".*git.*(commit|push)'; then
    exit 0
  fi
fi

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo .)"
cd "$ROOT" || exit 0

# Generic, non-identifying high-confidence patterns (safe to keep in git).
PATTERNS=(
  '-----BEGIN [A-Z ]*PRIVATE KEY-----'
  'AKIA[0-9A-Z]{16}'
)
# Project-specific denylist (gitignored; holds the real names/IPs/numbers).
DENY=".claude/leak-denylist.local"
if [ -f "$DENY" ]; then
  while IFS= read -r line; do
    case "$line" in ''|\#*) continue;; esac
    PATTERNS+=("$line")
  done < "$DENY"
fi

# Build one alternation and scan staged index + tracked tree (guard excluded).
PAT="$(IFS='|'; echo "${PATTERNS[*]}")"
EXCL=':(exclude)scripts/git-leak-guard.sh'
HITS=$(
  { git grep --cached -nIE -e "$PAT" -- . "$EXCL" 2>/dev/null
    git grep        -nIE -e "$PAT" -- . "$EXCL" 2>/dev/null; } | sort -u
)
if [ -n "$HITS" ]; then
  echo "BLOCKED: sensitive/business tokens found in tracked files (must not reach GitHub):" >&2
  echo "$HITS" >&2
  echo "Genericize with placeholders before committing (see CLAUDE.md sensitive-data rule)." >&2
  exit 2
fi
exit 0
