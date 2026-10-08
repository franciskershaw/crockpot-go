package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func decodeJSONBody(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

// decodeJSONBodyAny handles responses with non-string fields (e.g. retryAfterSeconds), where decodeJSONBody's map[string]string would fail to unmarshal.
func decodeJSONBodyAny(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

// jsonRequestBody sends a string as-is (for malformed-body cases) and JSON-encodes anything else.
func jsonRequestBody(body any) *bytes.Reader {
	if raw, ok := body.(string); ok {
		return bytes.NewReader([]byte(raw))
	}
	b, _ := json.Marshal(body)
	return bytes.NewReader(b)
}

// inTxKey marks a context as inside the mock transactor's WithinTx, so a test can assert a call ran in the transaction.
type inTxKey struct{}

func inTx(ctx context.Context) bool { return ctx.Value(inTxKey{}) != nil }
