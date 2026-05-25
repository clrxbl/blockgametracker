package queue

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.cloudflare.com"

type Client struct {
	BaseURL   string
	AccountID string
	QueueID   string
	Token     string
	HTTP      *http.Client
}

func NewClient(accountID, queueID, token string) *Client {
	return &Client{
		BaseURL:   defaultBaseURL,
		AccountID: accountID,
		QueueID:   queueID,
		Token:     token,
		HTTP:      &http.Client{Timeout: 10 * time.Second},
	}
}

type pushBody struct {
	Body        string `json:"body"`
	ContentType string `json:"content_type"`
}

// Push sends a single message to the queue. The body is base64-encoded and
// sent with content_type=bytes so the consumer receives the raw flatbuffers
// payload. Returns an error on any non-2xx response or transport failure.
func (c *Client) Push(ctx context.Context, payload []byte) error {
	base := c.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	url := fmt.Sprintf(
		"%s/client/v4/accounts/%s/queues/%s/messages",
		base, c.AccountID, c.QueueID,
	)

	// content_type=text with a base64 string body. The Cloudflare HTTP API
	// rejects content_type=bytes; the backend consumer base64-decodes string
	// bodies before handing them to the flatbuffers decoder.
	jsonBody, err := json.Marshal(pushBody{
		Body:        base64.StdEncoding.EncodeToString(payload),
		ContentType: "text",
	})
	if err != nil {
		return fmt.Errorf("marshal push body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare push: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("cloudflare push: status %d: %s", resp.StatusCode, string(snippet))
	}
	return nil
}
