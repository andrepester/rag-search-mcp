.DEFAULT_GOAL := help

INSTALL_SH = ./lib/install.sh
REINDEX_SH = ./lib/reindex.sh
DOCTOR_SH = ./lib/doctor.sh
INSTALL_ARGS =

.PHONY: help install setup install-help install-dry-run install-yes install-no-ollama install-no-config post-install update upgrade upgrade-package reindex doctor

help:
	@printf '%s\n' 'Available targets:'
	@printf '  %-20s %s\n' 'make help' 'Show this help'
	@printf '  %-20s %s\n' 'make install' 'Run the installer'
	@printf '  %-20s %s\n' 'make setup' 'Alias for install'
	@printf '  %-20s %s\n' 'make install-help' 'Show installer options'
	@printf '  %-20s %s\n' 'make install-dry-run' 'Preview installer actions'
	@printf '  %-20s %s\n' 'make install-yes' 'Run installer without prompts'
	@printf '  %-20s %s\n' 'make install-no-ollama' 'Skip ollama install/start/model pull'
	@printf '  %-20s %s\n' 'make install-no-config' 'Skip opencode.json generation'
	@printf '  %-20s %s\n' 'make update' 'Sync local Python deps from uv.lock'
	@printf '  %-20s %s\n' 'make upgrade' 'Upgrade Python deps and refresh uv.lock'
	@printf '  %-20s %s\n' 'make upgrade-package PACKAGE=<name>' 'Upgrade one dependency and refresh uv.lock'
	@printf '  %-20s %s\n' 'make reindex' 'Rebuild the local vector index'
	@printf '  %-20s %s\n' 'make doctor' 'Run health checks'
	@printf '%s\n' ''
	@printf '%s\n' 'Pass installer options through INSTALL_ARGS:'
	@printf '  %s\n' 'make install INSTALL_ARGS="--yes --skip-ollama"'

install:
	$(INSTALL_SH) $(INSTALL_ARGS)
	@$(MAKE) post-install

setup: install

install-help:
	$(INSTALL_SH) --help

install-dry-run:
	$(INSTALL_SH) --dry-run

install-yes:
	$(INSTALL_SH) --yes
	@$(MAKE) post-install

install-no-ollama:
	$(INSTALL_SH) --skip-ollama
	@$(MAKE) post-install

install-no-config:
	$(INSTALL_SH) --skip-config
	@$(MAKE) post-install

post-install:
	@printf '%s\n' ''
	@printf '%s\n' 'Next steps:'
	@printf '  %s\n' '1. Add documents to your configured docs directory (.env defaults to docs/)'
	@printf '  %s\n' '2. Run: make reindex'
	@printf '  %s\n' '3. Run: make doctor'
	@printf '  %s\n' '4. Start OpenCode in this directory'
	@printf '  %s\n' '5. Test: ask OpenCode to use local_rag_search_docs'
	@printf '  %s\n' '   If you used --skip-ollama or --skip-config, finish that setup first.'

update:
	@command -v uv >/dev/null 2>&1 || { printf '%s\n' '[error] uv not found on PATH. Run make install first.' >&2; exit 1; }
	uv sync --directory "$(CURDIR)"

upgrade:
	@command -v uv >/dev/null 2>&1 || { printf '%s\n' '[error] uv not found on PATH. Run make install first.' >&2; exit 1; }
	uv lock --directory "$(CURDIR)" --upgrade
	uv sync --directory "$(CURDIR)"

upgrade-package:
	@command -v uv >/dev/null 2>&1 || { printf '%s\n' '[error] uv not found on PATH. Run make install first.' >&2; exit 1; }
	@test -n "$(PACKAGE)" || { printf '%s\n' '[error] PACKAGE is required (example: make upgrade-package PACKAGE=chromadb)' >&2; exit 1; }
	uv lock --directory "$(CURDIR)" --upgrade-package "$(PACKAGE)"
	uv sync --directory "$(CURDIR)"

reindex:
	$(REINDEX_SH)

doctor:
	$(DOCTOR_SH)
