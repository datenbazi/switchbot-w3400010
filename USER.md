# switchbot-temp — User Guide

Passively receives temperature, humidity, and battery readings from
**SwitchBot Indoor/Outdoor Thermo-Hygrometer (Model W3400010)** over BLE
and exposes them via a web UI, a REST API, and optional Telegram alerts.
No pairing, no official app, no cloud required.

---

> [!WARNING]
> ## Privacy & Security Notice
>
> **The SwitchBot W3400010 broadcasts all sensor data openly over BLE — no pairing,
> no encryption, no authentication — by design.**
>
> This means:
>
> - **Anyone within BLE range (~10–30 m) can read your temperature, humidity, and
>   battery level** using any standard BLE scanner, including this tool.
> - **The sensor's MAC address is fixed and always included in the broadcast.**
>   A static MAC can be used to fingerprint a specific device and tie it to a
>   physical location.
> - **Occupancy patterns can be inferred.** A receiver outside your home can
>   log when your sensors are active or go offline.
>
> This is not a vulnerability in this software — our tool works *because* the
> sensor was designed this way (simpler, lower power, no app pairing needed).
> But you should be aware of it before deploying sensors in sensitive environments.
>
> **Mitigation:** BLE range is short. Sensors placed well inside a building and
> away from exterior walls limit who can realistically receive the signal.
> There is no software fix — this is a hardware/protocol limitation of the device.

---

## Requirements

**Hardware**
- A Bluetooth 4.0+ adapter (USB dongle or built-in)
- One or more SwitchBot W3400010 sensors

**OS / Software**
- Linux (x86-64 or ARMv7l, e.g. Raspberry Pi)
- BlueZ (`bluetoothd`) — install via `apt install bluez`
- `rfkill` — install via `apt install rfkill`
- Run as root, or grant `CAP_NET_ADMIN` to the binary (see below)

---

## Quick Start

### 1. Prepare Bluetooth

```bash
sudo ./bt-up.sh
```

This checks and fixes: bluetoothd running, rfkill unblock, adapter powered on.

### 2. Run

```bash
sudo ./switchbot-temp
```

The scanner starts printing readings to the terminal and the web UI is
available at **http://localhost:7700**.

### 3. Stop

Press `Ctrl+C`.

---

## Installation as a Service (recommended)

Use `switchbot.sh` to manage the daemon without systemd, or install it as
a proper systemd service that starts on boot and restarts on crash.

### Pre-install check

```bash
./switchbot.sh check
```

Verifies the binary, BT hardware, BlueZ, port availability, and DB path.

### Start / Stop manually

```bash
sudo ./switchbot.sh start
sudo ./switchbot.sh stop
sudo ./switchbot.sh restart
./switchbot.sh status
./switchbot.sh log
```

### Install as a systemd service

```bash
sudo ./switchbot.sh enable
```

This copies the service unit to `/etc/systemd/system/`, enables it, and
starts it immediately.

```bash
sudo ./switchbot.sh disable   # stop and remove the unit
```

### Alternative: install.sh (from the release package)

If you downloaded a release tarball, `install.sh` copies files to
`/opt/switchbot-temp/` and installs the service:

```bash
sudo ./install.sh
```

---

## Web UI

Open **http://localhost:7700** (or the host/port you configured) in a browser.

- Live temperature and humidity charts per device (updates via SSE)
- Battery indicator per device
- Toast notification when a new device is discovered
- Data export via the download button

---

## CLI Flags

| Flag | Default | Description |
|---|---|---|
| `-db` | `switchbot.db` | SQLite database file. Set to empty string to disable. |
| `-listen` | `:7700` | Web UI / API listen address. Set to empty to disable. |
| `-store-every` | `5m` | Force a DB write at least this often even when values unchanged. `0` = write on change only. |
| `-alerts` | *(disabled)* | Path to alerts config JSON file. |
| `-device` | *(all)* | Filter to a single MAC address. |
| `-timeout` | *(none)* | Stop scanning after this duration, e.g. `30s`. |
| `-once` | false | Print the first reading from each device then exit. |
| `-json` | false | Output one JSON object per line (machine-readable). |
| `-all` | false | Print every advertisement, including duplicates. |
| `-verbose` | false | Print raw BLE advertisement hex bytes to stderr. |

**Example — one-shot JSON output:**

```bash
sudo ./switchbot-temp -once -json
```

---

## Telegram Alerts

### 1. Create a Telegram Bot

1. Open Telegram and message **@BotFather**
2. Send `/newbot` and follow the prompts
3. Copy the **bot token** (looks like `123456789:ABCDefGh...`)

### 2. Get your Chat ID

Send a message to your bot, then visit:

```
https://api.telegram.org/bot<YOUR_TOKEN>/getUpdates
```

Find `"chat": {"id": ...}` in the response — that is your chat ID.
For a group chat, add the bot to the group first.

### 3. Configure alerts.json

```json
{
  "telegram_bot_token": "123456789:ABCDefGhIjklMNOpqrSTUvwxYZ",
  "telegram_chat_id": "987654321",
  "rules": [...]
}
```

### 4. Start with alerts enabled

```bash
sudo ./switchbot-temp -alerts alerts.json
```

### Alert Rule Fields

| Field | Required | Description |
|---|---|---|
| `name` | yes | Human-readable label used in the Telegram message |
| `device` | yes | MAC address to watch, or `"*"` for all devices |
| `metric` | yes | `"temperature_c"` or `"humidity_pct"` |
| `condition` | yes | `"above"` or `"below"` |
| `threshold` | yes | Numeric threshold value |
| `for` | no | Condition must hold for this duration before firing, e.g. `"5m"` |
| `cooldown` | no | Minimum time between repeated alerts (default `"30m"`) |

Alerts also send a **recovery message** when the condition clears.

---

## CAP_NET_ADMIN (avoid sudo)

To run without `sudo`:

```bash
sudo setcap cap_net_admin+ep ./switchbot-temp
./switchbot-temp
```

Note: this is reset if you overwrite the binary.

---

## Troubleshooting

**"BLE adapter error"**
Run `sudo ./bt-up.sh` or `./switchbot.sh check` to diagnose.

**No devices found**
- Confirm the sensor is active (press its button once)
- Run `sudo hcitool lescan` — if that also shows nothing, the adapter or BlueZ is the issue
- Try `-verbose` to see raw advertisement data

**Port already in use**
Change the listen address: `sudo ./switchbot-temp -listen :7701`

**Telegram alerts not arriving**
- Verify the bot token by opening `https://api.telegram.org/bot<TOKEN>/getMe` in a browser
- Ensure the bot has sent at least one message to the chat (or is a member of the group)
- Check stderr output for error messages
