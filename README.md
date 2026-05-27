# switchbot-temp

Passively reads temperature, humidity, and battery from
**SwitchBot Indoor/Outdoor Thermo-Hygrometer (Model W3400010)** over BLE on Linux.
No pairing. No official app. No cloud.

![Web UI showing live temperature and humidity charts per device](docs/screenshot.png)

---

## Features

- **Passive BLE scanning** — receives broadcasts without pairing or connecting
- **Live web UI** — real-time charts via SSE, auto-detects new devices
- **REST API** — query latest readings, history, and export CSV
- **SQLite storage** — local, no external dependencies
- **Telegram alerts** — configurable threshold rules with sustain + cooldown logic
- **Static binary** — single file, no runtime dependencies, runs on amd64 and ARMv7l
- **systemd integration** — ships with a service unit and a management script

---

## Quick Start

### Download a release

Grab the latest release for your architecture from the
[Releases page](../../releases/latest):

| File | Platform |
|---|---|
| `switchbot-temp-<ver>-linux-amd64.tar.gz` | x86-64 (PC, server) |
| `switchbot-temp-<ver>-linux-armv7l.tar.gz` | ARMv7l (Raspberry Pi, OSMC, …) |

```bash
tar xzf switchbot-temp-<ver>-linux-<arch>.tar.gz
cd switchbot-temp-<ver>
sudo ./install.sh          # copies to /opt/switchbot-temp, enables systemd service
```

Or run directly without installing:

```bash
sudo ./bt-up.sh            # ensure Bluetooth adapter is ready
sudo ./switchbot-temp      # start scanning; web UI at http://localhost:7700
```

### Build from source

```bash
git clone https://github.com/<you>/switchbot-temp
cd switchbot-temp
CGO_ENABLED=0 go build -ldflags="-s -w" -o switchbot-temp .
sudo ./switchbot-temp
```

Requires Go 1.23+. No CGO, no system libraries.

---

## Web UI

Open **http://localhost:7700** after starting.

- Live temperature and humidity charts per device
- Battery indicator
- Toast notification on new device discovery
- CSV export

---

## Telegram Alerts

Edit `alerts.json` with your bot token and chat ID, then start with:

```bash
sudo ./switchbot-temp -alerts alerts.json
```

You receive alert and recovery messages when thresholds are breached.
See [USER.md](USER.md#telegram-alerts) for setup instructions.

---

## Management Script

`switchbot.sh` manages the daemon without systemd:

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

| Device | Notes |
|---|---|
| SwitchBot W3400010 | Indoor/Outdoor Thermo-Hygrometer — confirmed working |

Other SwitchBot BLE sensors using company ID `0x0969` may work but are untested.

---

## License

MIT
