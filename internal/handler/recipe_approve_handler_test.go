package handler_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const approveSeenUpdatedAt = "2026-10-09T10:11:12.123456Z"

func doRecipeApprove(r *gin.Engine, id, body, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/recipes/"+id+"/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func approveBody() string {
	return `{"updatedAt":"` + approveSeenUpdatedAt + `"}`
}

func seenAt(t *testing.T) any {
	t.Helper()
	seen, err := time.Parse(time.RFC3339Nano, approveSeenUpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return mock.MatchedBy(func(got time.Time) bool { return got.Equal(seen) })
}

func TestRecipeApprove_NoToken_401(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeApprove(m.router, recipeID.String(), approveBody(), "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRecipeApprove_NonAdmin_403(t *testing.T) {
	for _, role := range []string{"FREE", "PREMIUM"} {
		t.Run(role, func(t *testing.T) {
			m := newRecipeMocks(t)
			w := doRecipeApprove(m.router, recipeID.String(), approveBody(), recipeAuth(t, role))
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}

func TestRecipeApprove_InvalidID_400(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeApprove(m.router, "not-a-uuid", approveBody(), recipeAuth(t, "ADMIN"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", decodeJSONBody(t, w)["error"])
}

func TestRecipeApprove_BadBody_400(t *testing.T) {
	cases := map[string]string{
		"empty body":          "",
		"no updatedAt":        `{}`,
		"null updatedAt":      `{"updatedAt":null}`,
		"not a timestamp":     `{"updatedAt":"yesterday"}`,
		"date without a time": `{"updatedAt":"2026-10-09"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			m := newRecipeMocks(t)
			w := doRecipeApprove(m.router, recipeID.String(), body, recipeAuth(t, "ADMIN"))
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "invalid_request", decodeJSONBody(t, w)["error"])
		})
	}
}

func TestRecipeApprove_Success_200_InTransaction(t *testing.T) {
	m := newRecipeMocks(t)
	approved := fakeCreatedRecipe()
	approved.Approved = true
	m.repo.EXPECT().Approve(mock.Anything, recipeID.String(), recipeUserID.String(), seenAt(t)).
		Return(approved, nil)

	w := doRecipeApprove(m.router, recipeID.String(), approveBody(), recipeAuth(t, "ADMIN"))

	assert.Equal(t, http.StatusOK, w.Code)
	body := decodeJSONBodyAny(t, w)
	assert.Equal(t, approved.ID.String(), body["id"])
	assert.Equal(t, true, body["approved"])
	assert.Contains(t, body, "ingredients", "approve returns the detail shape")
	assert.True(t, m.committed, "the approve must run inside a transaction so its row lock holds until the update")
}

func TestRecipeApprove_RepoErrorTranslation(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantErr  string
	}{
		{"changed since loaded", models.ErrRecipeChanged, http.StatusConflict, "recipe_changed"},
		{"unknown recipe", models.ErrRecipeNotFound, http.StatusNotFound, "not_found"},
		{"unexpected", errors.New("db down"), http.StatusInternalServerError, "server_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)
			m.repo.EXPECT().Approve(mock.Anything, recipeID.String(), recipeUserID.String(), seenAt(t)).
				Return(nil, tc.err)

			w := doRecipeApprove(m.router, recipeID.String(), approveBody(), recipeAuth(t, "ADMIN"))

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantErr, decodeJSONBody(t, w)["error"])
		})
	}
}
