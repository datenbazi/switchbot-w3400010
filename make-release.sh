#!/usr/bin/env bash
set -euo pipefail

PROJ_DIR="$(cd "$(dirname "$0")" && pwd)"

# Parse args: first non-flag arg is version, --publish triggers GH release
VERSION=""
PUBLISH=0
for arg in "$@"; do
  case "$arg" in
    --publish) PUBLISH=1 ;;
    *)         [[ -z "$VERSION" ]] && VERSION="$arg" ;;
  esac
done
VERSION="${VERSION:-$(date +%Y%m%d)}"

PKG_NAME="switchbot-temp-${VERSION}"
STAGE_DIR="${PROJ_DIR}/_release/${PKG_NAME}"
INSTALL_DIR="/opt/switchbot-temp"

echo "==> Building binaries..."
cd "$PROJ_DIR"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o switchbot-temp .
echo "    amd64:  $(du -sh switchbot-temp | cut -f1)"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o switchbot-temp-armv7l .
echo "    armv7l: $(du -sh switchbot-temp-armv7l | cut -f1)"

echo "==> Staging release into _release/${PKG_NAME}/"
rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR/ui"

# Binaries — kept separate, not bundled together
cp switchbot-temp "$STAGE_DIR/"
cp switchbot-temp-armv7l "$STAGE_DIR/"

# UI
cp ui/index.html "$STAGE_DIR/ui/"

# Alerts template (strip any personal GSH keys if present)
cp alerts.json "$STAGE_DIR/alerts.json.example"

# Service file with install-dir paths substituted
sed "s|/home/andy/data/SwitchBotReverse|${INSTALL_DIR}|g" \
    switchbot-temp.service > "$STAGE_DIR/switchbot-temp.service"

# Install script
cat > "$STAGE_DIR/install.sh" <<'INSTALL'
#!/usr/bin/env bash
set -euo pipefail
INSTALL_DIR="/opt/switchbot-temp"
SERVICE="switchbot-temp"

if [ "$(id -u)" -ne 0 ]; then
    echo "Run as root (sudo ./install.sh)" >&2
    exit 1
fi

# Pick the right binary for this machine; fall back to plain name (arch-specific package)
ARCH="$(uname -m)"
case "$ARCH" in
    armv7l|armv6l) BIN="switchbot-temp-armv7l" ;;
    *)             BIN="switchbot-temp" ;;
esac
[ -f "$BIN" ] || BIN="switchbot-temp"
echo "==> Detected arch: ${ARCH} -> using ${BIN}"

echo "==> Installing to ${INSTALL_DIR}..."
mkdir -p "${INSTALL_DIR}/ui"

cp "${BIN}" "${INSTALL_DIR}/switchbot-temp"
chmod 755 "${INSTALL_DIR}/switchbot-temp"
cp ui/index.html "${INSTALL_DIR}/ui/"

if [ ! -f "${INSTALL_DIR}/alerts.json" ]; then
    cp alerts.json.example "${INSTALL_DIR}/alerts.json"
    echo "    Copied alerts.json.example -> alerts.json (edit before starting)"
fi

echo "==> Installing systemd service..."
cp switchbot-temp.service "/etc/systemd/system/${SERVICE}.service"
systemctl daemon-reload
systemctl enable "${SERVICE}"

echo ""
echo "Done. Edit ${INSTALL_DIR}/alerts.json, then:"
echo "  sudo systemctl start ${SERVICE}"
echo "  sudo journalctl -fu ${SERVICE}"
INSTALL
chmod +x "$STAGE_DIR/install.sh"

# Tarballs — one per architecture
echo "==> Packing tarballs..."
cd "${PROJ_DIR}/_release"

# amd64
tar czf "${PKG_NAME}-linux-amd64.tar.gz" \
    --transform "s|${PKG_NAME}/||" \
    "${PKG_NAME}/switchbot-temp" \
    "${PKG_NAME}/ui" \
    "${PKG_NAME}/alerts.json.example" \
    "${PKG_NAME}/switchbot-temp.service" \
    "${PKG_NAME}/install.sh"

# armv7l
tar czf "${PKG_NAME}-linux-armv7l.tar.gz" \
    --transform "s|${PKG_NAME}/switchbot-temp-armv7l|${PKG_NAME}/switchbot-temp|" \
    --transform "s|${PKG_NAME}/||" \
    "${PKG_NAME}/switchbot-temp-armv7l" \
    "${PKG_NAME}/ui" \
    "${PKG_NAME}/alerts.json.example" \
    "${PKG_NAME}/switchbot-temp.service" \
    "${PKG_NAME}/install.sh"

echo ""
for f in "${PKG_NAME}-linux-amd64.tar.gz" "${PKG_NAME}-linux-armv7l.tar.gz"; do
    echo "  $(du -sh "$f" | cut -f1)  $f"
    echo "         sha256: $(sha256sum "$f" | cut -d' ' -f1)"
done

# ── GitHub release ────────────────────────────────────────────
if [[ "$PUBLISH" -eq 1 ]]; then
  cd "$PROJ_DIR"
  echo ""
  echo "==> Tagging ${VERSION}..."
  git tag -a "${VERSION}" -m "Release ${VERSION}"
  git push origin "${VERSION}"

  echo "==> Creating GitHub release ${VERSION}..."
  gh release create "${VERSION}" \
    "_release/${PKG_NAME}-linux-amd64.tar.gz" \
    "_release/${PKG_NAME}-linux-armv7l.tar.gz" \
    --title "${VERSION}" \
    --generate-notes
  echo ""
  echo "Release published:"
  gh release view "${VERSION}" --json url -q .url
fi
