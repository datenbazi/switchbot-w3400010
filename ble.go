package main

import (
	"math"
	"strings"
	"time"

	"tinygo.org/x/bluetooth"
)

const (
	companyID   uint16 = 0x0969
	serviceUUID uint16 = 0xFD3D
)

type reading struct {
	TS          int64   `json:"ts"`
	Time        string  `json:"time"`
	Address     string  `json:"address"`
	RSSI        int16   `json:"rssi"`
	Temperature float64 `json:"temperature_c"`
	Humidity    int     `json:"humidity_pct"`
	Battery     int     `json:"battery_pct"`
}

func readingNow(addr string, rssi int16, temp float64, humidity, battery int) reading {
	now := time.Now()
	return reading{
		TS:          now.Unix(),
		Time:        now.Format(time.RFC3339),
		Address:     addr,
		RSSI:        rssi,
		Temperature: temp,
		Humidity:    humidity,
		Battery:     battery,
	}
}

// decodeManufacturer extracts temperature and humidity from manufacturer-specific
// advertisement data broadcast by SwitchBot W3400010 devices.
// Payload layout (after 2-byte company ID 0x0969):
//
//	[0-5]  device MAC
//	[6-7]  internal flags
//	[8]    temperature decimal (low nibble × 0.1)
//	[9]    temperature integer (bits 6:0) + sign (bit 7: 1=positive)
//	[10]   humidity % (bits 6:0)
func decodeManufacturer(result bluetooth.ScanResult) (payload []byte, temp float64, humidity int, ok bool) {
	for _, mfg := range result.ManufacturerData() {
		if mfg.CompanyID != companyID {
			continue
		}
		p := mfg.Data
		if len(p) < 11 {
			continue
		}
		dec := float64(p[8]&0x0F) * 0.1
		intPart := float64(p[9] & 0x7F)
		sign := 1.0
		if p[9]&0x80 == 0 {
			sign = -1.0
		}
		t := math.Round((dec+intPart)*sign*10) / 10
		h := int(p[10] & 0x7F)
		return p, t, h, true
	}
	return nil, 0, 0, false
}

// decodeBattery extracts battery % from service data UUID 0xFD3D payload byte [2].
// Returns -1 if not present.
func decodeBattery(result bluetooth.ScanResult) int {
	for _, sd := range result.ServiceData() {
		if !strings.Contains(sd.UUID.String(), "fd3d") {
			continue
		}
		if len(sd.Data) >= 3 {
			return int(sd.Data[2] & 0x7F)
		}
	}
	return -1
}
