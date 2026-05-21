package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/YuriyDubinin/digix-api/internal/domain"
)

const (
	apiBaseURL     = "https://api.telegram.org"
	requestTimeout = 10 * time.Second
)

type Client struct {
	token      string
	chatID     string
	httpClient *http.Client
}

func NewClient(token, chatID string) *Client {
	return &Client{
		token:      token,
		chatID:     chatID,
		httpClient: &http.Client{Timeout: requestTimeout},
	}
}

func (c *Client) NotifyNewFeedback(ctx context.Context, f *domain.FeedbackRequest) error {
	return c.sendMessage(ctx, buildFeedbackMessage(f))
}

type sendMessageRequest struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

type sendMessageResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
}

func (c *Client) sendMessage(ctx context.Context, text string) error {
	body, err := json.Marshal(sendMessageRequest{
		ChatID:                c.chatID,
		Text:                  text,
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
	})
	if err != nil {
		return fmt.Errorf("telegram: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", apiBaseURL, c.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: do request: %w", err)
	}
	defer resp.Body.Close()

	var payload sendMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("telegram: decode response: %w", err)
	}
	if !payload.OK {
		return fmt.Errorf("telegram: api error (http=%d, code=%d): %s",
			resp.StatusCode, payload.ErrorCode, payload.Description)
	}
	return nil
}

func buildFeedbackMessage(f *domain.FeedbackRequest) string {
	var b strings.Builder

	b.WriteString("📩 <b>Новая заявка</b>\n")
	fmt.Fprintf(&b, "<code>id: %s</code>\n\n", f.ID)

	fmt.Fprintf(&b, "👤 <b>Имя:</b> %s\n", html.EscapeString(f.Name))
	fmt.Fprintf(&b, "📧 <b>Email:</b> %s\n", html.EscapeString(f.Email))
	if f.Phone != "" {
		fmt.Fprintf(&b, "📞 <b>Телефон:</b> %s\n", html.EscapeString(f.Phone))
	}
	if f.Subject != "" {
		fmt.Fprintf(&b, "📝 <b>Тема:</b> %s\n", html.EscapeString(f.Subject))
	}

	b.WriteString("\n💬 <b>Сообщение:</b>\n")
	b.WriteString(html.EscapeString(f.Message))

	b.WriteString("\n\n🕒 ")
	b.WriteString(f.CreatedAt.UTC().Format("2006-01-02 15:04:05 UTC"))

	return b.String()
}
