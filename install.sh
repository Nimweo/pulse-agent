#!/usr/bin/env bash

set -Eeuo pipefail

readonly SERVICE_NAME="pulse-agent.service"
readonly UPDATE_SERVICE_NAME="pulse-agent-update.service"
readonly UPDATE_TIMER_NAME="pulse-agent-update.timer"
readonly SERVICE_USER="pulse-agent"
readonly SERVICE_GROUP="pulse-agent"
readonly BINARY_DESTINATION="/usr/local/bin/pulse-agent"
readonly CONFIG_DIRECTORY="/etc/nimweo/pulse-agent"
readonly CONFIG_DESTINATION="${CONFIG_DIRECTORY}/config.yaml"
readonly SERVICE_DESTINATION="/etc/systemd/system/${SERVICE_NAME}"
readonly UPDATE_SERVICE_DESTINATION="/etc/systemd/system/${UPDATE_SERVICE_NAME}"
readonly UPDATE_TIMER_DESTINATION="/etc/systemd/system/${UPDATE_TIMER_NAME}"
readonly UPDATE_STATE_DIRECTORY="/var/lib/pulse-agent-updater"
readonly DEFAULT_BASE_URL="https://pulse.nimweo.dev/api/v1/"
readonly GITHUB_REPOSITORY="Nimweo/pulse-agent"
readonly GITHUB_URL="https://github.com/${GITHUB_REPOSITORY}"

BINARY_SOURCE=""
CONFIG_EXAMPLE=""
API_KEY="${PULSE_API_KEY:-}"
BASE_URL="${PULSE_BASE_URL:-${DEFAULT_BASE_URL}}"
REQUESTED_VERSION="latest"
API_KEY_SET=false
BASE_URL_SET=false
OVERWRITE_CONFIG=false
TEMPORARY_CONFIG=""
TEMPORARY_SERVICE=""
TEMPORARY_UPDATE_SERVICE=""
TEMPORARY_UPDATE_TIMER=""
DOWNLOAD_DIRECTORY=""

if [[ -n "${PULSE_API_KEY+x}" ]]; then
	API_KEY_SET=true
fi
if [[ -n "${PULSE_BASE_URL+x}" ]]; then
	BASE_URL_SET=true
fi

usage() {
	cat <<'EOF'
Install Pulse Agent as a systemd service on Linux.

Usage:
  sudo ./install.sh [options]

Options:
  --api-key VALUE          API key sent as a Bearer token (optional).
  --api-key-file PATH      Read the API key from a file.
  --base-url URL           API base URL (default: https://pulse.nimweo.dev/api/v1/).
  --version VERSION        Release version to install (default: latest).
  --overwrite-config       Replace an existing configuration from the template.
  -h, --help               Show this help message.

Environment variables:
  PULSE_API_KEY            Alternative to --api-key.
  PULSE_BASE_URL           Alternative to --base-url.

Passing the key through PULSE_API_KEY or --api-key-file avoids storing it in
the shell history. Command-line options take precedence over environment values.

Example:
  curl -fsSL https://raw.githubusercontent.com/Nimweo/pulse-agent/main/install.sh \
    | sudo bash -s -- --api-key-file /root/pulse-api-key
EOF
}

fail() {
	printf 'Error: %s\n' "$*" >&2
	exit 1
}

require_value() {
	local option="$1"
	local value="${2:-}"
	[[ -n "${value}" ]] || fail "${option} requires a value"
}

while (($# > 0)); do
	case "$1" in
		--api-key)
			require_value "$1" "${2:-}"
			API_KEY="$2"
			API_KEY_SET=true
			shift 2
			;;
		--api-key-file)
			require_value "$1" "${2:-}"
			[[ -r "$2" ]] || fail "API key file is not readable: $2"
			API_KEY="$(<"$2")"
			API_KEY_SET=true
			shift 2
			;;
		--base-url)
			require_value "$1" "${2:-}"
			BASE_URL="$2"
			BASE_URL_SET=true
			shift 2
			;;
		--version)
			require_value "$1" "${2:-}"
			REQUESTED_VERSION="$2"
			shift 2
			;;
		--overwrite-config)
			OVERWRITE_CONFIG=true
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			fail "unknown option: $1"
			;;
	esac
done

cleanup() {
	if [[ -n "${TEMPORARY_CONFIG}" ]]; then
		rm -f -- "${TEMPORARY_CONFIG}"
	fi
	if [[ -n "${TEMPORARY_SERVICE}" ]]; then
		rm -f -- "${TEMPORARY_SERVICE}"
	fi
	if [[ -n "${TEMPORARY_UPDATE_SERVICE}" ]]; then
		rm -f -- "${TEMPORARY_UPDATE_SERVICE}"
	fi
	if [[ -n "${TEMPORARY_UPDATE_TIMER}" ]]; then
		rm -f -- "${TEMPORARY_UPDATE_TIMER}"
	fi
	if [[ -n "${DOWNLOAD_DIRECTORY}" ]]; then
		rm -rf -- "${DOWNLOAD_DIRECTORY}"
	fi
}
trap cleanup EXIT

require_command() {
	command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

validate_input() {
	[[ "$(uname -s)" == "Linux" ]] || fail "this installer supports Linux only"
	((EUID == 0)) || fail "run this installer as root (for example, with sudo)"
	[[ -d /run/systemd/system ]] || fail "systemd is not running on this system"
	[[ "${BASE_URL}" =~ ^https?://[^[:space:]]+$ ]] || fail "base URL must be an absolute HTTP or HTTPS URL"
	[[ "${BASE_URL}" != *\?* && "${BASE_URL}" != *\#* ]] || fail "base URL must not contain a query or fragment"
	[[ "${API_KEY}" != *$'\n'* && "${API_KEY}" != *$'\r'* ]] || fail "API key must contain a single line"
}

resolve_architecture() {
	case "$(uname -m)" in
		x86_64 | amd64)
			printf 'amd64'
			;;
		aarch64 | arm64)
			printf 'arm64'
			;;
		*)
			fail "unsupported CPU architecture: $(uname -m)"
			;;
	esac
}

resolve_release_tag() {
	if [[ "${REQUESTED_VERSION}" != "latest" ]]; then
		local version="${REQUESTED_VERSION#v}"
		[[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "version must use the MAJOR.MINOR.PATCH format"
		printf 'v%s' "${version}"
		return
	fi

	local release_url
	release_url="$(curl \
		--proto '=https' \
		--tlsv1.2 \
		--fail \
		--silent \
		--show-error \
		--location \
		--output /dev/null \
		--write-out '%{url_effective}' \
		"${GITHUB_URL}/releases/latest")"

	release_url="${release_url%/}"
	local tag="${release_url##*/}"
	[[ "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "could not determine the latest release version"
	printf '%s' "${tag}"
}

download_release() {
	local architecture
	architecture="$(resolve_architecture)"

	local release_tag
	release_tag="$(resolve_release_tag)"
	local version="${release_tag#v}"
	local package_name="pulse-agent_${version}_linux_${architecture}"
	local archive_name="${package_name}.tar.gz"
	local release_url="${GITHUB_URL}/releases/download/${release_tag}"

	DOWNLOAD_DIRECTORY="$(mktemp -d)"
	local archive_path="${DOWNLOAD_DIRECTORY}/${archive_name}"
	local checksums_path="${DOWNLOAD_DIRECTORY}/checksums.txt"

	printf 'Downloading Pulse Agent %s for linux/%s...\n' "${version}" "${architecture}"
	curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
		--output "${archive_path}" "${release_url}/${archive_name}"
	curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
		--output "${checksums_path}" "${release_url}/checksums.txt"

	local expected_checksum
	expected_checksum="$(awk -v archive="${archive_name}" '$2 == archive { print $1 }' "${checksums_path}")"
	[[ "${expected_checksum}" =~ ^[[:xdigit:]]{64}$ ]] || fail "checksum not found for ${archive_name}"
	printf '%s  %s\n' "${expected_checksum}" "${archive_path}" | sha256sum --check --status - || {
		fail "checksum verification failed for ${archive_name}"
	}

	tar -xzf "${archive_path}" -C "${DOWNLOAD_DIRECTORY}"
	local package_directory="${DOWNLOAD_DIRECTORY}/${package_name}"
	BINARY_SOURCE="${package_directory}/pulse-agent"
	CONFIG_EXAMPLE="${package_directory}/config.example.yaml"
	[[ -f "${BINARY_SOURCE}" ]] || fail "release archive does not contain the Pulse Agent binary"
	[[ -f "${CONFIG_EXAMPLE}" ]] || fail "release archive does not contain config.example.yaml"
	chmod 0755 "${BINARY_SOURCE}"
}

yaml_escape() {
	local value="$1"
	value="${value//\\/\\\\}"
	value="${value//\"/\\\"}"
	value="${value//$'\t'/\\t}"
	printf '%s' "${value}"
}

sed_replacement_escape() {
	local value="$1"
	value="${value//\\/\\\\}"
	value="${value//&/\\&}"
	value="${value//|/\\|}"
	printf '%s' "${value}"
}

replace_yaml_value() {
	local file="$1"
	local key="$2"
	local value="$3"
	local escaped
	escaped="$(sed_replacement_escape "$(yaml_escape "${value}")")"

	grep -Eq "^[[:space:]]*${key}:" "${file}" || fail "missing ${key} field in configuration"
	sed -i -E "s|^([[:space:]]*${key}:).*$|\\1 \"${escaped}\"|" "${file}"
}

ensure_service_account() {
	if ! getent group "${SERVICE_GROUP}" >/dev/null; then
		groupadd --system "${SERVICE_GROUP}"
	fi

	if ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
		useradd \
			--system \
			--gid "${SERVICE_GROUP}" \
			--home-dir /var/lib/pulse-agent \
			--no-create-home \
			--shell /usr/sbin/nologin \
			"${SERVICE_USER}"
	fi

	local hardware_group
	for hardware_group in video render; do
		if getent group "${hardware_group}" >/dev/null; then
			usermod --append --groups "${hardware_group}" "${SERVICE_USER}"
		fi
	done

	install -d -o root -g root -m 0750 "${UPDATE_STATE_DIRECTORY}"
}

install_configuration() {
	install -d -o root -g "${SERVICE_GROUP}" -m 0750 "${CONFIG_DIRECTORY}"
	TEMPORARY_CONFIG="$(mktemp "${CONFIG_DIRECTORY}/config.yaml.XXXXXX")"

	if [[ -f "${CONFIG_DESTINATION}" && "${OVERWRITE_CONFIG}" == false ]]; then
		cp -- "${CONFIG_DESTINATION}" "${TEMPORARY_CONFIG}"
	else
		cp -- "${CONFIG_EXAMPLE}" "${TEMPORARY_CONFIG}"
		BASE_URL_SET=true
		API_KEY_SET=true
	fi

	if grep -Eq '^configured:' "${TEMPORARY_CONFIG}"; then
		sed -i -E 's/^configured:.*$/configured: true/' "${TEMPORARY_CONFIG}"
	else
		fail "missing configured field in configuration"
	fi
	if [[ "${BASE_URL_SET}" == true ]]; then
		replace_yaml_value "${TEMPORARY_CONFIG}" "base_url" "${BASE_URL}"
	fi
	if [[ "${API_KEY_SET}" == true ]]; then
		replace_yaml_value "${TEMPORARY_CONFIG}" "api_key" "${API_KEY}"
	fi

	install -o root -g "${SERVICE_GROUP}" -m 0640 "${TEMPORARY_CONFIG}" "${CONFIG_DESTINATION}"
	rm -f -- "${TEMPORARY_CONFIG}"
	TEMPORARY_CONFIG=""
}

install_service() {
	TEMPORARY_SERVICE="$(mktemp)"
	cat >"${TEMPORARY_SERVICE}" <<'EOF'
[Unit]
Description=Pulse Agent system metrics collector
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=pulse-agent
Group=pulse-agent
ExecStart=/usr/local/bin/pulse-agent --config /etc/nimweo/pulse-agent/config.yaml
Restart=on-failure
RestartSec=5s
RestartPreventExitStatus=78
UMask=0027
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictRealtime=true
StateDirectory=pulse-agent

[Install]
WantedBy=multi-user.target
EOF
	install -o root -g root -m 0644 "${TEMPORARY_SERVICE}" "${SERVICE_DESTINATION}"
	rm -f -- "${TEMPORARY_SERVICE}"
	TEMPORARY_SERVICE=""

	TEMPORARY_UPDATE_SERVICE="$(mktemp)"
	cat >"${TEMPORARY_UPDATE_SERVICE}" <<'EOF'
[Unit]
Description=Pulse Agent automatic updater
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/pulse-agent --automatic-update --config /etc/nimweo/pulse-agent/config.yaml --update-state /var/lib/pulse-agent-updater/update-state.json
TimeoutStartSec=10min
UMask=0027
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictRealtime=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=/usr/local/bin /etc/nimweo/pulse-agent /var/lib/pulse-agent-updater
EOF
	install -o root -g root -m 0644 "${TEMPORARY_UPDATE_SERVICE}" "${UPDATE_SERVICE_DESTINATION}"
	rm -f -- "${TEMPORARY_UPDATE_SERVICE}"
	TEMPORARY_UPDATE_SERVICE=""

	TEMPORARY_UPDATE_TIMER="$(mktemp)"
	cat >"${TEMPORARY_UPDATE_TIMER}" <<'EOF'
[Unit]
Description=Check for Pulse Agent updates

[Timer]
OnBootSec=5min
OnUnitActiveSec=1h
AccuracySec=5min
RandomizedDelaySec=5min
Unit=pulse-agent-update.service

[Install]
WantedBy=timers.target
EOF
	install -o root -g root -m 0644 "${TEMPORARY_UPDATE_TIMER}" "${UPDATE_TIMER_DESTINATION}"
	rm -f -- "${TEMPORARY_UPDATE_TIMER}"
	TEMPORARY_UPDATE_TIMER=""
}

require_command uname
validate_input
for command in awk cat chmod chown cp curl getent grep groupadd id install mktemp rm sed sha256sum systemctl tar useradd usermod; do
	require_command "${command}"
done
download_release

if systemctl is-active --quiet "${SERVICE_NAME}"; then
	systemctl stop "${SERVICE_NAME}"
fi
if systemctl is-active --quiet "${UPDATE_TIMER_NAME}"; then
	systemctl stop "${UPDATE_TIMER_NAME}"
fi
if systemctl is-active --quiet "${UPDATE_SERVICE_NAME}"; then
	systemctl stop "${UPDATE_SERVICE_NAME}"
fi

ensure_service_account
if [[ -e "${BINARY_DESTINATION}" && "${BINARY_SOURCE}" -ef "${BINARY_DESTINATION}" ]]; then
	chown root:root "${BINARY_DESTINATION}"
	chmod 0755 "${BINARY_DESTINATION}"
else
	install -o root -g root -m 0755 "${BINARY_SOURCE}" "${BINARY_DESTINATION}"
fi
install_configuration
"${BINARY_DESTINATION}" --migrate-config --config "${CONFIG_DESTINATION}"
install_service

systemctl daemon-reload
systemctl enable --now "${SERVICE_NAME}"
systemctl enable --now "${UPDATE_TIMER_NAME}"

printf 'Pulse Agent was installed successfully.\n'
printf 'Configuration: %s\n' "${CONFIG_DESTINATION}"
printf 'Service status: systemctl status %s\n' "${SERVICE_NAME}"
printf 'Service logs: journalctl -u %s -f\n' "${SERVICE_NAME}"
printf 'Manual update: sudo %s --update --config %s\n' "${BINARY_DESTINATION}" "${CONFIG_DESTINATION}"
