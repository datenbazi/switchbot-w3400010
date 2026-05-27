package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"tinygo.org/x/bluetooth"
)

var (
	flagJSON       = flag.Bool("json", false, "output one JSON object per line")
	flagDevice     = flag.String("device", "", "filter to single MAC address, e.g. AA:BB:CC:DD:EE:FF")
	flagTimeout    = flag.Duration("timeout", 0, "stop scanning after this duration (0 = run until Ctrl+C)")
	flagOnce       = flag.Bool("once", false, "print first reading per device then exit")
	flagVerbose    = flag.Bool("verbose", false, "print raw advertisement hex bytes to stderr")
	flagAll        = flag.Bool("all", false, "print every advertisement, including duplicates")
	flagDB         = flag.String("db", "switchbot.db", "SQLite database file (empty = disable)")
	flagStoreEvery = flag.Duration("store-every", 5*time.Minute, "force DB write this often even when values unchanged (0 = on change only)")
	flagListen     = flag.String("listen", ":7700", "WebUI listen address (empty = disable)")
	flagAlerts     = flag.String("alerts", "", "path to alerts config JSON (empty = disable)")
)

var adapter = bluetooth.DefaultAdapter

type deviceState struct {
	last      reading
	lastWrite time.Time
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: switchbot-temp [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Passively scans for SwitchBot W3400010 thermometers via BLE.\n")
		fmt.Fprintf(os.Stderr, "Run ./bt-up.sh first to ensure the adapter is powered on.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	deviceFilter := strings.ToUpper(*flagDevice)

	var db *sql.DB
	if *flagDB != "" {
		var err error
		db, err = openDB(*flagDB)
		if err != nil {
			fmt.Fprintf(os.Stderr, "db error: %v\n", err)
			os.Exit(1)
		}
		defer db.Close()
		if !*flagJSON {
			fmt.Fprintf(os.Stderr, "Storing readings in: %s\n", *flagDB)
		}
	}

	var alertMgr *AlertManager
	if *flagAlerts != "" {
		var err error
		alertMgr, err = loadAlerts(*flagAlerts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "alerts error: %v\n", err)
			os.Exit(1)
		}
		if !*flagJSON {
			fmt.Fprintf(os.Stderr, "Alerts loaded: %d rules\n", len(alertMgr.cfg.Rules))
		}
	}

	h := newHub()
	if *flagListen != "" {
		if db == nil {
			fmt.Fprintf(os.Stderr, "-listen requires -db (need a database for history)\n")
			os.Exit(1)
		}
		startServer(*flagListen, db, h, alertMgr)
	}

	if err := adapter.Enable(); err != nil {
		fmt.Fprintf(os.Stderr, "BLE adapter error: %v\n\nRun ./bt-up.sh first.\n", err)
		os.Exit(1)
	}

	if *flagTimeout > 0 {
		go func() {
			time.Sleep(*flagTimeout)
			adapter.StopScan()
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		adapter.StopScan()
	}()

	if !*flagJSON {
		fmt.Fprintln(os.Stderr, "Scanning for SwitchBot W3400010 devices... (Ctrl+C to stop)")
	}

	var stateMu sync.Mutex
	states := map[string]*deviceState{}
	onceSeen := map[string]bool{}

	err := adapter.Scan(func(a *bluetooth.Adapter, result bluetooth.ScanResult) {
		addr := result.Address.String()
		if deviceFilter != "" && strings.ToUpper(addr) != deviceFilter {
			return
		}

		payload, temp, humid, ok := decodeManufacturer(result)
		if !ok {
			return
		}
		battery := decodeBattery(result)

		if *flagVerbose {
			fmt.Fprintf(os.Stderr, "DBG %s mfg[%d]: % x\n", addr, len(payload), payload)
		}

		r := readingNow(addr, result.RSSI, temp, humid, battery)

		now := time.Now()
		stateMu.Lock()
		st, exists := states[addr]
		if !exists {
			st = &deviceState{}
			states[addr] = st
		}
		firstSeenThisSession := !exists
		valueChanged := !exists ||
			st.last.Temperature != r.Temperature ||
			st.last.Humidity != r.Humidity ||
			st.last.Battery != r.Battery
		intervalElapsed := *flagStoreEvery > 0 && now.Sub(st.lastWrite) >= *flagStoreEvery

		shouldPrint := *flagAll || valueChanged
		shouldStore := db != nil && (*flagAll || valueChanged || intervalElapsed)

		if shouldPrint || shouldStore {
			st.last = r
		}
		if shouldStore {
			st.lastWrite = now
		}
		stateMu.Unlock()

		// Register device on first sighting per session; notify if truly new (never seen before).
		if firstSeenThisSession && db != nil {
			if isNew, err := upsertDevice(db, addr, r.TS); err != nil {
				fmt.Fprintf(os.Stderr, "device register error: %v\n", err)
			} else if isNew {
				fmt.Fprintf(os.Stderr, "New device discovered: %s\n", addr)
				h.publishDeviceAdded(addr)
			}
		}

		if shouldStore {
			if err := storeReading(db, r); err != nil {
				fmt.Fprintf(os.Stderr, "db write error: %v\n", err)
			}
		}

		// Push every advertisement to SSE — real-time, independent of dedup/store logic.
		h.publish(r)

		if alertMgr != nil {
			alertMgr.Check(r)
		}

		if shouldPrint {
			if *flagJSON {
				b, _ := json.Marshal(r)
				fmt.Println(string(b))
			} else {
				batStr := fmt.Sprintf("%d%%", r.Battery)
				if r.Battery < 0 {
					batStr = "n/a"
				}
				fmt.Printf("[%s] %-17s  RSSI:%4d  %5.1f°C  %d%% RH  bat:%s\n",
					r.Time, r.Address, r.RSSI, r.Temperature, r.Humidity, batStr)
			}
		}

		if *flagOnce {
			stateMu.Lock()
			onceSeen[addr] = true
			count := len(onceSeen)
			stateMu.Unlock()
			if count >= 1 {
				adapter.StopScan()
			}
		}
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
		os.Exit(1)
	}
}
