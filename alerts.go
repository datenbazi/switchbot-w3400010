package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AlertConfig is loaded from the -alerts JSON file.
type AlertConfig struct {
	BotToken string      `json:"telegram_bot_token"`
	ChatID   string      `json:"telegram_chat_id"`
	Rules    []AlertRule `json:"rules"`
}

// AlertRule defines a single threshold condition.
type AlertRule struct {
	Name      string  `json:"name"`
	Device    string  `json:"device"`    // MAC address or "*" for all devices
	Metric    string  `json:"metric"`    // "temperature_c" or "humidity_pct"
	Condition string  `json:"condition"` // "above" or "below"
	Threshold float64 `json:"threshold"`
	For       string  `json:"for"`      // minimum sustained duration before firing, e.g. "5m"
	Cooldown  string  `json:"cooldown"` // min time between repeated alerts, default "30m"

	forDur      time.Duration
	cooldownDur time.Duration
}

type alertState struct {
	condSince time.Time // when the current condition streak started; zero = not active
	firedAt   time.Time // when we last sent an alert for this rule+device
}

// AlertStatus is the wire format returned by the /api/alerts endpoint.
type AlertStatus struct {
	RuleName    string  `json:"rule_name"`
	Device      string  `json:"device"`
	Metric      string  `json:"metric"`
	Condition   string  `json:"condition"`
	Threshold   float64 `json:"threshold"`
	ForStr      string  `json:"for"`
	CooldownStr string  `json:"cooldown"`
	Active      bool    `json:"active"`
	CondSince   string  `json:"cond_since,omitempty"`  // RFC3339
	CondForSecs float64 `json:"cond_for_secs"`
	FiredAt     string  `json:"fired_at,omitempty"` // RFC3339
}

// AlertRuleInfo is returned by /api/alerts/rules.
type AlertRuleInfo struct {
	Name      string  `json:"name"`
	Device    string  `json:"device"`
	Metric    string  `json:"metric"`
	Condition string  `json:"condition"`
	Threshold float64 `json:"threshold"`
	For       string  `json:"for"`
	Cooldown  string  `json:"cooldown"`
}

// AlertManager evaluates rules on each reading and sends Telegram notifications.
type AlertManager struct {
	cfg    AlertConfig
	client *telegramClient
	states map[string]*alertState // key: "<ruleIdx>:<address>"
	mu     sync.Mutex
}

func loadAlerts(path string) (*AlertManager, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg AlertConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	for i := range cfg.Rules {
		r := &cfg.Rules[i]
		if r.For != "" {
			d, err := time.ParseDuration(r.For)
			if err != nil {
				return nil, fmt.Errorf("rule %q: invalid 'for' %q: %w", r.Name, r.For, err)
			}
			r.forDur = d
		}
		cooldownStr := r.Cooldown
		if cooldownStr == "" {
			cooldownStr = "30m"
			r.Cooldown = cooldownStr
		}
		d, err := time.ParseDuration(cooldownStr)
		if err != nil {
			return nil, fmt.Errorf("rule %q: invalid cooldown %q: %w", r.Name, cooldownStr, err)
		}
		r.cooldownDur = d
	}

	return &AlertManager{
		cfg:    cfg,
		client: newTelegramClient(cfg.BotToken, cfg.ChatID),
		states: make(map[string]*alertState),
	}, nil
}

// Check evaluates all rules against a new reading. Called on every BLE advertisement.
func (m *AlertManager) Check(r reading) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, rule := range m.cfg.Rules {
		if rule.Device != "*" && !strings.EqualFold(rule.Device, r.Address) {
			continue
		}

		var val float64
		switch rule.Metric {
		case "temperature_c":
			val = r.Temperature
		case "humidity_pct":
			val = float64(r.Humidity)
		default:
			continue
		}

		condMet := (rule.Condition == "above" && val > rule.Threshold) ||
			(rule.Condition == "below" && val < rule.Threshold)

		key := fmt.Sprintf("%d:%s", i, r.Address)
		st, exists := m.states[key]
		if !exists {
			st = &alertState{}
			m.states[key] = st
		}

		if condMet {
			if st.condSince.IsZero() {
				st.condSince = now
			}
			dur := now.Sub(st.condSince)
			cooldownOK := st.firedAt.IsZero() || now.Sub(st.firedAt) >= rule.cooldownDur
			if dur >= rule.forDur && cooldownOK {
				st.firedAt = now
				go m.sendAlert(rule, r, val, dur)
			}
		} else {
			if !st.condSince.IsZero() {
				if !st.firedAt.IsZero() {
					go m.sendRecovery(rule, r, val)
				}
				st.condSince = time.Time{}
			}
		}
	}
}

// Statuses returns the current evaluation state of all rules for all known devices.
func (m *AlertManager) Statuses() []AlertStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	out := []AlertStatus{}
	for key, st := range m.states {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		ruleIdx, err := strconv.Atoi(parts[0])
		if err != nil || ruleIdx >= len(m.cfg.Rules) {
			continue
		}
		addr := parts[1]
		rule := m.cfg.Rules[ruleIdx]

		status := AlertStatus{
			RuleName:    rule.Name,
			Device:      addr,
			Metric:      rule.Metric,
			Condition:   rule.Condition,
			Threshold:   rule.Threshold,
			ForStr:      rule.For,
			CooldownStr: rule.Cooldown,
			Active:      !st.condSince.IsZero(),
		}
		if !st.condSince.IsZero() {
			status.CondSince = st.condSince.Format(time.RFC3339)
			status.CondForSecs = now.Sub(st.condSince).Seconds()
		}
		if !st.firedAt.IsZero() {
			status.FiredAt = st.firedAt.Format(time.RFC3339)
		}
		out = append(out, status)
	}
	return out
}

func (m *AlertManager) sendAlert(rule AlertRule, r reading, val float64, dur time.Duration) {
	unit := metricUnit(rule.Metric)
	label := metricLabel(rule.Metric)
	direction := "exceeded"
	if rule.Condition == "below" {
		direction = "undershot"
	}
	text := fmt.Sprintf(
		"🚨 ALERT: %s\n\nDevice: %s\n%s %s %.1f%s (threshold: %.1f%s)\nCondition for: %s\nTime: %s",
		rule.Name,
		r.Address,
		label, direction, val, unit,
		rule.Threshold, unit,
		dur.Round(time.Second),
		time.Now().Format("2006-01-02 15:04:05"),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.client.send(ctx, text); err != nil {
		fmt.Fprintf(os.Stderr, "alert send: %v\n", err)
	}
}

func (m *AlertManager) sendRecovery(rule AlertRule, r reading, val float64) {
	unit := metricUnit(rule.Metric)
	label := metricLabel(rule.Metric)
	text := fmt.Sprintf(
		"✅ RESOLVED: %s\n\nDevice: %s\n%s back to normal: %.1f%s\nTime: %s",
		rule.Name,
		r.Address,
		label, val, unit,
		time.Now().Format("2006-01-02 15:04:05"),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.client.send(ctx, text); err != nil {
		fmt.Fprintf(os.Stderr, "alert recover: %v\n", err)
	}
}

func (m *AlertManager) apiStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		statuses := m.Statuses()
		if statuses == nil {
			statuses = []AlertStatus{}
		}
		json.NewEncoder(w).Encode(statuses)
	}
}

func (m *AlertManager) apiRulesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		m.mu.Lock()
		rules := make([]AlertRuleInfo, len(m.cfg.Rules))
		for i, r := range m.cfg.Rules {
			rules[i] = AlertRuleInfo{
				Name:      r.Name,
				Device:    r.Device,
				Metric:    r.Metric,
				Condition: r.Condition,
				Threshold: r.Threshold,
				For:       r.For,
				Cooldown:  r.Cooldown,
			}
		}
		m.mu.Unlock()
		json.NewEncoder(w).Encode(rules)
	}
}

func metricLabel(metric string) string {
	if metric == "temperature_c" {
		return "Temperature"
	}
	return "Humidity"
}

func metricUnit(metric string) string {
	if metric == "temperature_c" {
		return "°C"
	}
	return "%"
}
