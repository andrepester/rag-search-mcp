# Container Supply-Chain Security

This policy covers Dockerfile base images, runtime container images, filesystem
SBOMs, image SBOMs, and the CI gates that decide whether those artifacts are
merge-ready.

## Required Gates

The `supply-chain` workflow is a required merge gate for runtime artifacts:

- `container-policy` verifies that Dockerfile base images are digest-pinned,
  Grype scans do not use exception mechanisms, and every Grype scan fails on
  high-or-higher findings.
- `sbom-and-licenses` generates the Go CycloneDX SBOM, exports dependency
  licenses, and enforces the allowed license set.
- `vulnerability-scan` builds the runtime image, generates filesystem and image
  CycloneDX SBOMs, uploads scan artifacts, and runs Grype against both SBOMs.

Grype scans must use `--fail-on high`. This blocks high and critical findings.
The scans must not use `--only-fixed`; unfixed high and critical findings block
the merge path as well.

## No Exceptions

Container vulnerability exceptions are not allowed in this repository. Do not
add Grype ignore files, Grype config files, `--ignore`, `--config`, `--only-fixed`,
or workflow conditions that bypass the required container scans.

When a high or critical finding appears, the allowed remediation paths are:

- update the affected base image, runtime dependency, or application code;
- remove the affected component from the runtime artifact;
- wait for the upstream vendor fix and keep the merge blocked until the fix is
  available and applied.

A false positive or urgent release pressure is not a local CI exception. Changing
this policy requires an explicit product/security decision and a normal reviewed
repository change.

## Base Image Policy

Every Dockerfile `FROM` instruction must use the readable tag plus immutable
digest form:

```dockerfile
FROM image:tag@sha256:digest
```

The tag keeps the intended update track visible to humans and Dependabot. The
digest pins the exact OCI image index used by builds and scans.

Base image updates must:

- update the tag and digest together in the same change;
- resolve the digest from the registry metadata for the selected tag;
- run `sh ./shell/ci-container-supply-chain-policy.sh`;
- run the relevant CI or local supply-chain checks before release;
- keep the previous tag and digest available in git history for rollback.

Rollback means reverting to the last known-good tag and digest pair, then
rerunning the same policy and scan gates.

## Product Dependencies And Delivery Tooling

The Go dependency license allowlist permits MIT, MIT-0, BSD-2-Clause,
BSD-3-Clause, Apache-2.0, ISC, and MPL-2.0. The shared gate is
`sh ./shell/ci-licenses-check.sh`; unknown licenses remain blocked.

The pinned `go-licenses` scanner does not recognize the MIT-0 license used by
`github.com/segmentio/asm` starting with v1.2.1. License export normalizes only
that module's unknown package rows after checking the SHA-256 digest of its
complete LICENSE file against the reviewed upstream text. The reference text
is in `shell/testdata/licenses/segmentio-asm-MIT-0.txt`, copied from
<https://github.com/segmentio/asm/blob/v1.2.1/LICENSE>. A changed license fails
closed and requires review; unrelated unknown licenses are never normalized.
`sh ./shell/ci-licenses-policy-smoke.sh` verifies acceptance and rejection cases
as part of the existing `sbom-and-licenses` CI job.

Runtime/product dependencies are governed by the root Go module, the Dockerfile,
runtime SBOMs, and runtime vulnerability scans.

The runtime image removes `apk-tools` after creating the non-root user so that
the package manager and its unused libraries are not shipped. The CA bundle is
copied from the digest-pinned Go base image to preserve HTTPS trust without
retaining those runtime packages.

Delivery and scan tooling is governed separately:

- Go-based CI tools live in the `tools` Go module and are checked by
  `security-baseline` / `toolchain-security`.
- Anchore binaries used by `supply-chain` are pinned in
  `shell/ci-tool-versions.env` and installed through
  `shell/install-anchore-tools.sh` with checksum verification.

Tooling findings and runtime findings are reported separately, but either class
can block merge readiness through its required workflow.

## Local Policy Proof

The static policy guard is:

```bash
sh ./shell/ci-container-supply-chain-policy.sh
```

The red/green smoke proof is:

```bash
sh ./shell/ci-container-supply-chain-policy-smoke.sh
```

The smoke test builds a temporary fixture, verifies the clean policy state, then
checks that the guard rejects an unpinned base image, an `--only-fixed` Grype
scan, and a Grype config file.
