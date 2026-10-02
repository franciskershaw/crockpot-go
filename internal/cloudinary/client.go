package cloudinary

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // Cloudinary's request-signature digest, not password storage.
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const apiBaseURL = "https://api.cloudinary.com"

type Client struct {
	cloudName  string
	apiKey     string
	apiSecret  string
	baseURL    string
	httpClient *http.Client
	now        func() time.Time
}

func NewClient(cloudName, apiKey, apiSecret string) *Client {
	return &Client{
		cloudName:  cloudName,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		baseURL:    apiBaseURL,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		now:        time.Now,
	}
}

type UploadedImage struct {
	SecureURL string
	PublicID  string
}

func (c *Client) Upload(ctx context.Context, file io.Reader, publicID string) (UploadedImage, error) {
	params := c.signedParams(map[string]string{
		"public_id":       publicID,
		"overwrite":       "false",
		"allowed_formats": "jpg,jpeg,png,webp",
		"transformation":  "c_limit,w_1600,h_1600",
	})

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range params {
		if err := mw.WriteField(k, v); err != nil {
			return UploadedImage{}, fmt.Errorf("cloudinary: write field %s: %w", k, err)
		}
	}
	part, err := mw.CreateFormFile("file", "upload")
	if err != nil {
		return UploadedImage{}, fmt.Errorf("cloudinary: create file part: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return UploadedImage{}, fmt.Errorf("cloudinary: copy file: %w", err)
	}
	if err := mw.Close(); err != nil {
		return UploadedImage{}, fmt.Errorf("cloudinary: close multipart: %w", err)
	}

	var resp struct {
		SecureURL string `json:"secure_url"`
		PublicID  string `json:"public_id"`
	}
	if err := c.post(ctx, "upload", mw.FormDataContentType(), &body, &resp); err != nil {
		return UploadedImage{}, err
	}
	return UploadedImage{SecureURL: resp.SecureURL, PublicID: resp.PublicID}, nil
}

func (c *Client) Destroy(ctx context.Context, publicID string) error {
	params := c.signedParams(map[string]string{
		"public_id":  publicID,
		"invalidate": "true",
	})
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	var resp struct {
		Result string `json:"result"`
	}
	if err := c.post(ctx, "destroy", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), &resp); err != nil {
		return err
	}
	// "not found" means the asset is already gone, which is what destroy is for.
	if resp.Result != "ok" && resp.Result != "not found" {
		return fmt.Errorf("cloudinary: destroy %s: result %q", publicID, resp.Result)
	}
	return nil
}

// signedParams adds timestamp, api_key and the signature: SHA-1 of the sorted name=value pairs joined by & with the secret appended.
func (c *Client) signedParams(params map[string]string) map[string]string {
	params["timestamp"] = strconv.FormatInt(c.now().Unix(), 10)

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = k + "=" + params[k]
	}
	sum := sha1.Sum([]byte(strings.Join(pairs, "&") + c.apiSecret)) //nolint:gosec // see import.

	params["signature"] = hex.EncodeToString(sum[:])
	params["api_key"] = c.apiKey
	return params
}

func (c *Client) post(ctx context.Context, action, contentType string, body io.Reader, out any) error {
	endpoint := fmt.Sprintf("%s/v1_1/%s/image/%s", c.baseURL, c.cloudName, action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("cloudinary: build %s request: %w", action, err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudinary: send %s request: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("cloudinary: read %s response: %w", action, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Error.Message != "" {
			return fmt.Errorf("cloudinary: %s: status %d: %s", action, resp.StatusCode, apiErr.Error.Message)
		}
		return fmt.Errorf("cloudinary: %s: status %d: %s", action, resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("cloudinary: decode %s response: %w", action, err)
	}
	return nil
}
