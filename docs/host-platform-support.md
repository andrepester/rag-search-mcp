# Host Platform Support

`rag-search-mcp` is Docker-first, but the public entry points still run through
host `make`, POSIX shell scripts, Docker Compose, bind mounts, and local path
resolution. This document defines the supported host matrix and the smoke plan
for Docker workflows.

## Supported Matrix

| Host | Support level | Requirements | Notes |
|---|---|---|---|
| macOS | Supported | Docker Desktop with Compose plugin 2.20+ or newer, GNU Make 3.81+, POSIX `/bin/sh`, Git | Use GNU Make as `make` or run `gmake` for the documented targets. Docker Desktop file sharing must include the repository and any configured `HOST_*` paths. |
| Ubuntu LTS / Debian stable | Supported | Docker Engine 24+, Docker Compose plugin 2.20+ or newer, GNU Make 3.81+, POSIX `/bin/sh`, Git | This is the primary automated runtime CI environment. |
| Windows via WSL2 | Supported via WSL2 | WSL2 Linux distro, Docker Desktop WSL integration or Docker Engine in the distro, GNU Make 3.81+, Git | Run commands inside the Linux distro and keep the repository and mounted source directories on the Linux filesystem when possible. |
| Native Windows shells | Not supported | N/A | PowerShell, `cmd.exe`, Git Bash outside WSL2, and native Windows path semantics are outside v1 scope. |
| Podman or alternate Compose implementations | Best effort | N/A | Docker Engine/Desktop plus Docker Compose plugin is the supported runtime boundary. |

Minimum versions are project support floors, not a claim that older local
setups cannot work. Newer Docker Desktop and Docker Engine releases are
acceptable when they preserve the Compose and bind-mount behavior used by this
repository.

## Portability Guardrails

- Host scripts must remain POSIX `sh` compatible and start with `#!/bin/sh`.
- Repository automation must use LF line endings for shell scripts, Makefiles,
  Docker files, workflow YAML, Markdown, and `.env.example`.
- Do not use host-side Go tooling directly. Use the Dockerfile `go-runner`
  stage through the documented shell helpers or `make test`.
- Avoid GNU-only host assumptions such as `readlink -f`, `sed -i`, `grep -P`,
  `xargs -r`, `stat -c`, Bash arrays, `[[ ... ]]`, `source`, and `local`.
- Keep the public Make surface limited to the documented user targets. New CI or
  maintainer checks belong under `shell/` and are called directly by workflows.

The host-only automated guard is:

```bash
sh ./shell/ci-host-portability.sh
```

It does not require a Docker daemon and is expected to run on Ubuntu and macOS
GitHub-hosted runners.

## Runtime Smoke Plan

Run this sequence when validating a host in the supported matrix:

```bash
make help
sh ./shell/ci-host-portability.sh
docker compose --project-directory . -f docker/docker-compose.yml config
sh ./shell/bootstrap-smoke.sh
make test
make install
make doctor
make index
make clean-install
make down
```

`make install`, `make doctor`, `make index`, and `make clean-install` require
a working Docker daemon, image pulls, bind mounts, and enough local disk space
for Chroma index data.

## CI Coverage

The full Docker runtime path is automated on Ubuntu through the required CI and
integration jobs. The macOS CI coverage is intentionally host-only: it validates
line endings, POSIX-shell syntax, shebangs, and known BSD/GNU portability
hazards without relying on a Docker Desktop daemon inside GitHub-hosted macOS.

Until a self-hosted macOS or WSL2 runner exists, macOS Docker Desktop and WSL2
runtime evidence should be captured in the PR body using the smoke plan above.

## Known Limits

- `HOST_DOCS_DIR`, `HOST_CODE_DIR`, and `HOST_INDEX_DIR` should resolve to
  directories visible to Docker. On macOS Docker Desktop, paths outside shared
  file roots will fail at mount time.
- WSL2 works best when the repository and data directories live in the distro
  filesystem, not under `/mnt/c`.
- File ownership can differ between native Linux and Docker Desktop. The
  project runs Go tooling in containers with the current host UID/GID where it
  writes into the repository.
- CRLF line endings in shell scripts or Makefiles are treated as unsupported
  automation drift, even if a specific local shell happens to tolerate them.
- Issue triage should classify native Windows shell, alternate container
  runtime, remote Docker context, and unsupported Compose-version reports as
  best effort unless a maintainer explicitly expands the support matrix.
