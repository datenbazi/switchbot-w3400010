# switchbot-temp — Developer Guide

## Overview

`switchbot-temp` is a single Go binary that:

1. Passively scans BLE advertisements from SwitchBot W3400010 sensors
2. Decodes temperature, humidity, and battery from the manufacturer payload
3. Stores readings in a local SQLite database
4. Serves a web UI + REST API + SSE stream
5. Evaluates configurable alert rules and sends Telegram notifications

No BLE pairing is required. The sensor broadcasts unconditionally ~every 5 s.

---

## Repository Layout

```
.
├── main.go                  # Entry point: flag parsing, BLE scan loop
├── ble.go                   # BLE advertisement decoding
├── db.go                    # SQLite schema, read/write helpers
├── server.go                # HTTP server, REST API, SSE hub
├── alerts.go                # Alert rule engine
├── telegram.go              # Telegram Bot API client
├── ui/
│   └── index.html           # Single-page web UI (Chart.js + SSE)
├── alerts.json              # Alert config (bot token, chat ID, rules)
├── switchbot-temp.service   # systemd unit
├── bt-up.sh                 # BT adapter pre-flight helper
├── switchbot.sh             # Daemon management (start/stop/status/enable…)
└── make-release.sh          # Cross-compile + package release tarballs
```

---

## BLE Protocol (SwitchBot W3400010)

The sensor uses **passive** BLE advertisements — it broadcasts to anyone
listening; no pairing or connection is needed.

### Identifying packets

| AD type | Value | Meaning |
|---|---|---|
| `0xFF` (Manufacturer Specific) | Company ID `0x0969` | Identifies SwitchBot sensor |
| `0x16` (Service Data) | UUID `0xFD3D` | Contains battery level |

### Manufacturer payload (AD type `0xFF`, after the 2-byte company ID)

| Byte(s) | Content |
|---|---|
| `[0–5]` | Device MAC address |
| `[8]` | Temperature decimal nibble: `(byte & 0x0F) × 0.1` |
| `[9]` | Temperature integer: bits `6:0`; sign: bit 7 (`1` = positive) |
| `[10]` | Humidity %: bits `6:0` |

Full temperature: `sign × (integer + decimal)`, e.g. byte 9 = `0x9C` (positive, 28), byte 8 = `0x03` → **28.3 °C**.

### Service data payload (AD type `0x16`, after the 2-byte UUID)

| Byte | Content |
|---|---|
| `[2]` | Battery %: bits `6:0` |

---

## Module & Dependencies

```
module switchbot-temp   (go 1.23+)

tinygo.org/x/bluetooth  — BLE scanning via BlueZ D-Bus (pure Go, CGO_ENABLED=0)
modernc.org/sqlite      — CGO-free SQLite driver
```

`CGO_ENABLED=0` is required. The binary is fully static.

---

## Build

```bash
# Host architecture (amd64)
CGO_ENABLED=0 go build -ldflags="-s -w" -o switchbot-temp .

# ARMv7l cross-compile (e.g. Raspberry Pi)
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o switchbot-temp-armv7l .
```

---

## Database Schema

SQLite WAL mode, file defaults to `switchbot.db`.

```sql
CREATE TABLE readings (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    ts       DATETIME NOT NULL,
    address  TEXT NOT NULL,
    rssi     INTEGER,
    temp     REAL,
    humidity INTEGER,
    battery  INTEGER
);
CREATE INDEX readings_ts      ON readings(ts);
CREATE INDEX readings_address ON readings(address);

CREATE TABLE devices (
    address    TEXT PRIMARY KEY,
    first_seen DATETIME NOT NULL,
    last_seen  DATETIME NOT NULL
);
```

Readings are deduplicated in memory: a row is written only when a value
changes, or when `-store-every` has elapsed since the last write.

---

## REST API

All endpoints return JSON.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/latest` | Most recent reading per device |
| `GET` | `/api/history?address=<MAC>&hours=<n>` | Historical readings (default 24 h) |
| `GET` | `/api/devices` | All registered devices with `first_seen`/`last_seen` |
| `GET` | `/api/export?address=<MAC>&hours=<n>` | CSV export |
| `GET` | `/api/alerts` | Current alert evaluation state |
| `GET` | `/api/alerts/rules` | Configured alert rules |
| `GET` | `/events` | SSE stream (see below) |

---

## SSE Stream (`/events`)

Each message is a JSON object. The `event:` field identifies the type.

| Event | Payload | Fired when |
|---|---|---|
| `reading` | `{address, ts, temp, humidity, battery, rssi}` | Every BLE advertisement received |
| `device_added` | `{address}` | A device is seen for the first time ever |

The UI subscribes to both. `reading` drives the live charts; `device_added`
triggers a toast notification and refreshes the device list.

---

## Alert System

`alerts.go` implements a stateful rule engine evaluated on every reading.

### Lifecycle

1. **Condition onset** — `condSince` is set to `now`
2. **Sustain check** — if `now − condSince >= rule.For` → fire
3. **Cooldown check** — fire only if `now − firedAt >= rule.Cooldown`
4. **Recovery** — when condition clears and an alert was previously sent, a recovery message is dispatched

State is keyed by `"<ruleIndex>:<deviceAddress>"` so each rule tracks each
device independently.

### Telegram client (`telegram.go`)

A thin wrapper around the Telegram Bot API `sendMessage` endpoint.
Configures via `telegram_bot_token` and `telegram_chat_id` in `alerts.json`.
Calls are made in a goroutine so they never block the BLE scan loop.

---

## Release Packaging

```bash
./make-release.sh              # version = YYYYMMDD
./make-release.sh v1.2         # custom version tag
```

Produces three tarballs in `_release/`:

| File | Contents |
|---|---|
| `switchbot-temp-<ver>.tar.gz` | Full bundle: both binaries + all assets |
| `switchbot-temp-<ver>-linux-amd64.tar.gz` | amd64 binary only |
| `switchbot-temp-<ver>-linux-armv7l.tar.gz` | ARMv7l binary (named `switchbot-temp`) |

Each tarball includes `install.sh`, which auto-detects architecture and
installs to `/opt/switchbot-temp/`.

---

## Adding a New Metric

1. Add the decode logic in `ble.go` and expose it on the `reading` struct in `db.go`
2. Add the DB column in `openDB()` in `db.go` and update `storeReading` / `queryLatest` / `queryHistory`
3. Add the metric name to the `switch` in `alerts.go` (`Check` method)
4. Update the SSE payload and chart in `ui/index.html`
