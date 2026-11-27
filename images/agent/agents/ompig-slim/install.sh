#!/usr/bin/env bash
# Installs ompig-slim $VERSION with its release installer, which checks the
# release checksum.
set -euo pipefail
curl -fsSL https://raw.githubusercontent.com/tmih06/ompig-slim/main/scripts/install.sh |
  OMPIG_SLIM_INSTALL_DIR=/usr/local/bin bash -s -- --version "v${VERSION}" --dir /usr/local/bin
