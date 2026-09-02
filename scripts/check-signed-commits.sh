#!/usr/bin/env bash
#
# Verify that every commit in the range carries a signature.
#
# CI cannot check *whose* signature it is — that requires the signer's public
# key, which is what GitHub's "Verified" badge and the repository's "require
# signed commits" rule are for. What this script catches is the common mistake:
# a commit made on a machine where signing was not configured.
#
# Run locally:  ./scripts/check-signed-commits.sh
# In CI:        BASE_SHA/HEAD_SHA are supplied by the workflow.
set -euo pipefail

base="${BASE_SHA:-}"
head="${HEAD_SHA:-HEAD}"
if [[ -z "$base" ]]; then
  base="$(git merge-base origin/main HEAD 2>/dev/null || git rev-list --max-parents=0 HEAD)"
fi

fail=0
while read -r sha status subject; do
  # %G? reports: G good, U good-but-untrusted, X expired, Y expired key,
  # R revoked key, E cannot check, B bad, N no signature.
  case "$status" in
    N)
      echo "::error::commit $sha is not signed: $subject"
      fail=1
      ;;
    B)
      echo "::error::commit $sha has a bad signature: $subject"
      fail=1
      ;;
  esac
done < <(git log --format='%H %G? %s' "$base..$head")

if (( fail )); then
  cat >&2 <<'USAGE'

Every commit must be signed. To configure SSH signing once:

  git config --global gpg.format ssh
  git config --global user.signingkey ~/.ssh/id_ed25519.pub
  git config --global commit.gpgsign true

Then register the same public key on GitHub as a *signing* key:

  gh ssh-key add ~/.ssh/id_ed25519.pub --type signing --title "$(hostname)"

To sign commits you already made on this branch:

  git rebase --root -f
USAGE
  exit 1
fi

echo "All commits are signed."
