#!/usr/bin/env bash
set -Eeuo pipefail
SERVICE_LABEL="dev.nimweo.pulse-agent"
BINARY_DESTINATION="/usr/local/bin/pulse-agent"
CONFIG_DIRECTORY="/Library/Application Support/Nimweo/Pulse Agent"
CONFIG_DESTINATION="${CONFIG_DIRECTORY}/config.yaml"
PLIST_DESTINATION="/Library/LaunchDaemons/${SERVICE_LABEL}.plist"
GITHUB_URL="https://github.com/Nimweo/pulse-agent"
BASE_URL="${PULSE_BASE_URL:-https://pulse.nimweo.dev/api/v1/}"
API_KEY="${PULSE_API_KEY:-}"
VERSION="latest"
OVERWRITE_CONFIG=false
fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }
while (($#)); do
  case "$1" in
    --api-key) [[ -n "${2:-}" ]] || fail "$1 requires a value"; API_KEY="$2"; shift 2;;
    --base-url) [[ -n "${2:-}" ]] || fail "$1 requires a value"; BASE_URL="$2"; shift 2;;
    --version) [[ -n "${2:-}" ]] || fail "$1 requires a value"; VERSION="$2"; shift 2;;
    --overwrite-config) OVERWRITE_CONFIG=true; shift;;
    -h|--help) printf '%s\n' 'Install Pulse Agent as a launchd service on macOS.' 'Usage: sudo ./install-macos.sh [--api-key VALUE] [--base-url URL] [--version VERSION] [--overwrite-config]'; exit 0;;
    *) fail "unknown option: $1";;
  esac
done
[[ "$(uname -s)" == Darwin ]] || fail 'this installer supports macOS only'
(( EUID == 0 )) || fail 'run this installer as root (for example, with sudo)'
command -v curl >/dev/null || fail 'required command not found: curl'
command -v shasum >/dev/null || fail 'required command not found: shasum'
[[ "$BASE_URL" =~ ^https?://[^[:space:]]+$ ]] || fail 'base URL must be an absolute HTTP or HTTPS URL'
case "$(uname -m)" in x86_64|amd64) ARCH=amd64;; arm64|aarch64) ARCH=arm64;; *) fail "unsupported CPU architecture: $(uname -m)";; esac
if [[ "$VERSION" == latest ]]; then latest_url="$(curl --fail --silent --show-error --location --output /dev/null --write-out '%{url_effective}' "${GITHUB_URL}/releases/latest")"; TAG="v${latest_url##*/}"; else VERSION="${VERSION#v}"; [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'version must use MAJOR.MINOR.PATCH'; TAG="v${VERSION}"; fi
VERSION="${TAG#v}"; PACKAGE="pulse-agent_${VERSION}_darwin_${ARCH}"; TEMP="$(mktemp -d)"; trap 'rm -rf -- "$TEMP"' EXIT
RELEASE_URL="${GITHUB_URL}/releases/download/${TAG}"
curl --fail --silent --show-error --location --output "$TEMP/$PACKAGE.tar.gz" "$RELEASE_URL/$PACKAGE.tar.gz"
curl --fail --silent --show-error --location --output "$TEMP/checksums.txt" "$RELEASE_URL/checksums.txt"
expected="$(awk -v f="$PACKAGE.tar.gz" '$2 == f {print $1}' "$TEMP/checksums.txt")"; actual="$(shasum -a 256 "$TEMP/$PACKAGE.tar.gz" | awk '{print $1}')"; [[ "$expected" == "$actual" ]] || fail 'checksum verification failed'
tar -xzf "$TEMP/$PACKAGE.tar.gz" -C "$TEMP"; SOURCE="$TEMP/$PACKAGE"; [[ -f "$SOURCE/pulse-agent" && -f "$SOURCE/config.example.yaml" ]] || fail 'release archive is incomplete'
mkdir -p "$CONFIG_DIRECTORY" "$(dirname "$BINARY_DESTINATION")"; if [[ "$OVERWRITE_CONFIG" == true || ! -f "$CONFIG_DESTINATION" ]]; then cp "$SOURCE/config.example.yaml" "$CONFIG_DESTINATION"; fi
if [[ -n "$API_KEY" ]]; then sed -i '' -E "s|^  api_key:.*|  api_key: \"${API_KEY}\"|" "$CONFIG_DESTINATION"; fi
if [[ -n "$BASE_URL" ]]; then sed -i '' -E "s|^  base_url:.*|  base_url: \"${BASE_URL}\"|" "$CONFIG_DESTINATION"; fi
cp "$SOURCE/pulse-agent" "$BINARY_DESTINATION"; chmod 0755 "$BINARY_DESTINATION"
cat > "$PLIST_DESTINATION" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>${SERVICE_LABEL}</string><key>ProgramArguments</key><array><string>${BINARY_DESTINATION}</string><string>--config</string><string>${CONFIG_DESTINATION}</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>StandardOutPath</key><string>/var/log/pulse-agent.log</string><key>StandardErrorPath</key><string>/var/log/pulse-agent.error.log</string></dict></plist>
EOF
chown root:wheel "$PLIST_DESTINATION"; chmod 0644 "$PLIST_DESTINATION"; launchctl bootout system "$PLIST_DESTINATION" 2>/dev/null || true
if grep -Eq '^configured:[[:space:]]*true[[:space:]]*$' "$CONFIG_DESTINATION"; then launchctl bootstrap system "$PLIST_DESTINATION"; printf 'Pulse Agent %s installed and started successfully.\n' "$VERSION"; else printf 'Pulse Agent %s installed; set configured: true before starting the service.\n' "$VERSION"; fi
printf 'Configuration: %s\n' "$CONFIG_DESTINATION"
