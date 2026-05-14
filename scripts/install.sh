#!/usr/bin/env sh
#
# shiptrace installer. POSIX shell; no bash-isms.
#
# Default behavior: download the latest release tarball for the current
# (os, arch) from GitHub and place the three binaries on PATH.
#
# Dev mode: set SHIPTRACE_LOCAL_DIST to a local dist/<version>/ directory
# and the installer copies binaries from there instead of hitting the
# network. Useful for testing the installer itself before cutting a real
# release.
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/LaurPl/shiptrace/main/scripts/install.sh | sh
#   SHIPTRACE_VERSION=v0.0.6 sh install.sh
#   SHIPTRACE_LOCAL_DIST=/path/to/dist/v0.0.6 sh install.sh
#
# Flags (env vars, since this is POSIX sh):
#   SHIPTRACE_VERSION   — version to download (default: latest)
#   SHIPTRACE_INSTALL_DIR — install dir (default: ~/.local/bin or /usr/local/bin)
#   SHIPTRACE_LOCAL_DIST — install from a local dist tree (dev mode)
#   SHIPTRACE_RUN_INIT=1 — run `shiptrace init` after install

set -eu

REPO="LaurPl/shiptrace"
VERSION="${SHIPTRACE_VERSION:-latest}"
LOCAL_DIST="${SHIPTRACE_LOCAL_DIST:-}"
RUN_INIT="${SHIPTRACE_RUN_INIT:-0}"

color() { printf "%s" "$1"; }
green="$(printf '\033[32m')"
yellow="$(printf '\033[33m')"
red="$(printf '\033[31m')"
dim="$(printf '\033[2m')"
reset="$(printf '\033[0m')"

ok()    { printf "%s✓%s %s\n" "$green" "$reset" "$1"; }
warn()  { printf "%s⚠%s %s\n" "$yellow" "$reset" "$1"; }
fail()  { printf "%s✗%s %s\n" "$red" "$reset" "$1" >&2; exit 1; }
hint()  { printf "%s  %s%s\n" "$dim" "$1" "$reset"; }

detect_os() {
  case "$(uname -s)" in
    Darwin) echo darwin ;;
    Linux) echo linux ;;
    MINGW*|MSYS*|CYGWIN*) echo windows ;;
    *) fail "unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    arm64|aarch64) echo arm64 ;;
    x86_64|amd64) echo amd64 ;;
    *) fail "unsupported arch: $(uname -m). Builds are only published for arm64/amd64." ;;
  esac
}

pick_install_dir() {
  if [ -n "${SHIPTRACE_INSTALL_DIR:-}" ]; then
    echo "$SHIPTRACE_INSTALL_DIR"
    return
  fi
  # Prefer ~/.local/bin when it exists or when $PATH already contains it.
  case ":$PATH:" in
    *":$HOME/.local/bin:"*) echo "$HOME/.local/bin"; return ;;
  esac
  if [ -d "$HOME/.local/bin" ]; then
    echo "$HOME/.local/bin"
    return
  fi
  echo "/usr/local/bin"
}

ensure_writable() {
  dir="$1"
  if [ -w "$dir" ]; then return 0; fi
  if [ ! -d "$dir" ]; then
    mkdir -p "$dir" 2>/dev/null && return 0
  fi
  # If we got here, we'd need elevation. Fall back to ~/.local/bin if
  # possible; otherwise tell the user and exit.
  if [ "$dir" != "$HOME/.local/bin" ]; then
    fallback="$HOME/.local/bin"
    warn "$dir not writable; falling back to $fallback"
    mkdir -p "$fallback"
    echo "$fallback"
    return
  fi
  fail "install dir $dir is not writable. Try: SHIPTRACE_INSTALL_DIR=/some/writable/dir sh install.sh"
}

ensure_on_path() {
  dir="$1"
  case ":$PATH:" in
    *":$dir:"*) return 0 ;;
  esac
  warn "$dir is not on \$PATH"
  hint "add this to your shell rc to fix:"
  hint "  export PATH=\"$dir:\$PATH\""
}

resolve_version() {
  if [ "$VERSION" != "latest" ]; then
    echo "$VERSION"
    return
  fi
  api="https://api.github.com/repos/$REPO/releases/latest"
  if command -v curl >/dev/null 2>&1; then
    tag=$(curl -fsSL "$api" 2>/dev/null \
      | grep '"tag_name":' | head -1 \
      | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  elif command -v wget >/dev/null 2>&1; then
    tag=$(wget -qO- "$api" \
      | grep '"tag_name":' | head -1 \
      | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  else
    fail "need curl or wget to resolve latest version"
  fi
  if [ -z "$tag" ]; then
    fail "could not resolve latest release tag from $api"
  fi
  echo "$tag"
}

download() {
  url="$1"; out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --proto '=https' "$url" -o "$out"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
  else
    fail "need curl or wget to download $url"
  fi
}

install_from_local() {
  src="$1"; dest="$2"; os="$3"; arch="$4"
  archive=""
  for cand in "$src"/shiptrace-*-"${os}-${arch}".tar.gz "$src"/shiptrace-*-"${os}-${arch}".zip; do
    if [ -f "$cand" ]; then archive="$cand"; break; fi
  done
  if [ -z "$archive" ]; then
    fail "no archive matching ${os}-${arch} under $src"
  fi
  extract_and_place "$archive" "$dest" "$os"
}

install_from_release() {
  os="$1"; arch="$2"; dest="$3"
  resolved="$(resolve_version)"
  ext="tar.gz"
  [ "$os" = "windows" ] && ext="zip"
  archive_name="shiptrace-${resolved}-${os}-${arch}.${ext}"
  url="https://github.com/$REPO/releases/download/${resolved}/${archive_name}"
  ok "downloading $url"
  tmp="$(mktemp -d 2>/dev/null || mktemp -d -t shiptrace-install)"
  trap 'rm -rf "$tmp"' EXIT
  download "$url" "$tmp/$archive_name"
  extract_and_place "$tmp/$archive_name" "$dest" "$os"
}

extract_and_place() {
  archive="$1"; dest="$2"; os="$3"
  staging="$(dirname "$archive")/_extract"
  mkdir -p "$staging"
  case "$archive" in
    *.tar.gz|*.tgz) tar -C "$staging" -xzf "$archive" ;;
    *.zip)
      if command -v unzip >/dev/null 2>&1; then
        unzip -q -o "$archive" -d "$staging"
      else
        fail "need unzip to extract $archive"
      fi
      ;;
    *) fail "unknown archive format: $archive" ;;
  esac
  ext=""
  [ "$os" = "windows" ] && ext=".exe"
  for bin in shiptrace shiptrace-cc-hook shiptrace-git-postcommit; do
    src_file="$staging/${bin}${ext}"
    if [ ! -f "$src_file" ]; then
      fail "expected $src_file inside archive — got these instead: $(ls "$staging" | tr '\n' ' ')"
    fi
    mv "$src_file" "$dest/${bin}${ext}"
    chmod +x "$dest/${bin}${ext}"
    ok "installed $dest/${bin}${ext}"
  done
}

main() {
  os="$(detect_os)"
  arch="$(detect_arch)"
  dest_initial="$(pick_install_dir)"
  dest="$(ensure_writable "$dest_initial")"
  if [ -z "$dest" ]; then dest="$dest_initial"; fi
  ok "platform:    $os/$arch"
  ok "install dir: $dest"

  if [ -n "$LOCAL_DIST" ]; then
    ok "source:      local dist ($LOCAL_DIST)"
    install_from_local "$LOCAL_DIST" "$dest" "$os" "$arch"
  else
    ok "source:      github releases"
    install_from_release "$os" "$arch" "$dest"
  fi

  ensure_on_path "$dest"

  if [ -x "$dest/shiptrace" ]; then
    printf "\n%sshiptrace installed.%s try:\n" "$green" "$reset"
    hint "  shiptrace --version"
    hint "  shiptrace init      # wire Claude Code hooks"
    hint "  shiptrace doctor    # verify"
    hint "  shiptrace serve     # local dashboard"
  fi

  if [ "$RUN_INIT" = "1" ] && [ -x "$dest/shiptrace" ]; then
    echo
    ok "running shiptrace init"
    "$dest/shiptrace" init --yes || warn "init returned non-zero"
  fi
}

main "$@"
