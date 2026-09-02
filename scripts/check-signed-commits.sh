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

# The signature is read off the commit object itself rather than through
# `git log --format=%G?`.
#
# %G? asks git to *verify*, which needs to know the signature format and, for
# SSH, an allowed-signers file. A CI checkout has neither, so a perfectly good
# SSH signature comes back as "N: no signature" and the check fails every commit
# it was written to protect. The header is the fact this script is after; whose
# key it is, and whether that key is trusted, is GitHub's job (ADR 15).
fail=0
while read -r sha subject; do
  if ! git cat-file commit "$sha" | sed -n '/^$/q;p' | grep -q '^gpgsig'; then
    echo "::error::commit $sha is not signed: $subject"
    fail=1
  fi
done < <(git log --format='%H %s' "$base..$head")

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
