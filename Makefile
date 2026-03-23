.DEFAULT_GOAL := help

INSTALL_SH = ./lib/install.sh
REINDEX_SH = ./lib/reindex.sh
DOCTOR_SH = ./lib/doctor.sh
INSTALL_ARGS =

.PHONY: help install setup install-help install-dry-run install-yes install-no-ollama install-no-config post-install reindex doctor check

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
	@printf '  %-20s %s\n' 'make reindex' 'Rebuild the local vector index'
	@printf '  %-20s %s\n' 'make doctor' 'Run health checks'
	@printf '  %-20s %s\n' 'make check' 'Alias for doctor'
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

reindex:
	$(REINDEX_SH)

doctor:
	$(DOCTOR_SH)

check: doctor
