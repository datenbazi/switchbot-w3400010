# switchbot-w3400010

Passively reads temperature, humidity, and battery from
**SwitchBot Indoor/Outdoor Thermo-Hygrometer (Model W3400010)** over BLE on Linux.
No pairing. No official app. No cloud.

![Web UI showing live temperature and humidity charts per device](docs/screenshot.png)

---

## Platform Support

| Architecture | Binary | Confirmed working on |
|---|---|---|
| **x86-64** | `switchbot-temp` | Linux PC, home server, NUC, VM |
| **ARMv7l** | `switchbot-temp-armv7l` | Raspberry Pi 2/3/4, OSMC, LibreELEC, Armbian |

Fully static binary — no runtime dependencies, no CGO, no shared libraries.
If it runs Linux and has a Bluetooth adapter, it will work.

> **Note:** ARMv6l (Raspberry Pi 1 / Zero) is not currently provided as a
> pre-built binary but can be compiled from source with `GOARCH=arm GOARM=6`.

---

## Features

- **Passive BLE scanning** — receives broadcasts without pairing or connecting
- **Live web UI** — real-time charts via SSE, auto-detects new devices
- **REST API** — query latest readings, history, and export CSV/JSON
- **SQLite storage** — local, no external dependencies
- **Telegram alerts** — configurable threshold rules with sustain + cooldown logic
- **Privacy mode** — toggle MAC address redaction in the UI
- **systemd integration** — ships with a service unit and a management script

---

## Quick Start

### Download a release

Grab the latest release for your architecture from the
[Releases page](../../releases/latest):

| File | For |
|---|---|
| `switchbot-temp-<ver>-linux-amd64.tar.gz` | PC / server (x86-64) |
| `switchbot-temp-<ver>-linux-armv7l.tar.gz` | Raspberry Pi, OSMC, Armbian (ARMv7l) |

```bash
tar xzf switchbot-temp-<ver>-linux-<arch>.tar.gz
cd switchbot-temp-<ver>/
sudo ./install.sh          # copies to /opt/switchbot-temp, enables systemd service
```

Or run directly without installing:

```bash
sudo ./bt-up.sh            # ensure Bluetooth adapter is ready
sudo ./switchbot-temp      # start scanning; web UI at http://localhost:7700
```

### Build from source

```bash
git clone https://github.com/datenbazi/switchbot-w3400010
cd switchbot-w3400010
CGO_ENABLED=0 go build -ldflags="-s -w" -o switchbot-temp .
sudo ./switchbot-temp
```

Requires Go 1.23+. Cross-compile for ARM:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o switchbot-temp-armv7l .
```

---

## Web UI

Open **http://localhost:7700** after starting.

- Live temperature and humidity charts per device
- Battery indicator per sensor
- Toast notification on new device discovery
- MAC address redaction toggle (for screenshots / privacy)
- CSV / JSON export

---

## Telegram Alerts

Edit `alerts.json` with your bot token and chat ID, then start with:

```bash
sudo ./switchbot-temp -alerts alerts.json
```

Alert and recovery messages are sent when thresholds are breached.
See [USER.md](USER.md#telegram-alerts) for setup instructions.

---

## Management Script

`switchbot.sh` manages the daemon without typing raw commands:

```bash
./switchbot.sh check      # pre-flight: BT hardware, BlueZ, port, binary
sudo ./switchbot.sh start
sudo ./switchbot.sh stop
./switchbot.sh status
./switchbot.sh log
sudo ./switchbot.sh enable   # install + start as systemd service
sudo ./switchbot.sh disable  # remove systemd service
```

---

## Documentation

- [USER.md](USER.md) — full setup guide, CLI flags, Telegram configuration, troubleshooting
- [DEVELOPER.md](DEVELOPER.md) — architecture, BLE protocol, DB schema, API reference, release packaging

---

## Privacy & Security

> [!WARNING]
> The SwitchBot W3400010 broadcasts all data (temperature, humidity, battery,
> fixed MAC address) **openly and unencrypted** over BLE to anyone within range
> (~10–30 m). No pairing is required to receive it — by design.
>
> See [USER.md — Privacy & Security Notice](USER.md#privacy--security-notice)
> for full details.

---

## Tested Hardware

| Hardware | Role | Notes |
|---|---|---|
| SwitchBot W3400010 | Sensor | Indoor/Outdoor Thermo-Hygrometer — confirmed working |
| Linux PC (x86-64) | Scanner | Any distro with BlueZ |
| OSMC (ARMv7l) | Scanner | Raspberry Pi-based media centre — confirmed working |
| Raspberry Pi 3/4 | Scanner | Armbian / Raspberry Pi OS — confirmed working |

Other SwitchBot BLE sensors using company ID `0x0969` may work but are untested.

---

## License

MIT
