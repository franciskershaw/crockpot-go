package email

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"io"
	"net/http"
	texttemplate "text/template"
	"time"
)

const resendAPIURL = "https://api.resend.com/emails"

//go:embed templates/confirmation.html templates/confirmation.txt templates/reset.html templates/reset.txt
var templateFS embed.FS

var (
	confirmationHTMLTemplate = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/confirmation.html"))
	confirmationTextTemplate = texttemplate.Must(texttemplate.ParseFS(templateFS, "templates/confirmation.txt"))
	resetHTMLTemplate        = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/reset.html"))
	resetTextTemplate        = texttemplate.Must(texttemplate.ParseFS(templateFS, "templates/reset.txt"))
)

type confirmationEmailData struct {
	Code string
}

type passwordResetEmailData struct {
	ResetURL string
}

type ResendClient struct {
	apiKey     string
	fromEmail  string
	apiURL     string
	httpClient *http.Client
}

func NewResendClient(apiKey, fromEmail string) *ResendClient {
	return &ResendClient{
		apiKey:     apiKey,
		fromEmail:  fromEmail,
		apiURL:     resendAPIURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type resendEmailRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

func (c *ResendClient) SendConfirmationCode(ctx context.Context, toEmail, code string) error {
	data := confirmationEmailData{Code: code}

	var html, text bytes.Buffer
	if err := confirmationHTMLTemplate.Execute(&html, data); err != nil {
		return fmt.Errorf("resend: render html template: %w", err)
	}
	if err := confirmationTextTemplate.Execute(&text, data); err != nil {
		return fmt.Errorf("resend: render text template: %w", err)
	}

	body, err := json.Marshal(resendEmailRequest{
		From:    c.fromEmail,
		To:      toEmail,
		Subject: "Your Crockpot confirmation code",
		HTML:    html.String(),
		Text:    text.String(),
	})
	if err != nil {
		return fmt.Errorf("resend: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("resend: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("resend: send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("resend: unexpected status %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (c *ResendClient) SendPasswordResetLink(ctx context.Context, toEmail, resetURL string) error {
	data := passwordResetEmailData{ResetURL: resetURL}

	var html, text bytes.Buffer
	if err := resetHTMLTemplate.Execute(&html, data); err != nil {
		return fmt.Errorf("resend: render html template: %w", err)
	}
	if err := resetTextTemplate.Execute(&text, data); err != nil {
		return fmt.Errorf("resend: render text template: %w", err)
	}

	body, err := json.Marshal(resendEmailRequest{
		From:    c.fromEmail,
		To:      toEmail,
		Subject: "Reset your Crockpot password",
		HTML:    html.String(),
		Text:    text.String(),
	})
	if err != nil {
		return fmt.Errorf("resend: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("resend: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("resend: send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("resend: unexpected status %d: %s", resp.StatusCode, body)
	}
	return nil
}
