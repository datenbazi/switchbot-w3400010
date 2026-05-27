package main

import (
	"database/sql"
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

//go:embed ui/index.html
var uiFS embed.FS

func serveUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := uiFS.ReadFile("ui/index.html")
	if err != nil {
		http.Error(w, "ui not found", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// sseMsg carries a single SSE frame: an optional named event and its JSON payload.
type sseMsg struct {
	event string
	data  []byte
}

// hub fans out new readings to all connected SSE clients and caches the latest
// value per device so new clients receive an immediate snapshot on connect.
type hub struct {
	mu      sync.RWMutex
	clients map[chan sseMsg]struct{}
	latest  map[string]reading // address → most recent reading
}

func newHub() *hub {
	return &hub{
		clients: make(map[chan sseMsg]struct{}),
		latest:  make(map[string]reading),
	}
}

func (h *hub) subscribe() chan sseMsg {
	ch := make(chan sseMsg, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(ch chan sseMsg) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *hub) publish(r reading) {
	b, _ := json.Marshal(r)
	msg := sseMsg{data: b}
	h.mu.Lock()
	h.latest[r.Address] = r
	for ch := range h.clients {
		select {
		case ch <- msg:
		default: // slow client — drop rather than block
		}
	}
	h.mu.Unlock()
}

func (h *hub) publishDeviceAdded(address string) {
	b, _ := json.Marshal(map[string]string{"address": address})
	msg := sseMsg{event: "device_added", data: b}
	h.mu.Lock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
	h.mu.Unlock()
}

func startServer(addr string, db *sql.DB, h *hub, alerts *AlertManager) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveUI)
	mux.HandleFunc("/api/latest", apiLatest(db))
	mux.HandleFunc("/api/history", apiHistory(db))
	mux.HandleFunc("/api/devices", apiDevices(db))
	mux.HandleFunc("/events", sseHandler(h))
	if alerts != nil {
		mux.HandleFunc("/api/alerts", alerts.apiStatusHandler())
		mux.HandleFunc("/api/alerts/rules", alerts.apiRulesHandler())
	}
	mux.HandleFunc("/api/export", apiExport(db))
	mux.HandleFunc("/api/export/count", apiExportCount(db))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WebUI: cannot bind %s: %v\n", addr, err)
		os.Exit(1)
	}
	fmt.Printf("WebUI listening on http://%s\n", addr)
	go http.Serve(ln, mux)
}

func apiLatest(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := queryLatest(db)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if rows == nil {
			rows = []reading{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rows)
	}
}

func apiHistory(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hoursStr := r.URL.Query().Get("hours")
		hours := 24.0
		if hoursStr != "" {
			if v, err := strconv.ParseFloat(hoursStr, 64); err == nil && v > 0 {
				hours = v
			}
		}
		address := r.URL.Query().Get("address")
		since := time.Duration(hours * float64(time.Hour))

		rows, err := queryHistory(db, since, address)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if rows == nil {
			rows = []reading{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rows)
	}
}

// parseExportParams extracts from, to (Unix seconds) and address from query params.
// Accepts ?from=<unix>&to=<unix>&address=<mac>  OR  ?hours=<N> as a shortcut.
func parseExportParams(r *http.Request) (from, to int64, address string) {
	q := r.URL.Query()
	address = q.Get("address")

	if h := q.Get("hours"); h != "" {
		if v, err := strconv.ParseFloat(h, 64); err == nil && v > 0 {
			from = time.Now().Add(-time.Duration(v * float64(time.Hour))).Unix()
		}
		return
	}
	if s := q.Get("from"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			from = v
		}
	}
	if s := q.Get("to"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			to = v
		}
	}
	return
}

func apiExport(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, address := parseExportParams(r)
		format := r.URL.Query().Get("format")
		if format == "" {
			format = "json"
		}

		rows, err := exportRows(db, from, to, address)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()

		now := time.Now().Format("2006-01-02")
		switch format {
		case "csv":
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="switchbot-%s.csv"`, now))
			cw := csv.NewWriter(w)
			cw.Write([]string{"ts", "time", "address", "rssi", "temperature_c", "humidity_pct", "battery_pct"})
			for rows.Next() {
				var rd reading
				var ts int64
				if err := rows.Scan(&ts, &rd.Address, &rd.RSSI, &rd.Temperature, &rd.Humidity, &rd.Battery); err != nil {
					break
				}
				cw.Write([]string{
					strconv.FormatInt(ts, 10),
					time.Unix(ts, 0).UTC().Format(time.RFC3339),
					rd.Address,
					strconv.FormatInt(int64(rd.RSSI), 10),
					strconv.FormatFloat(rd.Temperature, 'f', 1, 64),
					strconv.Itoa(rd.Humidity),
					strconv.Itoa(rd.Battery),
				})
			}
			cw.Flush()
		default: // json
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="switchbot-%s.json"`, now))
			w.Write([]byte("["))
			first := true
			for rows.Next() {
				var rd reading
				var ts int64
				if err := rows.Scan(&ts, &rd.Address, &rd.RSSI, &rd.Temperature, &rd.Humidity, &rd.Battery); err != nil {
					break
				}
				rd.TS = ts
				rd.Time = time.Unix(ts, 0).UTC().Format(time.RFC3339)
				if !first {
					w.Write([]byte(","))
				}
				first = false
				b, _ := json.Marshal(rd)
				w.Write(b)
			}
			w.Write([]byte("]"))
		}
	}
}

func apiExportCount(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, address := parseExportParams(r)
		n, err := countExport(db, from, to, address)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"count":%d}`, n)
	}
}

func apiDevices(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		devs, err := queryDevices(db)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if devs == nil {
			devs = []deviceInfo{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(devs)
	}
}

func sseHandler(h *hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", 500)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Subscribe first, then send snapshot — no gap between the two.
		ch := h.subscribe()
		defer h.unsubscribe(ch)

		// Flush latest value for each device so the page is non-empty immediately.
		h.mu.RLock()
		for _, rd := range h.latest {
			b, _ := json.Marshal(rd)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		h.mu.RUnlock()
		flusher.Flush()

		for {
			select {
			case msg, open := <-ch:
				if !open {
					return
				}
				if msg.event != "" {
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.event, msg.data)
				} else {
					fmt.Fprintf(w, "data: %s\n\n", msg.data)
				}
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}
