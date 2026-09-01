#!/usr/bin/env bash
#
# Validate that commit subjects and the pull request title follow Conventional
# Commits (https://www.conventionalcommits.org).
#
# Run locally:   ./scripts/check-conventional-commits.sh
# In CI:         BASE_SHA/HEAD_SHA/PR_TITLE are supplied by the workflow.
set -euo pipefail

# type(optional scope)!: subject
PATTERN='^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9._/-]+\))?!?: .+'
MAX_SUBJECT=72

fail=0

check_subject() {
  local subject="$1" origin="$2"

  if [[ "$subject" =~ ^(Merge|Revert)\  ]]; then
    return 0
  fi
  if ! [[ "$subject" =~ $PATTERN ]]; then
    echo "::error::$origin does not follow Conventional Commits: '$subject'"
    fail=1
    return 0
  fi
  if (( ${#subject} > MAX_SUBJECT )); then
    echo "::error::$origin is ${#subject} characters, the limit is $MAX_SUBJECT: '$subject'"
    fail=1
  fi
}

if [[ -n "${PR_TITLE:-}" ]]; then
  check_subject "$PR_TITLE" "pull request title"
fi

base="${BASE_SHA:-}"
head="${HEAD_SHA:-HEAD}"
if [[ -z "$base" ]]; then
  base="$(git merge-base origin/main HEAD 2>/dev/null || git rev-parse HEAD~1)"
fi

while IFS= read -r subject; do
  [[ -z "$subject" ]] && continue
  check_subject "$subject" "commit"
done < <(git log --format=%s "$base..$head")

if (( fail )); then
  cat >&2 <<'USAGE'

Expected format:
  <type>(<optional scope>): <subject>

Types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert
Examples:
  feat(palette): rank recently used commands first
  fix(kubeconfig): keep the session context when the file is reloaded
  feat(api)!: drop the deprecated --context-name flag
USAGE
  exit 1
fi

echo "All commit subjects follow Conventional Commits."
