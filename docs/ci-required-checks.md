# Required Merge Gates

GitHub Actions are the merge-readiness source of truth for pull requests into
`main`. Local checks are useful for fast feedback, but branch protection should
require the stable check names listed here before merge.

## Branch Protection Audit

Audit date: 2026-05-28.

`main` branch protection exists, but required status checks are not enabled yet.
The GitHub API endpoint for required status checks returned `404 Required status
checks not enabled`.

Repository admins should enable:

- Require a pull request before merging.
- Require status checks to pass before merging.
- Require branches to be up to date before merging.
- Required status checks from the table below.

Do not rename required workflow job IDs without updating this file and branch
protection in the same change.

## Required Checks

| Workflow | Job ID / required check | Gate |
|---|---|---|
| `ci-fast` | `container-only-go` | Direct host-side Go automation is blocked. |
| `ci-fast` | `mod-tidy` | Root and tools module files are tidy after containerized tidy. |
| `ci-fast` | `fmt` | Go formatting is clean in the container runner. |
| `ci-fast` | `vet` | Go vet is clean in the container runner. |
| `ci-fast` | `test` | Unit tests and coverage gate pass in the container runner. |
| `ci-fast` | `build` | Project packages build in the container runner. |
| `ci-fast` | `bootstrap-smoke` | Bootstrap config smoke test passes. |
| `ci-fast` | `compose-validate` | Docker Compose configuration is valid. |
| `security-baseline` | `secret-scan` | Gitleaks secret scan passes. |
| `security-baseline` | `govulncheck` | Runtime vulnerability reachability scan passes. |
| `security-baseline` | `toolchain-security` | Tools module dependency guardrails pass. |
| `dependency-review` | `dependency-review` | New PR dependency changes do not introduce high or critical vulnerabilities. |
| `supply-chain` | `sbom-and-licenses` | Go SBOM generation and license allowlist pass. |
| `supply-chain` | `vulnerability-scan` | Filesystem and image vulnerability scans pass. |
| `integration-ollama` | `ollama-e2e` | Full runtime startup and health smoke pass. |

## Non-Required Checks

`docker-test-stage` is intentionally non-required. It remains useful CI signal,
but the required `test` and `build` jobs are the stable merge gates for Go code.

## Red/Green Guard Proof

For any new required guard, keep the failure mode reproducible with a small PR
or local fixture that violates exactly one rule, verify that the matching job
fails, then remove the violation and verify that the job returns green. Record
the proof in the implementing PR body.
