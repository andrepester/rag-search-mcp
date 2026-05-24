# Validate Profile

This repository is Docker-first. Prefer documented `make` targets and CI-equivalent shell checks; avoid direct local `go` execution.

## Always Run

- `git status --short --branch`
- `git diff --check`
- `docker compose --project-directory . -f docker/docker-compose.yml config`
- `make test`

## Run When Relevant

For Go, Dockerfile, shell, or workflow changes:

- `sh ./shell/ci-fmt-check.sh`
- `sh ./shell/ci-vet.sh`
- `sh ./shell/ci-build.sh`
- `sh ./shell/go-runner-overrides-smoke.sh`

For dependency, security, Docker base image, or CI security changes:

- `make govulncheck`

## Rules

- Do not run `make clean-install FULL_RESET=1` unless explicitly requested.
- Do not mutate `.env` during validation unless the task is about install/bootstrap behavior.
- For docs-only changes, `git diff --check` and targeted `rg` checks may be sufficient, but still note when `make test` was skipped.
- Report exact commands and pass/fail status.
