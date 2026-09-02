# Local release pipeline v1

The stage 12 pipeline creates self-contained `tripgoctl` archives locally. It does not create a Git tag, commit, remote OCI image, release, attestation, or upload.

## Reproducible build

Start from a clean checkout with the pinned homework submodule and protobuf tools:

```bash
make clean
make proto-tools-install
# Full gate: performs two release builds and compares their output.
make release-repro-check VERSION=v1.0.0 \
  COMMIT="$(git rev-parse HEAD)" \
  SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)"
```

`release` requires an existing superproject `HEAD`, requires `COMMIT` to equal
its full object ID, requires the complete superproject index and worktree to be
clean, and resolves `third_party/homework` from that committed tree. A merely
staged gitlink advance, any tracked or untracked source change, an
index/checkout mismatch, or an unborn superproject is rejected. The separate development `contract-check` may be used
while bootstrapping an unborn checkout, but it is not a release provenance gate.

`release` performs, in order:

1. exact relative-symlink, clean committed contract bytes, HEAD-pinned homework
   gitlink equal to the index and checkout, and SHA contract gate;
2. clean protobuf regeneration comparison;
3. package-local ignored staging of verified canonical OpenAPI/proto/manifest bytes;
4. tagged embedded-contract tests;
5. static `linux/amd64` and `linux/arm64` Push Service builds;
6. architecture-matched Push staging and four final CLI builds;
7. exact byte inclusion checks against every final CLI;
8. deterministic archive creation, executable-mode and member audit, and `SHA256SUMS`;
9. deletion of raw intermediates and both ignored staging directories; any failed
   build or post-build cleanup/archive audit removes the entire release directory.

The only retained files are four `tar.gz` archives, `SHA256SUMS`, and `RELEASE_NOTES.md`. Each archive contains exactly `tripgoctl` and `RELEASE_NOTES.md`; source, tests, smoke scripts, rootfs, contracts as separate files, and build staging are excluded. The CLI itself directly embeds the verified canonical contract bytes because `go:embed` cannot follow the canonical symlinks.

SBOM and provenance are deferred until a pinned generator can produce output without wall-clock time, host paths, or environment-dependent metadata.

## Native four-platform Docker gate

`make release-platform-smoke` selects the final archive matching the native Darwin/Linux amd64/arm64 host. It extracts that archive and runs lab 3 through the real CLI, including its owned registry, local Push image build/push, immutable RepoDigest, Kubernetes pull, and HTTP/gRPC behavior. A non-destructive preflight refuses to run when the fixed-name cluster or registry already exists. Cleanup uses the CLI identity gate and removes only Push image references absent from the preflight inventory, preserving developer-owned state. The four native jobs in `.github/workflows/release-smoke-platforms.yml` are defined for both architectures on Linux and Darwin without publishing artifacts. They have not run from the current repository state: the definition is not execution evidence. Native Linux and a clean external machine therefore remain unverified release gates.

The release build separately proves exact architecture-matched Push ELF, Dockerfile, and canonical contract inclusion in every final CLI before archiving, then proves each archive contains that exact verified CLI. Native jobs ensure that this byte-level gate is complemented by actual execution on all four supported CLI platforms.

## Cleanup audit

After a local Docker smoke, run:

```bash
make release-audit
```

The audit rejects package-local staging, project `.env`/non-golden `.tripgo`, temporary editor files, unexpected release files, archive members, checksum drift, and remaining `tripgo-local*` containers. `make clean` removes all generated build and staging output.

## Updating pinned dependencies

1. Update one kind, Kubernetes/node, or catalog OCI pin at a time.
2. Verify the artifact digest for both `linux/amd64` and `linux/arm64`; never replace a digest with a mutable tag.
3. Update tests and the relevant catalog/design document.
4. Run `make verify`, the lifecycle smoke affected by the component, `make release-repro-check`, and native Linux release smoke on both architectures.
5. Review generated `RELEASE_NOTES.md` inventory before any separately approved publication process.

Canonical Push contracts are updated only by advancing `third_party/homework`, then running `make contract-sync` and protobuf generation. `api/openapi/push-service.openapi.yaml` and `api/proto/push/v1/push.proto` must remain relative symlinks; tracked fallback copies are forbidden.
