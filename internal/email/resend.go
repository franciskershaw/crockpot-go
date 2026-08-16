package email

import (
	"context"
	"net/http"
)

const resendAPIURL = "https://api.resend.com/emails"

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
		httpClient: http.DefaultClient,
	}
}

func (c *ResendClient) SendConfirmationCode(ctx context.Context, toEmail, code string) error {
	return nil
}
