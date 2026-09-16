# Release pipeline v1

`make release` builds locally and never publishes. The GitHub Actions workflow
`.github/workflows/release-platform-check.yml` publishes verified `main` builds
as GitHub Releases. Push Service OCI images still have no remote publication path.

## Reproducible build

Start from a clean checkout with the pinned public
[`course`](https://github.com/course-go-autumn-2026/course) submodule and protobuf tools:

```bash
make clean
make proto-tools-install
make release-repro-check VERSION="main-$(git rev-parse HEAD)" \
  COMMIT="$(git rev-parse HEAD)" \
  SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)"
```

`release` requires a committed superproject `HEAD`, `COMMIT` equal to its full
object ID, a clean index and worktree, and `third_party/homework` pinned in that
committed tree. A staged-only gitlink advance, checkout/index mismatch, dirty
source, or unborn checkout is rejected. Development `contract-check` permits
staged updates; it is not the release provenance gate.

`release` performs, in order:

1. exact relative-symlink, committed contract bytes, HEAD/index/checkout gitlink,
   and SHA verification;
2. clean protobuf regeneration comparison;
3. ignored staging of verified canonical OpenAPI/proto/manifest bytes;
4. embedded-contract tests;
5. static `linux/amd64` and `linux/arm64` Push Service builds;
6. architecture-matched Push staging and four final CLI builds;
7. exact embedded-byte inclusion checks against every final CLI;
8. deterministic archives, executable-mode/member audit, and `SHA256SUMS`;
9. deletion of intermediates and staging; failed builds or audits remove the
   entire release directory.

Only four `tar.gz` archives, `SHA256SUMS`, and `RELEASE_NOTES.md` remain in
`build/release`. Archives contain exactly `tripgoctl`, `RELEASE_NOTES.md`, and
`LICENSE` (MIT). The build and cleanup audits verify the archived license bytes.
The CLI embeds verified contracts because `go:embed` cannot follow symlinks.
`release-repro-check` rebuilds twice with the same toolchain and inputs, then
compares checksums and notes. CI currently uses the latest Go 1.24 patch.

## CI and publication

The workflow runs on every PR, push to `main`, and manual dispatch:

1. `make source-check` (format, vet, race tests, script fixtures, contracts,
   generated protobuf, and staging cleanup).
2. Build the four-platform release twice on Linux and verify reproducibility.
   Version is `main-<full HEAD SHA>`; build time is the source commit timestamp.
3. Upload `build/release` as the `tripgoctl-release` Actions artifact, retained
   for 14 days. No source tree, submodule, or tool cache is uploaded.
4. Download those exact archives on native Linux/macOS amd64/arm64 runners.
   Run installer fixtures, install the matching archive, check build metadata
   and `--help`. Linux additionally runs the full Docker/kind runtime gate.
5. Only after all checks pass, `scripts/publish-release` publishes from trusted
   `course-go-autumn-2026/course-infra` `main` push/manual runs. PRs and forks
   never publish, and no job uses `pull_request_target`.

Only the publication job has `contents: write`. It uses `GITHUB_TOKEN`, not a
personal token. Checkout does not persist credentials; actions are pinned to
commit SHAs. Main runs are serialized without cancellation during publication.
The publisher skips a commit that is no longer `main` HEAD, so rerunning an old
workflow cannot move `latest` backwards.

Release assets are the four tested archives, `SHA256SUMS`, `RELEASE_NOTES.md`,
and `scripts/install-tripgoctl` from the same checked-out commit. The publisher
rechecks archive checksums, creates a **draft** with an explicit target commit,
uploads all assets, then publishes and marks it `latest`. Nothing is rebuilt.
These are ordinary releases, not GitHub prereleases: `/releases/latest` does not
select prereleases. There is no separate stable channel yet.

An already published version is left unchanged. A failed upload leaves an
unpublished draft, not a partial `latest`. If a draft remains, inspect its assets
and target commit, delete only that incomplete draft in GitHub, and rerun the
workflow. The script intentionally refuses to overwrite existing drafts or
published assets. Enable GitHub release immutability to enforce this server-side.

Actions artifacts are for CI/debugging, not anonymous installation: they expire
and require GitHub authentication. Public Release assets provide the download URLs
used by students. Pinned release tags allow repeatable installs and rollback.

## Installation

After the repository is public and its first release is published:

```sh
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL \
  https://github.com/course-go-autumn-2026/course-infra/releases/latest/download/install-tripgoctl \
  | sh
tripgoctl version
```

Alternatively, download and inspect the script before invoking `sh`; see README.
The POSIX installer supports macOS/Linux amd64/arm64, needs `curl`, `tar`, and
`sha256sum` or `shasum`, and defaults to `latest`. It resolves `latest` once to a
concrete tag, validates that tag, and uses version-pinned HTTPS URLs for both the
archive and checksum. An explicit version skips discovery. The installer is a
complete function followed by its invocation, so a truncated function received
via a pipe does not start installing.

For local build output, specify its exact version without network access:

```bash
TRIPGOCTL_RELEASE_DIR=build/release \
  ./scripts/install-tripgoctl "main-$(git rev-parse HEAD)"
```

Both `TRIPGOCTL_RELEASE_DIR` and a custom `TRIPGOCTL_BASE_URL` require an explicit
version. `TRIPGOCTL_REPOSITORY` overrides the default GitHub repository.
Destination is writable `/usr/local/bin`, otherwise `$HOME/.local/bin`;
`--install-dir` or `TRIPGOCTL_INSTALL_DIR` overrides it. No `sudo` or shell-profile
changes are made. Installation uses a temporary file in the destination directory
and atomic rename; download or checksum failures preserve the existing binary.
SHA-256 ensures archive integrity, not authenticity if GitHub or the installer
itself is compromised. No independent signing/attestation scheme is provided.

`make install` is different: it builds a full self-contained CLI from the current
source checkout and installs it locally. It does not download a release.

## Runtime coverage

`make release-platform-check` selects the archive matching the native host,
extracts it, and runs lab 3 through the real CLI: owned registry, local Push image
build/push, immutable RepoDigest, Kubernetes pull, and HTTP/gRPC behavior.
Preflight refuses an existing fixed-name cluster or registry. Cleanup uses the
CLI identity gate and removes only Push references absent from its baseline.

CI runs this full gate on **Linux amd64/arm64**. On macOS amd64/arm64 it checks
installation and CLI execution only. Hosted macOS arm64 runners do not support
nested virtualization, so the previous Colima-based four-platform Docker gate
is not used. Before the first public release, run the full gate on real Macs
with Docker on both supported architectures. Continuous macOS Docker coverage
would require suitable separate runners; it is not claimed by this workflow.
A workflow definition is not evidence that its native checks have passed.

## First public release checklist

1. Review all Git refs/history and existing Actions logs for secrets and internal
   material. Revoke any exposed credentials before changing visibility.
2. Preserve the project's MIT `LICENSE` in source and binary distributions;
   external contracts and dependencies retain their own licensing terms.
   The public course submodule does not expose `course-internal`.
3. Protect `main` with required review/checks, restrict release-tag changes, and
   enable release immutability. Keep publication write permissions scoped to its job.
4. Complete the macOS Docker acceptance above. Commit the gitlink, symlinks,
   metadata, and workflow changes together; never bypass the clean-release gate.
5. Make `course-infra` public, then merge to `main` or manually dispatch the
   workflow from `main`. Check the native jobs and all seven Release assets.
6. From a clean machine, test anonymous latest and pinned installs, `version`,
   `doctor`, and the lab runtime. Docker must be running for infrastructure use.

Repository visibility, protection rules, license selection, and the first remote
publication are maintainer actions, not side effects of local build commands.

## Cleanup and dependency updates

`make release-audit` rejects staging, project `.env`/non-golden `.tripgo`, temporary
editor files, unexpected release files/members, checksum drift, and remaining
`tripgo-local*` containers. `make clean` removes all generated build/staging output.

Update one kind, Kubernetes/node, or catalog OCI pin at a time. Verify both Linux
architectures, update the affected tests/docs, run `make verify`, the relevant
integration, reproducibility, and native Linux runtime gates. Review the generated
release inventory before merging.

Canonical contracts are under `homework/contracts/` in the pinned public `course`
submodule at `third_party/homework`. Advance its gitlink, run `make contract-sync`,
regenerate protobuf, and commit together. `api/openapi/push-service.openapi.yaml`
and `api/proto/push/v1/push.proto` must remain relative symlinks, not fallback copies.

SBOM and build attestations remain deferred until a pinned deterministic generator
is selected. This change adds binary distribution, not another packaging system.
