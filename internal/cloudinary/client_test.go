package cloudinary

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Expected signatures computed outside Go (Python hashlib.sha1) from the sorted params + "test-secret".
const (
	wantUploadSignature  = "532c8a288f9a0e85b7702f79c97d6e2960034a40"
	wantDestroySignature = "d195e5c54640efd173b98a59a086f6a8279c6a4e"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &Client{
		cloudName:  "test-cloud",
		apiKey:     "test-key",
		apiSecret:  "test-secret",
		baseURL:    server.URL,
		httpClient: server.Client(),
		now:        func() time.Time { return time.Unix(1700000000, 0) },
	}
}

func TestUpload_SendsSignedMultipartRequest(t *testing.T) {
	var gotMethod, gotPath, gotFile string
	gotFields := map[string]string{}
	var parseErr error

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if parseErr = r.ParseMultipartForm(1 << 20); parseErr != nil {
			return
		}
		for k, v := range r.MultipartForm.Value {
			gotFields[k] = v[0]
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			parseErr = err
			return
		}
		b, _ := io.ReadAll(f)
		gotFile = string(b)
		_, _ = w.Write([]byte(`{"secure_url":"https://res.cloudinary.com/test-cloud/image/upload/v1/dev/recipes/abc.jpg","public_id":"dev/recipes/abc"}`))
	})

	got, err := client.Upload(context.Background(), strings.NewReader("image-bytes"), "dev/recipes/abc")
	if err != nil {
		t.Fatalf("Upload returned unexpected error: %v", err)
	}
	if parseErr != nil {
		t.Fatalf("server failed to parse multipart request: %v", parseErr)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1_1/test-cloud/image/upload" {
		t.Errorf("path = %q, want /v1_1/test-cloud/image/upload", gotPath)
	}
	if gotFile != "image-bytes" {
		t.Errorf("file = %q, want %q", gotFile, "image-bytes")
	}
	wantFields := map[string]string{
		"api_key":         "test-key",
		"timestamp":       "1700000000",
		"public_id":       "dev/recipes/abc",
		"overwrite":       "false",
		"allowed_formats": "jpg,jpeg,png,webp",
		"transformation":  "c_limit,w_1600,h_1600",
		"signature":       wantUploadSignature,
	}
	for k, want := range wantFields {
		if gotFields[k] != want {
			t.Errorf("field %s = %q, want %q", k, gotFields[k], want)
		}
	}
	if len(gotFields) != len(wantFields) {
		t.Errorf("sent %d fields %v, want exactly %d", len(gotFields), gotFields, len(wantFields))
	}
	want := UploadedImage{
		SecureURL: "https://res.cloudinary.com/test-cloud/image/upload/v1/dev/recipes/abc.jpg",
		PublicID:  "dev/recipes/abc",
	}
	if got != want {
		t.Errorf("Upload = %+v, want %+v", got, want)
	}
}

func TestUpload_ErrorStatusReturnsCloudinaryMessage(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Image file format txt not allowed"}}`))
	})

	_, err := client.Upload(context.Background(), strings.NewReader("x"), "dev/recipes/abc")
	if err == nil {
		t.Fatal("expected an error for a 400 response")
	}
	if !strings.Contains(err.Error(), "Image file format txt not allowed") {
		t.Errorf("error = %q, want it to include Cloudinary's message", err)
	}
}

func TestDestroy_SendsSignedInvalidatingRequest(t *testing.T) {
	var gotMethod, gotPath string
	gotFields := map[string]string{}
	var parseErr error

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if parseErr = r.ParseForm(); parseErr != nil {
			return
		}
		for k, v := range r.PostForm {
			gotFields[k] = v[0]
		}
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	})

	if err := client.Destroy(context.Background(), "dev/recipes/abc"); err != nil {
		t.Fatalf("Destroy returned unexpected error: %v", err)
	}
	if parseErr != nil {
		t.Fatalf("server failed to parse form: %v", parseErr)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1_1/test-cloud/image/destroy" {
		t.Errorf("path = %q, want /v1_1/test-cloud/image/destroy", gotPath)
	}
	wantFields := map[string]string{
		"api_key":    "test-key",
		"timestamp":  "1700000000",
		"public_id":  "dev/recipes/abc",
		"invalidate": "true",
		"signature":  wantDestroySignature,
	}
	for k, want := range wantFields {
		if gotFields[k] != want {
			t.Errorf("field %s = %q, want %q", k, gotFields[k], want)
		}
	}
	if len(gotFields) != len(wantFields) {
		t.Errorf("sent %d fields %v, want exactly %d", len(gotFields), gotFields, len(wantFields))
	}
}

func TestDestroy_Results(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"already gone counts as destroyed", http.StatusOK, `{"result":"not found"}`, false},
		{"unexpected result is an error", http.StatusOK, `{"result":"error"}`, true},
		{"error status is an error", http.StatusUnauthorized, `{"error":{"message":"Invalid Signature"}}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})

			err := client.Destroy(context.Background(), "dev/recipes/abc")
			if requests != 1 {
				t.Fatalf("server received %d requests, want 1", requests)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("Destroy error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
