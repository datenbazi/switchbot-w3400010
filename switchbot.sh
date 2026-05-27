#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY="$SCRIPT_DIR/switchbot-temp"
PIDFILE="$SCRIPT_DIR/switchbot-temp.pid"
LOGFILE="$SCRIPT_DIR/switchbot-temp.log"
DB="$SCRIPT_DIR/switchbot.db"
LISTEN=":7700"
STORE_EVERY="5m"

need_root() {
    if [[ $EUID -ne 0 ]]; then
        exec sudo "$0" "$@"
    fi
}

cmd_start() {
    if [[ -f "$PIDFILE" ]]; then
        PID=$(cat "$PIDFILE")
        if [[ -d "/proc/$PID" ]]; then
            echo "already running (PID $PID)"
            exit 0
        fi
        rm -f "$PIDFILE"
    fi

    # Ensure Bluetooth is ready
    if ! systemctl is-active --quiet bluetooth 2>/dev/null; then
        echo "starting bluetooth service..."
        systemctl start bluetooth
        sleep 1
    fi
    if rfkill list bluetooth 2>/dev/null | grep -q "Soft blocked: yes"; then
        echo "unblocking bluetooth..."
        rfkill unblock bluetooth
        sleep 1
    fi
    if hciconfig hci0 2>/dev/null | grep -q "DOWN"; then
        echo "bringing hci0 up..."
        hciconfig hci0 up
        sleep 1
    fi

    nohup "$BINARY" -db "$DB" -listen "$LISTEN" -store-every "$STORE_EVERY" \
        >> "$LOGFILE" 2>&1 &
    echo $! > "$PIDFILE"
    sleep 1

    PID=$(cat "$PIDFILE")
    if [[ -d "/proc/$PID" ]]; then
        echo "started (PID $PID)  WebUI → http://localhost${LISTEN}"
    else
        echo "failed to start — check $LOGFILE"
        rm -f "$PIDFILE"
        exit 1
    fi
}

cmd_stop() {
    if [[ ! -f "$PIDFILE" ]]; then
        echo "not running (no pidfile)"
        return
    fi
    PID=$(cat "$PIDFILE")
    if [[ -d "/proc/$PID" ]]; then
        kill "$PID"
        rm -f "$PIDFILE"
        echo "stopped (PID $PID)"
    else
        echo "not running (stale pidfile)"
        rm -f "$PIDFILE"
    fi
}

cmd_restart() {
    cmd_stop || true
    sleep 1
    cmd_start
}

cmd_status() {
    if [[ -f "$PIDFILE" ]]; then
        PID=$(cat "$PIDFILE")
        if [[ -d "/proc/$PID" ]]; then
            UPTIME=$(ps -o etime= -p "$PID" 2>/dev/null | xargs)
            echo "running  PID=$PID  uptime=$UPTIME  WebUI=http://localhost${LISTEN}"
            echo ""
            tail -5 "$LOGFILE" 2>/dev/null && true
        else
            echo "dead (stale pidfile)"
        fi
    else
        # Also check if it's running without our pidfile (e.g. manual start)
        if pgrep -x switchbot-temp > /dev/null 2>&1; then
            pgrep -a switchbot-temp | sed 's/^/running (unmanaged): /'
        else
            echo "stopped"
        fi
    fi
}

cmd_log() {
    tail -f "$LOGFILE"
}

cmd_check() {
    local ok=0 fail=0

    pass() { echo "  [ok]  $*";  (( ok++   )) || true; }
    warn() { echo "  [!!]  $*";  (( fail++ )) || true; }

    echo "Pre-install check for switchbot-temp"
    echo "======================================"

    # 1. Binary
    echo ""
    echo "Binary"
    if [[ -f "$BINARY" ]]; then
        if [[ -x "$BINARY" ]]; then
            SIZE=$(du -h "$BINARY" | cut -f1)
            pass "found: $BINARY ($SIZE)"
        else
            warn "exists but not executable: $BINARY  →  run: chmod +x $BINARY"
        fi
    else
        warn "not found: $BINARY  →  run: CGO_ENABLED=0 go build -ldflags='-s -w' -o switchbot-temp ."
    fi

    # 2. sudo / root access
    echo ""
    echo "Privileges"
    if [[ $EUID -eq 0 ]]; then
        pass "running as root"
    elif sudo -n true 2>/dev/null; then
        pass "sudo available without password"
    elif sudo -v 2>/dev/null; then
        pass "sudo available (password may be required)"
    else
        warn "neither root nor sudo — BLE scanning requires root or CAP_NET_ADMIN"
    fi

    # 3. Bluetooth hardware
    echo ""
    echo "Bluetooth hardware"
    if command -v hciconfig &>/dev/null; then
        pass "hciconfig found: $(command -v hciconfig)"
        if hciconfig 2>/dev/null | grep -q "hci"; then
            ADDR=$(hciconfig 2>/dev/null | grep "BD Address" | awk '{print $3}')
            BUS=$(hciconfig  2>/dev/null | grep "Bus:"       | awk -F'Bus: ' '{print $2}' | head -1)
            pass "adapter present: hci0  addr=$ADDR  bus=$BUS"
        else
            warn "no Bluetooth adapter found (hciconfig shows nothing)"
        fi
    else
        warn "hciconfig not found  →  apt install bluez"
    fi

    # 4. rfkill
    echo ""
    echo "RF-kill"
    if command -v rfkill &>/dev/null; then
        pass "rfkill found: $(command -v rfkill)"
        if rfkill list bluetooth 2>/dev/null | grep -q "Hard blocked: yes"; then
            warn "Bluetooth is HARD blocked (physical switch or BIOS) — cannot unblock in software"
        elif rfkill list bluetooth 2>/dev/null | grep -q "Soft blocked: yes"; then
            warn "Bluetooth is soft-blocked  →  start will auto-fix via: rfkill unblock bluetooth"
        else
            pass "not blocked"
        fi
    else
        warn "rfkill not found  →  apt install rfkill"
    fi

    # 5. bluetoothd / BlueZ
    echo ""
    echo "BlueZ daemon"
    if systemctl is-active --quiet bluetooth 2>/dev/null; then
        VER=$(bluetoothctl version 2>/dev/null | awk '{print $NF}' || echo "?")
        pass "bluetoothd running  (BlueZ $VER)"
    elif systemctl list-unit-files bluetooth.service &>/dev/null 2>&1; then
        warn "bluetooth.service exists but is not running  →  start will auto-fix via: systemctl start bluetooth"
    else
        warn "bluetooth.service not found  →  apt install bluez"
    fi

    # 6. Port availability
    echo ""
    echo "Network port"
    PORT="${LISTEN#:}"
    # Determine who owns the port (needs root for PID visibility; fall back to pidfile)
    PORT_TAKEN=$(ss -tlnp 2>/dev/null | grep -c ":${PORT} " || true)
    if (( PORT_TAKEN > 0 )); then
        OUR_PID=""
        [[ -f "$PIDFILE" ]] && OUR_PID=$(cat "$PIDFILE")
        if [[ -n "$OUR_PID" ]] && [[ -d "/proc/$OUR_PID" ]] && \
           grep -q "switchbot-temp" "/proc/$OUR_PID/cmdline" 2>/dev/null; then
            pass "port $PORT in use by switchbot-temp (PID $OUR_PID) — already running"
        else
            # Try with sudo to identify the owner
            OWNER=$(sudo ss -tlnp 2>/dev/null | grep ":${PORT} " | grep -oP '"[^"]+"' | head -1 || echo "unknown")
            warn "port $PORT already in use by $OWNER  →  change LISTEN in this script"
        fi
    else
        pass "port $PORT is free"
    fi

    # 7. DB directory writable
    echo ""
    echo "Database"
    DB_DIR=$(dirname "$DB")
    if [[ -w "$DB_DIR" ]]; then
        if [[ -f "$DB" ]]; then
            ROWS=$(python3 -c "import sqlite3; c=sqlite3.connect('$DB'); print(c.execute('select count(*) from readings').fetchone()[0])" 2>/dev/null || echo "?")
            pass "exists: $DB  ($ROWS stored readings)"
        else
            pass "will be created at: $DB"
        fi
    else
        warn "$DB_DIR is not writable by current user"
    fi

    # Summary
    echo ""
    echo "======================================"
    if (( fail == 0 )); then
        echo "All checks passed — ready to run."
        return 0
    else
        echo "$fail issue(s) found, $ok check(s) passed."
        echo "Issues marked [!!] must be resolved before starting."
        return 1
    fi
}

cmd_enable() {
    local unit_src="$SCRIPT_DIR/switchbot-temp.service"
    local unit_dst="/etc/systemd/system/switchbot-temp.service"

    if [[ ! -f "$unit_src" ]]; then
        echo "service file not found: $unit_src"
        exit 1
    fi

    # Stop any manually-started instance first
    cmd_stop 2>/dev/null || true

    cp "$unit_src" "$unit_dst"
    systemctl daemon-reload
    systemctl enable switchbot-temp
    systemctl start  switchbot-temp
    sleep 2
    systemctl status switchbot-temp --no-pager -l | head -20
}

cmd_disable() {
    systemctl stop    switchbot-temp 2>/dev/null || true
    systemctl disable switchbot-temp 2>/dev/null || true
    rm -f /etc/systemd/system/switchbot-temp.service
    systemctl daemon-reload
    echo "systemd unit removed"
}

case "${1:-}" in
    start)   need_root "$@"; cmd_start   ;;
    stop)    need_root "$@"; cmd_stop    ;;
    restart) need_root "$@"; cmd_restart ;;
    status)  cmd_status ;;
    log)     cmd_log    ;;
    check)   cmd_check  ;;
    enable)  need_root "$@"; cmd_enable  ;;
    disable) need_root "$@"; cmd_disable ;;
    *)
        echo "Usage: $0 {start|stop|restart|status|log|check|enable|disable}"
        echo ""
        echo "  check    pre-install check: binary, BT hardware, BlueZ, port, DB"
        echo "  start    ensure BT is up, start scanner + WebUI in background"
        echo "  stop     stop the scanner"
        echo "  restart  stop then start"
        echo "  status   show running state and last 5 log lines"
        echo "  log      tail -f the log"
        echo "  enable   install + start as a systemd service (auto-restart on boot/crash)"
        echo "  disable  stop and uninstall the systemd service"
        exit 1
        ;;
esac
