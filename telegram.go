package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type telegramClient struct {
	botToken string
	chatID   string
	http     *http.Client
}

func newTelegramClient(botToken, chatID string) *telegramClient {
	return &telegramClient{
		botToken: botToken,
		chatID:   chatID,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *telegramClient) send(ctx context.Context, text string) error {
	payload := map[string]string{
		"chat_id": c.chatID,
		"text":    text,
	}
	b, _ := json.Marshal(payload)

	url := "https://api.telegram.org/bot" + c.botToken + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var e struct {
			Description string `json:"description"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("telegram API: HTTP %d: %s", resp.StatusCode, e.Description)
	}
	return nil
}
