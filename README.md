# Pulse Agent

[![Tests](https://github.com/Nimweo/pulse-agent/actions/workflows/tests.yml/badge.svg?branch=main)](https://github.com/Nimweo/pulse-agent/actions/workflows/tests.yml)
[![Latest release](https://img.shields.io/github/v/release/Nimweo/pulse-agent?display_name=tag&sort=semver)](https://github.com/Nimweo/pulse-agent/releases/latest)
[![Release](https://github.com/Nimweo/pulse-agent/actions/workflows/release.yml/badge.svg)](https://github.com/Nimweo/pulse-agent/actions/workflows/release.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Pulse Agent is a small, cross-platform system telemetry agent written in Go. It collects host metrics at configurable intervals, batches them in memory, and sends them to an HTTP API. The agent is designed to run quietly as a system service and to keep the data contract simple: one JSON payload contains host identity, system details, core samples, and metric points.

## What it collects

- System identity: operating system, distribution, kernel, architecture, uptime, virtualization, hostname, processor model, memory size, and CPU topology.
- CPU: total utilization and optional per-logical-CPU utilization.
- Memory: used/total memory plus swap capacity, usage, and swap-in/swap-out rates.
- Load average: 1, 5, and 15 minute values.
- Disk: filesystem utilization and disk I/O throughput and operations.
- Network: receive/transmit throughput, packets, errors, and drops.
- GPU: utilization, VRAM, temperature, and power when supported by the vendor and driver.
- Processes (optional): process counts by state, top CPU/RSS process groups, and configured process groups to monitor.

Unsupported platform metrics are skipped or reported as unavailable; one unavailable collector does not invalidate the rest of a payload.

## How it works

1. The agent loads and validates `config.yaml`.
2. Enabled collectors run on their own intervals and append samples to a bounded in-memory buffer.
3. Samples are sent in batches to the configured ingest endpoint (optionally gzip-compressed).
4. At startup the configured health endpoint must return HTTP 200.
5. A `401 Unauthorized` or `403 Forbidden` response is treated as a fatal authentication error. The agent stops and exits with status code `78`, allowing systemd to keep restarting a misconfigured service from looping.

The API key is optional. When present, it is sent as `Authorization: Bearer <api-key>`.

## Requirements

- Linux, macOS, or Windows.
- A reachable HTTP(S) endpoint implementing `/health` and `/ingest`.
- Go 1.26 when building from source.
- Linux service installation additionally requires systemd, `curl`, `tar`, and `sha256sum`.

## Installation on Linux

The installer downloads the selected release directly from the public GitHub repository, verifies its SHA-256 checksum, installs the binary, creates a dedicated unprivileged `pulse-agent` user, and registers a systemd service.

Install the latest release without an API key:

```bash
curl -fsSL https://raw.githubusercontent.com/Nimweo/pulse-agent/main/install.sh \
  | sudo bash
```

Install with an API key (the key is not written to shell history when read from a file):

```bash
sudo install -m 600 /dev/null /root/pulse-api-key
sudo sh -c 'printf "%s" "YOUR_API_KEY" > /root/pulse-api-key'
curl -fsSL https://raw.githubusercontent.com/Nimweo/pulse-agent/main/install.sh \
  | sudo bash -s -- --api-key-file /root/pulse-api-key
```

Use a custom API endpoint or pin a release:

```bash
curl -fsSL https://raw.githubusercontent.com/Nimweo/pulse-agent/main/install.sh \
  | sudo bash -s -- \
      --base-url https://example.com/api/ \
      --version 0.11.1
```

The installer creates:

| Path | Purpose |
| --- | --- |
| `/usr/local/bin/pulse-agent` | Agent binary |
| `/etc/nimweo/pulse-agent/config.yaml` | Root-owned configuration |
| `/etc/systemd/system/pulse-agent.service` | Main service unit |
| `/etc/systemd/system/pulse-agent-update.service` | Privileged updater unit |
| `/etc/systemd/system/pulse-agent-update.timer` | Hourly updater trigger |
| `/var/lib/pulse-agent-updater/` | Updater state and lock files |

The embedded example configuration is copied on first installation. It starts with `configured: false` and the agent will refuse to collect until you review the file and set it to `true`.

## Configuration

Copy `configs/config.example.yaml` when running manually, or edit the file created by the installer:

```yaml
configured: true

server:
  base_url: "https://example.com/api/"
  api_key: ""
  timeout: 10
  api_endpoints:
    health: "health"
    ingest: "ingest"

intervals:
  collect: 1
  send: 60

collectors:
  system: { enabled: true, interval: 60 }
  load: { enabled: true, interval: 1 }
  cpu:
    enabled: true
    interval: 1
    per_cpu: false
  memory: { enabled: true, interval: 1 }
  disk: { enabled: true, interval: 1 }
  network: { enabled: true, interval: 1 }
  gpu: { enabled: true, interval: 5 }
  process:
    enabled: false
    interval: 5
    top_cpu: 5
    top_memory: 5
    monitored_processes: []

updates:
  enabled: false
  interval: "24h"
```

All available options, defaults, and comments are maintained in [`configs/config.example.yaml`](configs/config.example.yaml). Important details:

- `server.base_url` must be an absolute HTTP(S) URL without a query or fragment. `server.api_endpoints.health` and `server.api_endpoints.ingest` are relative paths appended to it; both default to `health` and `ingest` when omitted. This supports routes such as `v1/agent/health` and `v1/agent/metrics`.
- `intervals.send` controls batch delivery; `buffer.max_size` limits memory retained before a forced send.
- `collectors.process` is disabled by default. `top_cpu` and `top_memory` are independent rankings. Add names to `monitored_processes` to receive instance counts and dedicated CPU/memory metrics for those groups.
- `buffer.disk_spool_enabled` is reserved for a future disk-spool implementation and currently has no effect.

### Automatic updates

Automatic updates are disabled by default. On Linux, enable them in the configuration:

```yaml
updates:
  enabled: true
  interval: "24h" # 1h, 6h, 12h, 24h, weekly, monthly
```

The installer installs a root-owned systemd updater timer. It downloads only official GitHub release archives, verifies the published checksum, validates the downloaded binary version, migrates missing configuration keys without overwriting existing values, and rolls back the binary if migration fails.

Run an update immediately, regardless of `updates.enabled`:

```bash
sudo /usr/local/bin/pulse-agent \
  --update \
  --config /etc/nimweo/pulse-agent/config.yaml
```

Check the installed version and service:

```bash
/usr/local/bin/pulse-agent --version
sudo systemctl status pulse-agent
sudo journalctl -u pulse-agent -f
```

The current updater replaces binaries automatically on Linux and macOS. Windows builds include the update command and release metadata, but binary replacement is intentionally not performed by the updater yet.

## Running manually

Build and run from source:

```bash
go build -trimpath -ldflags="-s -w" -o pulse-agent ./cmd/agent
./pulse-agent --config ./configs/config.yaml
```

Useful commands:

```bash
./pulse-agent --version
./pulse-agent --update --config ./configs/config.yaml
./pulse-agent --migrate-config --config ./configs/config.yaml
```

On Linux, without `--config`, the installed agent uses `/etc/nimweo/pulse-agent/config.yaml` when it exists. On other platforms it resolves the platform configuration directory. The first run creates an embedded example and exits with a message asking you to configure it.

## API contract

The agent performs:

```text
GET  <base_url>/<api_endpoints.health>
POST <base_url>/<api_endpoints.ingest>
```

The ingest request is a JSON payload (gzip when `transport.compression` is enabled):

```json
{
  "schema_version": 1,
  "batch_id": "a-unique-id",
  "sent_at": 1730000000000,
  "agent_version": "0.11.1",
  "hostname": "server-01",
  "system": {},
  "core": [],
  "points": [
    {
      "time": 1730000000000,
      "metric": "process_top_cpu_percent",
      "device": "php-fpm",
      "value": 18.4
    }
  ]
}
```

Every point has a timestamp, metric name, device/group label, and numeric value. The schema version is currently `1`.

## Supported release targets

Each tagged release publishes archives for:

| OS | Architectures |
| --- | --- |
| Linux | amd64, arm64 |
| Windows | amd64, arm64 |
| macOS | amd64, arm64 |

Release archives include the binary, `LICENSE`, `NOTICE`, `THIRD-PARTY-NOTICES`, and `configs/config.example.yaml`; Linux archives also include `install.sh`. Every release includes `checksums.txt`.

## Development

Run the complete local validation suite:

```bash
go test ./...
go test -race ./...
go vet ./...
bash -n install.sh
go mod verify
```

Build a release binary for a target platform:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o pulse-agent ./cmd/agent
```

Pull requests and pushes run formatting checks, installer syntax checks, `go vet`, and tests on Ubuntu, Windows, and macOS. Version tags in the `vMAJOR.MINOR.PATCH` format trigger the multi-platform release workflow.

## Troubleshooting

### The agent exits saying it is not configured

Edit the path shown in the error, set `configured: true`, and verify `server.base_url`. The guard is intentional: an untouched example configuration must never start sending data.

### The service repeatedly restarts with exit code 78

The API returned `401` or `403`. Check `server.api_key`, the endpoint permissions, and the service logs:

```bash
sudo journalctl -u pulse-agent -n 100 --no-pager
```

### The service cannot reach the API

Verify both endpoints independently:

```bash
curl -i https://example.com/api/health
curl -i -H 'Content-Type: application/json' https://example.com/api/ingest
```

The health endpoint must return `200 OK`. A valid ingest endpoint should accept the agent payload and return any `2xx` response.

### A process has `--` for CPU or memory

The process collector reports CPU and memory as separate top lists. A process can be in `top_cpu` without being in `top_memory`, or vice versa. Add the process to `monitored_processes` when both dimensions and instance counts are required.

## License

Pulse Agent is distributed under the [Apache License 2.0](LICENSE).

Third-party dependency attributions and license references are listed in [`THIRD-PARTY-NOTICES`](THIRD-PARTY-NOTICES) and are included in every release archive.

You may modify, fork, and redistribute the project, including for commercial use. When distributing the project or a derivative work, retain [`NOTICE`](NOTICE), the license, and existing copyright notices. Please identify substantial changes as your own modifications and do not imply that Nimweo endorses a derived product.

The upstream project is [Nimweo/pulse-agent](https://github.com/Nimweo/pulse-agent). The Apache license requires preservation of the attribution notices; it does not require publishing private modifications or using the Pulse Agent name for a derivative work.

## Security

Please see [SECURITY.md](SECURITY.md) for the vulnerability reporting process. Do not disclose credentials or security-sensitive details in public issues.
