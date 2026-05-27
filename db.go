package main

import (
	"database/sql"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS readings (
			id       INTEGER PRIMARY KEY,
			ts       INTEGER NOT NULL,
			address  TEXT    NOT NULL,
			rssi     INTEGER,
			temp     REAL    NOT NULL,
			humidity INTEGER NOT NULL,
			battery  INTEGER
		);
		CREATE INDEX IF NOT EXISTS readings_ts      ON readings(ts);
		CREATE INDEX IF NOT EXISTS readings_addr_ts ON readings(address, ts);
		CREATE TABLE IF NOT EXISTS devices (
			address    TEXT PRIMARY KEY,
			first_seen INTEGER NOT NULL,
			last_seen  INTEGER NOT NULL
		);
	`)
	return db, err
}

// upsertDevice records a device address. Returns true if this is the first time it has ever been seen.
func upsertDevice(db *sql.DB, address string, ts int64) (bool, error) {
	res, err := db.Exec(
		`INSERT OR IGNORE INTO devices (address, first_seen, last_seen) VALUES (?, ?, ?)`,
		address, ts, ts,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		_, err = db.Exec(`UPDATE devices SET last_seen = ? WHERE address = ? AND last_seen < ?`, ts, address, ts)
		return false, err
	}
	return true, nil
}

type deviceInfo struct {
	Address   string `json:"address"`
	FirstSeen int64  `json:"first_seen"`
	LastSeen  int64  `json:"last_seen"`
}

func queryDevices(db *sql.DB) ([]deviceInfo, error) {
	rows, err := db.Query(`SELECT address, first_seen, last_seen FROM devices ORDER BY first_seen`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deviceInfo
	for rows.Next() {
		var d deviceInfo
		if err := rows.Scan(&d.Address, &d.FirstSeen, &d.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func storeReading(db *sql.DB, r reading) error {
	_, err := db.Exec(
		`INSERT INTO readings (ts, address, rssi, temp, humidity, battery) VALUES (?, ?, ?, ?, ?, ?)`,
		r.TS, r.Address, r.RSSI, r.Temperature, r.Humidity, r.Battery,
	)
	return err
}

// queryLatest returns the most recent reading for each known device.
func queryLatest(db *sql.DB) ([]reading, error) {
	rows, err := db.Query(`
		SELECT ts, address, rssi, temp, humidity, battery
		FROM readings
		WHERE id IN (SELECT max(id) FROM readings GROUP BY address)
		ORDER BY address
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReadings(rows)
}

// queryHistory returns all readings within the given duration for all devices,
// or filtered to one device if address != "".
func queryHistory(db *sql.DB, since time.Duration, address string) ([]reading, error) {
	cutoff := time.Now().Add(-since).Unix()
	var rows *sql.Rows
	var err error
	if address == "" {
		rows, err = db.Query(
			`SELECT ts, address, rssi, temp, humidity, battery FROM readings WHERE ts >= ? ORDER BY ts`,
			cutoff,
		)
	} else {
		rows, err = db.Query(
			`SELECT ts, address, rssi, temp, humidity, battery FROM readings WHERE ts >= ? AND address = ? ORDER BY ts`,
			cutoff, address,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReadings(rows)
}

// exportRows returns a live *sql.Rows cursor for streaming exports.
// from/to are Unix seconds; 0 means unbounded. address "" means all devices.
// Caller must close the returned rows.
func exportRows(db *sql.DB, from, to int64, address string) (*sql.Rows, error) {
	conds := []string{}
	args := []interface{}{}
	if from > 0 {
		conds = append(conds, "ts >= ?")
		args = append(args, from)
	}
	if to > 0 {
		conds = append(conds, "ts <= ?")
		args = append(args, to)
	}
	if address != "" {
		conds = append(conds, "address = ?")
		args = append(args, address)
	}
	q := "SELECT ts, address, rssi, temp, humidity, battery FROM readings"
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY ts"
	return db.Query(q, args...)
}

// countExport returns the number of rows that exportRows would return.
func countExport(db *sql.DB, from, to int64, address string) (int64, error) {
	conds := []string{}
	args := []interface{}{}
	if from > 0 {
		conds = append(conds, "ts >= ?")
		args = append(args, from)
	}
	if to > 0 {
		conds = append(conds, "ts <= ?")
		args = append(args, to)
	}
	if address != "" {
		conds = append(conds, "address = ?")
		args = append(args, address)
	}
	q := "SELECT COUNT(*) FROM readings"
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	var n int64
	err := db.QueryRow(q, args...).Scan(&n)
	return n, err
}

func scanReadings(rows *sql.Rows) ([]reading, error) {
	var out []reading
	for rows.Next() {
		var r reading
		var ts int64
		if err := rows.Scan(&ts, &r.Address, &r.RSSI, &r.Temperature, &r.Humidity, &r.Battery); err != nil {
			return nil, err
		}
		r.TS = ts
		r.Time = time.Unix(ts, 0).UTC().Format(time.RFC3339)
		out = append(out, r)
	}
	return out, rows.Err()
}
