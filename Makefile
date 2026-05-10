# Soft Serve - developer convenience targets.
#
# These targets manage a local Garage daemon used as an S3-compatible target
# for integration tests under `go test -tags integration ./...`. The Garage
# binary must be on PATH (e.g. `brew install garage`) or pointed at via
# GARAGE_BIN.

GARAGE_DIR     := .garage
GARAGE_BIN     ?= garage
GARAGE_CONFIG  := $(GARAGE_DIR)/garage.toml
GARAGE_PID     := $(GARAGE_DIR)/garage.pid
GARAGE_LOG     := $(GARAGE_DIR)/garage.log
GARAGE_ENV     := $(GARAGE_DIR)/garage.env

# Override if these collide with something else on your machine.
GARAGE_S3_PORT    ?= 3900
GARAGE_RPC_PORT   ?= 3901
GARAGE_ADMIN_PORT ?= 3903

GARAGE_KEY_NAME ?= soft-serve-it

# Install layout (override on the make command line if needed).
PREFIX        ?= /usr/local
BINDIR        ?= $(PREFIX)/bin
SYSTEMD_DIR   ?= /etc/systemd/system
SOFT_SERVE_USER  ?= soft-serve
SOFT_SERVE_GROUP ?= soft-serve
SOFT_SERVE_HOME  ?= /var/lib/soft-serve
SOFT_SERVE_BIN   ?= $(BINDIR)/soft

BUILD_DIR ?= bin
BUILD_BIN := $(BUILD_DIR)/soft

# Remote deployment (cross-compile macOS → Linux)
DEPLOY_HOST  ?=
DEPLOY_USER  ?=
DEPLOY_BIN   ?= $(SOFT_SERVE_BIN)
DEPLOY_SSH   := $(if $(DEPLOY_USER),$(DEPLOY_USER)@$(DEPLOY_HOST),$(DEPLOY_HOST))

.PHONY: help
help:
	@echo "Targets:"
	@echo "  garage-up         Start a local Garage daemon for integration tests"
	@echo "  garage-down       Stop the local Garage daemon"
	@echo "  garage-reset      Stop the daemon, wipe state, and start fresh"
	@echo "  garage-status     Show whether the daemon is running and print the env"
	@echo "  test-integration  Run integration-tagged tests against the daemon"
	@echo "  test              Run the regular unit-test suite"
	@echo "  build             Compile the soft binary into $(BUILD_DIR)/"
	@echo "  install-binary    Build and install the soft binary, replacing $(SOFT_SERVE_BIN) (run as root)"
	@echo "  install-systemd   Install the soft-serve systemd unit (run as root)"
	@echo "  uninstall-systemd Stop, disable, and remove the systemd unit (run as root)"
	@echo "  deploy            Cross-compile for Linux, scp to DEPLOY_HOST, and restart soft-serve.service"
	@echo "                    (requires DEPLOY_HOST=..., optional DEPLOY_USER=..., DEPLOY_BIN=...)"

.PHONY: test
test:
	go test ./...

.PHONY: test-integration
test-integration:
	@if [ ! -f $(GARAGE_ENV) ]; then \
		echo "run 'make garage-up' first"; exit 1; \
	fi
	@set -a; . $(GARAGE_ENV); set +a; go test -tags integration ./...

.PHONY: garage-up
garage-up: $(GARAGE_CONFIG)
	@if [ -f $(GARAGE_PID) ] && kill -0 "$$(cat $(GARAGE_PID))" 2>/dev/null; then \
		echo "garage is already running (PID $$(cat $(GARAGE_PID)))"; \
	else \
		echo "starting garage..."; \
		nohup $(GARAGE_BIN) -c $(GARAGE_CONFIG) server >$(GARAGE_LOG) 2>&1 & echo $$! > $(GARAGE_PID); \
		$(MAKE) --no-print-directory _garage-wait-ready; \
	fi
	@$(MAKE) --no-print-directory _garage-bootstrap
	@echo
	@cat $(GARAGE_ENV)
	@echo
	@echo "Garage is up. Either:"
	@echo "  eval \"\$$(cat $(GARAGE_ENV))\"  &&  go test -tags integration ./..."
	@echo "or:"
	@echo "  make test-integration"

.PHONY: garage-down
garage-down:
	@if [ ! -f $(GARAGE_PID) ]; then echo "garage is not running"; exit 0; fi
	@PID=$$(cat $(GARAGE_PID)); \
	if kill -0 $$PID 2>/dev/null; then \
		echo "stopping garage (PID $$PID)"; \
		kill $$PID; \
		for i in 1 2 3 4 5 6 7 8 9 10; do kill -0 $$PID 2>/dev/null || break; sleep 1; done; \
		kill -0 $$PID 2>/dev/null && kill -9 $$PID || true; \
	else \
		echo "garage PID $$PID is not running"; \
	fi
	@rm -f $(GARAGE_PID)

.PHONY: garage-reset
garage-reset:
	@$(MAKE) --no-print-directory garage-down
	@rm -rf $(GARAGE_DIR)
	@$(MAKE) --no-print-directory garage-up

.PHONY: garage-status
garage-status:
	@if [ -f $(GARAGE_PID) ] && kill -0 "$$(cat $(GARAGE_PID))" 2>/dev/null; then \
		echo "running (PID $$(cat $(GARAGE_PID)))"; \
		[ -f $(GARAGE_ENV) ] && echo && cat $(GARAGE_ENV); \
	else \
		echo "not running"; \
		exit 1; \
	fi

# --- internals (not for direct use) ---

$(GARAGE_DIR):
	@mkdir -p $(GARAGE_DIR)/meta $(GARAGE_DIR)/data

$(GARAGE_CONFIG): | $(GARAGE_DIR)
	@command -v $(GARAGE_BIN) >/dev/null 2>&1 || { \
		echo "garage binary not found (set GARAGE_BIN or install: 'brew install garage')"; \
		exit 1; \
	}
	@command -v openssl >/dev/null 2>&1 || { echo "openssl required to generate secrets"; exit 1; }
	@RPC_SECRET=$$(openssl rand -hex 32); \
	ADMIN_TOKEN=$$(openssl rand -hex 32); \
	METRICS_TOKEN=$$(openssl rand -hex 32); \
	{ \
		echo 'metadata_dir = "$(GARAGE_DIR)/meta"'; \
		echo 'data_dir = "$(GARAGE_DIR)/data"'; \
		echo 'db_engine = "sqlite"'; \
		echo 'replication_factor = 1'; \
		echo 'rpc_bind_addr = "127.0.0.1:$(GARAGE_RPC_PORT)"'; \
		echo 'rpc_public_addr = "127.0.0.1:$(GARAGE_RPC_PORT)"'; \
		echo "rpc_secret = \"$$RPC_SECRET\""; \
		echo ''; \
		echo '[s3_api]'; \
		echo 's3_region = "garage"'; \
		echo 'api_bind_addr = "127.0.0.1:$(GARAGE_S3_PORT)"'; \
		echo 'root_domain = ".s3.garage.localhost"'; \
		echo ''; \
		echo '[admin]'; \
		echo 'api_bind_addr = "127.0.0.1:$(GARAGE_ADMIN_PORT)"'; \
		echo "admin_token = \"$$ADMIN_TOKEN\""; \
		echo "metrics_token = \"$$METRICS_TOKEN\""; \
	} > $(GARAGE_CONFIG)

.PHONY: _garage-wait-ready
_garage-wait-ready:
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do \
		if $(GARAGE_BIN) -c $(GARAGE_CONFIG) status >/dev/null 2>&1; then exit 0; fi; \
		sleep 0.5; \
	done; \
	echo "garage failed to come up; see $(GARAGE_LOG)"; \
	exit 1

.PHONY: _garage-bootstrap
_garage-bootstrap:
	@if ! $(GARAGE_BIN) -c $(GARAGE_CONFIG) layout show 2>/dev/null | grep -q '^Role:'; then \
		NODE_ID=$$($(GARAGE_BIN) -c $(GARAGE_CONFIG) status | awk '/^[0-9a-f]/{print $$1; exit}'); \
		if [ -z "$$NODE_ID" ]; then echo "could not determine garage node ID"; exit 1; fi; \
		$(GARAGE_BIN) -c $(GARAGE_CONFIG) layout assign $$NODE_ID -z dc1 -c 1G >/dev/null; \
		$(GARAGE_BIN) -c $(GARAGE_CONFIG) layout apply --version 1 >/dev/null; \
	fi
	@if ! $(GARAGE_BIN) -c $(GARAGE_CONFIG) key info $(GARAGE_KEY_NAME) >/dev/null 2>&1; then \
		$(GARAGE_BIN) -c $(GARAGE_CONFIG) key create $(GARAGE_KEY_NAME) >/dev/null; \
		$(GARAGE_BIN) -c $(GARAGE_CONFIG) key allow $(GARAGE_KEY_NAME) --create-bucket >/dev/null; \
	fi
	@KEY_INFO=$$($(GARAGE_BIN) -c $(GARAGE_CONFIG) key info $(GARAGE_KEY_NAME) --show-secret 2>/dev/null); \
	KEY_ID=$$(echo "$$KEY_INFO" | awk -F': *' '/^Key ID:/{print $$2}'); \
	SECRET=$$(echo "$$KEY_INFO" | awk -F': *' '/^Secret key:/{print $$2}'); \
	if [ -z "$$KEY_ID" ] || [ -z "$$SECRET" ]; then \
		echo "could not extract key from garage; output was:"; \
		echo "$$KEY_INFO"; \
		exit 1; \
	fi; \
	{ \
		echo "export GARAGE_S3_ENDPOINT=127.0.0.1:$(GARAGE_S3_PORT)"; \
		echo "export GARAGE_S3_REGION=garage"; \
		echo "export GARAGE_ACCESS_KEY=$$KEY_ID"; \
		echo "export GARAGE_SECRET_KEY=$$SECRET"; \
		echo "export GARAGE_KEY_NAME=$(GARAGE_KEY_NAME)"; \
	} > $(GARAGE_ENV)

# --- build & install ---

.PHONY: build
build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_BIN) ./cmd/soft

.PHONY: install-binary
install-binary: build
	@if [ "$$(id -u)" -ne 0 ]; then echo "install-binary must be run as root"; exit 1; fi
	@install -d $(dir $(SOFT_SERVE_BIN))
	install -m 0755 $(BUILD_BIN) $(SOFT_SERVE_BIN)
	@echo "installed $(SOFT_SERVE_BIN)"
	@if systemctl is-active --quiet soft-serve.service 2>/dev/null; then \
		echo "soft-serve.service is running; restart it to pick up the new binary:"; \
		echo "  sudo systemctl restart soft-serve.service"; \
	fi

# --- remote deploy ---

.PHONY: deploy
deploy:
	@test -n "$(DEPLOY_HOST)" || { echo "DEPLOY_HOST is required (e.g. make deploy DEPLOY_HOST=server.example.com)"; exit 1; }
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(BUILD_DIR)/soft-linux-amd64 ./cmd/soft
	scp $(BUILD_DIR)/soft-linux-amd64 $(DEPLOY_SSH):$(DEPLOY_BIN)
	ssh $(DEPLOY_SSH) 'sudo systemctl restart soft-serve.service'
	@echo "deployed and restarted soft-serve on $(DEPLOY_HOST)"

# --- systemd install ---
#
# Assumes the `soft` binary is already in place at $(SOFT_SERVE_BIN). Override
# any of the install variables on the command line, e.g.:
#   sudo make install-systemd SOFT_SERVE_BIN=/usr/bin/soft

.PHONY: install-systemd
install-systemd:
	@if [ "$$(id -u)" -ne 0 ]; then echo "install-systemd must be run as root"; exit 1; fi
	@if [ ! -x "$(SOFT_SERVE_BIN)" ]; then \
		echo "binary not found or not executable at $(SOFT_SERVE_BIN)"; \
		echo "install it first or override SOFT_SERVE_BIN=/path/to/soft"; \
		exit 1; \
	fi
	@if ! getent group $(SOFT_SERVE_GROUP) >/dev/null; then \
		echo "creating group $(SOFT_SERVE_GROUP)"; \
		groupadd --system $(SOFT_SERVE_GROUP); \
	fi
	@if ! id -u $(SOFT_SERVE_USER) >/dev/null 2>&1; then \
		echo "creating user $(SOFT_SERVE_USER)"; \
		useradd --system --gid $(SOFT_SERVE_GROUP) \
			--home-dir $(SOFT_SERVE_HOME) --shell /usr/sbin/nologin \
			$(SOFT_SERVE_USER); \
	fi
	@install -d -o $(SOFT_SERVE_USER) -g $(SOFT_SERVE_GROUP) -m 0750 $(SOFT_SERVE_HOME)
	@install -m 0644 deploy/soft-serve.service $(SYSTEMD_DIR)/soft-serve.service
	@if [ ! -e /etc/soft-serve.conf ]; then \
		install -m 0644 deploy/soft-serve.conf /etc/soft-serve.conf; \
	else \
		echo "/etc/soft-serve.conf already exists, leaving it alone"; \
	fi
	@systemctl daemon-reload
	@echo
	@echo "soft-serve.service installed."
	@echo "Enable and start with:"
	@echo "  sudo systemctl enable --now soft-serve.service"

.PHONY: uninstall-systemd
uninstall-systemd:
	@if [ "$$(id -u)" -ne 0 ]; then echo "uninstall-systemd must be run as root"; exit 1; fi
	-@systemctl disable --now soft-serve.service 2>/dev/null || true
	@rm -f $(SYSTEMD_DIR)/soft-serve.service
	@systemctl daemon-reload
	@echo "soft-serve.service removed."
	@echo "Data in $(SOFT_SERVE_HOME), the $(SOFT_SERVE_USER) user, and /etc/soft-serve.conf were left in place."
