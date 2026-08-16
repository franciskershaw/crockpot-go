package email

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestResendClient(t *testing.T, handler http.HandlerFunc) *ResendClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &ResendClient{
		apiKey:     "test-api-key",
		fromEmail:  "noreply@example.com",
		apiURL:     server.URL,
		httpClient: server.Client(),
	}
}

func TestSendConfirmationCode_SendsExpectedRequest(t *testing.T) {
	var gotMethod, gotAuth, gotContentType string
	var gotBody map[string]string
	var decodeErr error

	client := newTestResendClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		decodeErr = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	})

	if err := client.SendConfirmationCode(context.Background(), "user@example.com", "482913"); err != nil {
		t.Fatalf("SendConfirmationCode returned unexpected error: %v", err)
	}

	if decodeErr != nil {
		t.Fatalf("failed to decode request body: %v", decodeErr)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want %q", gotMethod, http.MethodPost)
	}
	if gotAuth != "Bearer test-api-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-api-key")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
	if gotBody["from"] != "noreply@example.com" {
		t.Errorf("from = %q, want %q", gotBody["from"], "noreply@example.com")
	}
	if gotBody["to"] != "user@example.com" {
		t.Errorf("to = %q, want %q", gotBody["to"], "user@example.com")
	}
	if !strings.Contains(gotBody["html"], "482913") {
		t.Errorf("html body does not contain the code: %q", gotBody["html"])
	}
	if !strings.Contains(gotBody["text"], "482913") {
		t.Errorf("text body does not contain the code: %q", gotBody["text"])
	}
}

func TestSendConfirmationCode_ReturnsErrorOnNonSuccessStatus(t *testing.T) {
	client := newTestResendClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"statusCode":422,"message":"domain is not verified","name":"validation_error"}`))
	})

	err := client.SendConfirmationCode(context.Background(), "user@example.com", "482913")
	if err == nil {
		t.Fatal("expected an error for a non-2xx response, got nil")
	}
	if !strings.Contains(err.Error(), "domain is not verified") {
		t.Errorf("expected error to include Resend's response body, got: %v", err)
	}
}

func TestSendConfirmationCode_ReturnsErrorWhenRequestFails(t *testing.T) {
	client := newTestResendClient(t, func(w http.ResponseWriter, r *http.Request) {})
	client.apiURL = "http://127.0.0.1:0" // nothing listening here

	err := client.SendConfirmationCode(context.Background(), "user@example.com", "482913")
	if err == nil {
		t.Fatal("expected an error when the request can't be made, got nil")
	}
}
