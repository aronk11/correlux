# Verifying a Correlux release

Correlux is handed a kubeconfig the first time it runs. Before that, it is a
binary somebody downloaded from the internet, and "does the checksum match?"
answers a smaller question than it looks like: a checksum published next to a
file proves the download was not corrupted, not that the project produced it.

Every release since v0.12.0 carries three things that answer the larger
question:

| File | What it is |
| --- | --- |
| `checksums.txt` | SHA-256 of every archive in the release |
| `checksums.txt.sig`, `checksums.txt.pem` | a keyless cosign signature over that list, and the certificate it was made with |
| `correlux_<version>_<os>_<arch>.tar.gz.sbom.json` | an SPDX SBOM of what is inside that archive |

plus a build provenance attestation, stored by GitHub rather than in the
release, that records which commit and which workflow run produced each file.

There is no private key anywhere in this. Signing happens inside the release
workflow with a certificate issued to its OIDC identity for the few seconds the
signature takes, so what you are checking is "GitHub asserts this came from
`aronk11/correlux`'s release workflow at a tag", not "somebody still has the
key they had two years ago".

## The quick path

If you have the GitHub CLI, this is the whole check:

```bash
gh attestation verify correlux_0.12.0_linux_amd64.tar.gz --repo aronk11/correlux
```

It succeeds only if that exact file was built by this repository's release
workflow, and it prints the commit and workflow run that produced it. Nothing
has to be configured first, and it works offline against a previously fetched
bundle with `--bundle`.

Verify the file you are about to install, not the one you meant to download.

## The full path

`cosign` does not depend on the GitHub CLI or on GitHub's attestation API, and
it checks a different thing: the signature over the release's checksum list.
Download `checksums.txt`, `checksums.txt.sig` and `checksums.txt.pem` from the
release, then:

```bash
cosign verify-blob checksums.txt \
  --signature checksums.txt.sig \
  --certificate checksums.txt.pem \
  --certificate-identity-regexp '^https://github\.com/aronk11/correlux/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

Both flags are required, and leaving either out is the mistake worth avoiding:
a signature that is *valid* only says some Sigstore certificate signed this.
The identity flags are what say **which** workflow in **which** repository, and
without them any GitHub Actions workflow anywhere would pass.

`--certificate-identity-regexp` rather than `--certificate-identity` because
the identity ends in the tag being released, which differs every time. The
pattern above is anchored at both ends of what matters: the repository and
workflow file are fixed, and only a `refs/tags/v…` ref is accepted, so a
signature made by this workflow running on a branch would not satisfy it.

Then check the archive against the list you just verified:

```bash
sha256sum --ignore-missing --check checksums.txt
```

On macOS, `shasum -a 256 --ignore-missing --check checksums.txt`. Windows has
no equivalent one-liner; compare `Get-FileHash .\correlux_0.12.0_windows_amd64.zip -Algorithm SHA256`
with the matching line by eye.

The order matters. Verify the signature on `checksums.txt` *first*, then check
the archive against it — checking the archive against an unverified list proves
nothing about who wrote the list.

## Using the SBOM

Each archive has an SPDX JSON document beside it on the release page listing
what went into it. It answers "is the thing my scanner just flagged in here?"
without unpacking the archive or rebuilding the project:

```bash
grype sbom:correlux_0.12.0_linux_amd64.tar.gz.sbom.json
```

Any SPDX-consuming scanner works; `grype` is one that reads the file directly.
To read it without a scanner, every dependency and its version is in there as
JSON:

```bash
jq -r '.packages[] | "\(.name) \(.versionInfo)"' correlux_0.12.0_linux_amd64.tar.gz.sbom.json | sort
```

Two things the SBOM is not. It is a statement about the release it ships with,
made when that release was built, so a dependency that becomes vulnerable next
month does not change it — the SBOM tells you whether you are affected, and the
release notes tell you what was done about it. And it is not signed on its own:
it is covered by `checksums.txt`, so verify that first if the SBOM's integrity
matters to you.

## Air-gapped environments

Verification needs no cluster and no Correlux, but `gh attestation verify` and
`cosign verify-blob` both reach Sigstore and GitHub by default. Do the
verification on the connected machine you download from, before transferring,
and carry the checksum across the boundary the way
[docs/air-gapped.md](air-gapped.md) describes. `cosign verify-blob` can work
against a previously fetched trust root with `--offline` and a bundle, which is
worth setting up only if your process requires verification to happen inside
the enclave.

## If verification fails

Do not install the binary, and do not work around it by skipping the check.

Re-download first: a truncated transfer fails a signature check the same way
tampering does, and it is overwhelmingly the more likely cause. If it fails
again on a clean download, that is worth reporting privately through the
process in [SECURITY.md](../SECURITY.md) — including the exact command and its
output — rather than in a public issue.
