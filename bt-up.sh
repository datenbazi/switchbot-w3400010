#!/usr/bin/env bash
set -euo pipefail

need_root() {
    if [[ $EUID -ne 0 ]]; then
        echo "re-running with sudo..."
        exec sudo "$0" "$@"
    fi
}
need_root "$@"

ok()   { echo "[ok]  $*"; }
fix()  { echo "[fix] $*"; }
die()  { echo "[err] $*" >&2; exit 1; }

# 1. bluetoothd running
if ! systemctl is-active --quiet bluetooth; then
    fix "starting bluetooth service"
    systemctl start bluetooth
    sleep 1
fi
ok "bluetoothd running"

# 2. rfkill unblocked
if rfkill list bluetooth 2>/dev/null | grep -q "Soft blocked: yes"; then
    fix "unblocking bluetooth (rfkill)"
    rfkill unblock bluetooth
    sleep 1
fi
ok "rfkill clear"

# 3. adapter up
if hciconfig hci0 2>/dev/null | grep -q "DOWN"; then
    fix "bringing hci0 up"
    hciconfig hci0 up
    sleep 1
fi
ok "hci0 up"

# 4. bluetoothctl powered on
if bluetoothctl show 2>/dev/null | grep -q "Powered: no"; then
    fix "powering on adapter"
    bluetoothctl power on >/dev/null
    sleep 1
fi
ok "adapter powered"

echo ""
echo "Bluetooth ready. Run:  sudo ./switchbot-temp"
