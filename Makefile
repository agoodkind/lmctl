# Lint is centralized in go-makefile. Do NOT define project-local lint,
# deadcode, audit, fmt, vet, or staticcheck targets here. They duplicate
# the central pipeline and let agents bypass strict rules. Run `make help`
# for the canonical entry points (build/check/lint/fmt) and per-linter
# sub-targets (lint-golangci, lint-format, lint-gocyclo, lint-deadcode,
# staticcheck-extra). Refresh baselines via the matching *-baseline target.
#
# lmctl Makefile.
# Library-mode: build/install are no-ops. Lint/test/vet from go.mk apply.
# Pipeline lives in go-makefile and is fetched at runtime.

# Library mode: no binary. Consumers stamp the version package via ldflags.
LIBRARY := 1

# Pipeline modules (fetched + included by go.mk)
GO_MK_MODULES := go-build.mk

include bootstrap.mk

.DEFAULT_GOAL := check
