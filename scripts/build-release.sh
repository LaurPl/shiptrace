#!/usr/bin/env bash
#
# Cross-compile every shiptrace binary for the four supported targets and
# package them into tarballs under dist/<version>/. Re-builds the React
# bundle first so the embedded //go:embed picks up the latest dashboard.
#
# Usage:
#   scripts/build-release.sh [version]
#
# If version is omitted, we read it from `git describe --tags`. Output:
#
#   dist/<version>/
#     shiptrace-<version>-darwin-arm64.tar.gz
#     shiptrace-<version>-darwin-amd64.tar.gz
#     shiptrace-<version>-linux-amd64.tar.gz
#     shiptrace-<version>-windows-amd64.zip
#     SHA256SUMS
#
# Each archive contains all three binaries (shiptrace, shiptrace-cc-hook,
# shiptrace-git-postcommit) so a single download lays down everything a
# new install needs.

set -euo pipefail

cd "$(dirname "$0")/.."
REPO_ROOT="$(pwd)"

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
DIST="$REPO_ROOT/dist/$VERSION"
mkdir -p "$DIST"

BINARIES=(shiptrace shiptrace-cc-hook shiptrace-git-postcommit)
# Targets formatted "os/arch" — keep the list short and obvious.
TARGETS=(
  "darwin/arm64"
  "darwin/amd64"
  "linux/amd64"
  "windows/amd64"
)

# Build the React bundle first so all per-platform builds embed the same
# version. Skip the install step if node_modules is already present.
echo "▸ building React bundle"
pushd web > /dev/null
if [ ! -d node_modules ]; then
  npm install --no-audit --no-fund
fi
npm run build
popd > /dev/null
echo

# Sanity-check that the embed target now has a real index.html.
if [ ! -f "$REPO_ROOT/cmd/shiptrace/web/dist/index.html" ]; then
  echo "✗ web bundle did not land at cmd/shiptrace/web/dist/index.html" >&2
  exit 1
fi

LDFLAGS="-s -w -X github.com/LaurPl/shiptrace/internal/cli.version=$VERSION"

for target in "${TARGETS[@]}"; do
  GOOS="${target%/*}"
  GOARCH="${target#*/}"
  ARCHIVE_BASE="shiptrace-${VERSION}-${GOOS}-${GOARCH}"
  STAGE="$DIST/.stage-${GOOS}-${GOARCH}"
  rm -rf "$STAGE"
  mkdir -p "$STAGE"

  echo "▸ building ${GOOS}/${GOARCH}"
  for bin in "${BINARIES[@]}"; do
    OUT="$STAGE/$bin"
    if [ "$GOOS" = "windows" ]; then
      OUT="${OUT}.exe"
    fi
    GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 \
      go build -trimpath -ldflags "$LDFLAGS" -o "$OUT" "./cmd/$bin"
  done

  # Throw in the install script and a tiny manifest so users can verify
  # what they're untarring before running anything.
  cp "$REPO_ROOT/scripts/install.sh" "$STAGE/install.sh"
  cat > "$STAGE/MANIFEST.txt" <<EOF
shiptrace $VERSION
target:   $GOOS/$GOARCH
binaries: ${BINARIES[*]}
install:  see install.sh or copy binaries to a directory on \$PATH
EOF

  if [ "$GOOS" = "windows" ]; then
    # `zip ... .` does nothing without -r; list each entry explicitly so
    # we don't accidentally pick up dotfiles or staging cruft.
    (cd "$STAGE" && zip -qj "$DIST/${ARCHIVE_BASE}.zip" ./*.exe install.sh MANIFEST.txt)
  else
    tar -C "$STAGE" -czf "$DIST/${ARCHIVE_BASE}.tar.gz" .
  fi
  rm -rf "$STAGE"
done

echo
echo "▸ writing SHA256SUMS"
(
  cd "$DIST"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 ./*.tar.gz ./*.zip 2>/dev/null > SHA256SUMS || true
  else
    sha256sum ./*.tar.gz ./*.zip 2>/dev/null > SHA256SUMS || true
  fi
)

echo
echo "✓ release built — dist/$VERSION/"
ls -lh "$DIST" | awk 'NR>1 {print "  " $9 "  " $5}'

# Vite's emptyOutDir cleared the //go:embed placeholder; restore it so a
# fresh `go build` after a clone (without `npm run build` first) doesn't
# fail when the real bundle isn't present.
PLACEHOLDER="$REPO_ROOT/cmd/shiptrace/web/dist/.placeholder"
if [ ! -f "$PLACEHOLDER" ]; then
  echo "// placeholder so //go:embed all:web/dist has something to embed when the React bundle hasn't been built yet" > "$PLACEHOLDER"
fi
