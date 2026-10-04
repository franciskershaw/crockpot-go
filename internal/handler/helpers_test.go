package handler_test

import (
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
