.PHONY: default desktop engine relay relay-local ios ios-check ios-test desktop-test engine-test test test-all clean check-file-sizes check-contracts check-status-writers claude-symlinks hooks

# Homebrew installs node/npm under /opt/homebrew/bin on Apple Silicon.
# Make runs recipes with /bin/sh which only has /usr/bin:/bin in PATH,
# so node/npm are not found unless we add the Homebrew prefix here.
# The export propagates to every recipe in this Makefile.
export PATH := /opt/homebrew/bin:$(PATH)

default: engine

engine:
	@cd engine && bash commands/install.command --standalone || { echo "❌ Engine build failed"; exit 1; }

desktop:
	@cd desktop && bash commands/install-bg.command

relay:
	@cd relay && docker build --platform linux/amd64 -t ion-relay:latest .

relay-local:
	@cd relay && go run .

# iPhone UDID: BB98AF19-BD09-5432-89B6-054CC2C02847
# If Apple Watch is also connected and detection picks it up, pass --device explicitly:
#   make ios DEVICE=BB98AF19-BD09-5432-89B6-054CC2C02847
# (install.command xctrace fallback now filters Watch entries, but --device is the sure fix)
ios:
	@cd ios && bash commands/install.command $(if $(DEVICE),--device $(DEVICE),)

ios-check:
	@cd ios && xcodebuild -project IonRemote.xcodeproj -scheme IonRemote \
		-destination 'generic/platform=iOS' build 2>&1 | grep -E "error:|BUILD"

# Run the IonRemoteTests unit-test bundle on a real iOS Simulator. Picks the
# newest available simulator automatically; override with the
# IOS_TEST_DESTINATION env var (see scripts/run-ios-tests.sh for format).
ios-test:
	@bash scripts/run-ios-tests.sh

# Per-component test convenience wrappers. The CI workflows already exercise
# each surface in isolation; these mirror what they do so contributors can
# run a focused check locally without remembering each toolchain's command.
engine-test:
	@cd engine && go test -race ./...

desktop-test:
	@cd desktop && npm test

test:
	@cd engine && go test ./...
	@cd desktop && npm test 2>/dev/null || true

# Run every test surface end-to-end before merging. Stops at the first
# failure so you don't waste minutes on a downstream failure that's really
# caused by an earlier component.
test-all: check-file-sizes check-contracts check-status-writers engine-test desktop-test ios-test
	@echo "✅ test-all: all surfaces green"

clean:
	@cd engine && rm -rf bin/ dist/
	@cd desktop && rm -rf dist/ out/

# File-architecture guardrails (see docs/architecture/file-organization.md)
check-file-sizes:
	@bash scripts/check-file-sizes.sh

# Phase 4 of the state-management overhaul. Prohibits new direct writes
# to tab.status / inst.statusFields outside the dispatcher chokepoints
# whitelisted in scripts/check-status-writers.sh.
check-status-writers:
	@bash scripts/check-status-writers.sh

# Cross-language contract drift detection.
# Asserts the Go-generated contracts.json is up to date; TS and Swift tests
# validate against it via their own test suites (npm test / xcodebuild test).
check-contracts:
	@cd engine && go test ./internal/types/ -run TestContractManifest

# Create CLAUDE.md symlinks pointing at sibling AGENTS.md files. Idempotent.
# CLAUDE.md is gitignored; AGENTS.md is committed as the canonical context file.
claude-symlinks:
	@bash scripts/setup-claude-symlinks.sh

# Point this clone's git hooks at the tracked .githooks/ directory so the
# pre-push file-size check runs before pushes hit CI. One-time per clone.
hooks:
	@git config core.hooksPath .githooks
	@echo "core.hooksPath -> .githooks"

# Local pipeline testing (requires: brew install act)
test-pipeline-dry:
	act workflow_dispatch -W .github/workflows/build.yml \
		--input release_report="$$(cat .act/release-report.json)" \
		--dryrun

test-pipeline-engine:
	act workflow_dispatch -W .github/workflows/build.yml \
		-j build-engine \
		--input release_report="$$(cat .act/release-report.json)"

test-pipeline-relay:
	act workflow_dispatch -W .github/workflows/build.yml \
		-j build-relay \
		--input release_report="$$(cat .act/release-report.json)"
