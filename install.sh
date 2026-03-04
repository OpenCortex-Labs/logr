#!/usr/bin/env sh
# install.sh — Install logr binary from GitHub Releases
# Usage: curl -sf https://raw.githubusercontent.com/Mihir99-mk/logr/main/install.sh | sh

set -e

REPO="Mihir99-mk/logr"
BINARY="logr"
INSTALL_DIR="/usr/local/bin"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

info()  { printf "  \033[34minfo\033[0m  %s\n" "$1"; }
ok()    { printf "  \033[32m  ok\033[0m  %s\n" "$1"; }
err()   { printf "  \033[31merror\033[0m %s\n" "$1" >&2; exit 1; }

need_cmd() {
  if ! command -v "$1" > /dev/null 2>&1; then
    err "Required command not found: $1"
  fi
}

# ---------------------------------------------------------------------------
# Detect OS and architecture
# ---------------------------------------------------------------------------

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "Linux" ;;
    Darwin*) echo "Darwin" ;;
    *)       err "Unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)   echo "x86_64" ;;
    arm64|aarch64)  echo "arm64" ;;
    *)              err "Unsupported architecture: $(uname -m)" ;;
  esac
}

# ---------------------------------------------------------------------------
# Resolve latest release tag
# ---------------------------------------------------------------------------

latest_tag() {
  need_cmd curl
  curl -sf "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
  need_cmd curl
  need_cmd tar

  OS=$(detect_os)
  ARCH=$(detect_arch)

  TAG="${LOGR_VERSION:-$(latest_tag)}"
  if [ -z "$TAG" ]; then
    err "Could not determine latest release tag. Set LOGR_VERSION to override."
  fi

  # Strip leading 'v' for the archive filename (GoReleaser convention)
  VERSION="${TAG#v}"
  ARCHIVE="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
  URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"

  info "Detected OS:   ${OS}"
  info "Detected arch: ${ARCH}"
  info "Installing:    ${BINARY} ${TAG}"
  info "Source:        ${URL}"

  TMP=$(mktemp -d)
  trap 'rm -rf "$TMP"' EXIT

  info "Downloading archive..."
  curl -sfL "$URL" -o "${TMP}/${ARCHIVE}" || err "Download failed: ${URL}"

  info "Extracting..."
  tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP"

  if [ ! -f "${TMP}/${BINARY}" ]; then
    err "Binary '${BINARY}' not found in archive."
  fi

  info "Installing to ${INSTALL_DIR}/${BINARY}..."
  if [ -w "$INSTALL_DIR" ]; then
    mv "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
    chmod +x "${INSTALL_DIR}/${BINARY}"
  else
    sudo mv "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
    sudo chmod +x "${INSTALL_DIR}/${BINARY}"
  fi

  ok "${BINARY} ${TAG} installed successfully."
  ok "Run '${BINARY} --help' to get started."
}

main "$@"
