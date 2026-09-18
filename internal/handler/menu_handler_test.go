package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/franciskershaw/crockpot-go/internal/handler"
	genmocks "github.com/franciskershaw/crockpot-go/internal/handler/mocks"
	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var menuUserID = uuid.MustParse("d4d4d4d4-d4d4-d4d4-d4d4-d4d4d4d4d4d4")

type menuMocks struct {
	repo          *genmocks.MockMenuRepository
	shoppingLists *genmocks.MockShoppingListRegenerator
	transactor    *genmocks.MockTransactor
	router        *gin.Engine
}

func newMenuMocks(t *testing.T) *menuMocks {
	m := &menuMocks{
		repo:          genmocks.NewMockMenuRepository(t),
		shoppingLists: genmocks.NewMockShoppingListRegenerator(t),
		transactor:    genmocks.NewMockTransactor(t),
	}
	m.transactor.EXPECT().WithinTx(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }).
		Maybe()
	h := handler.NewMenuHandler(m.repo, m.shoppingLists, m.transactor)
	m.router = gin.New()
	authed := m.router.Group("/menu")
	authed.Use(middleware.AuthMiddleware(testutil.TestAccessSecret))
	{
		authed.GET("", h.Get)
		authed.POST("/entries", h.UpsertEntry)
		authed.PATCH("/entries/:recipeId", h.UpdateEntryServes)
		authed.DELETE("/entries/:recipeId", h.RemoveEntry)
		authed.DELETE("", h.ClearMenu)
	}
	return m
}

func menuAuth(t *testing.T, role string) string {
	t.Helper()
	return testutil.AuthHeader(t, "cook@example.com", menuUserID.String(), role)
}

func doMenuGet(r *gin.Engine, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/menu", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doMenuUpsert(r *gin.Engine, body any, auth string) *httptest.ResponseRecorder {
	var reqBody *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(http.MethodPost, "/menu/entries", reqBody)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doMenuPatch(r *gin.Engine, recipeID string, body any, auth string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/menu/entries/"+recipeID, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doMenuDelete(r *gin.Engine, recipeID, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/menu/entries/"+recipeID, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doMenuClear(r *gin.Engine, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/menu", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func menuMsg(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body["message"]
}

func menuErr(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body["error"]
}

func fakeMenu() *models.Menu {
	return &models.Menu{
		Entries: []models.MenuEntry{
			{
				RecipeID: uuid.MustParse("c3c3c3c3-c3c3-c3c3-c3c3-c3c3c3c3c3c3"),
				Serves:   6,
				Recipe: &models.RecipeCard{
					ID:            uuid.MustParse("c3c3c3c3-c3c3-c3c3-c3c3-c3c3c3c3c3c3"),
					Name:          "Slow Cooker Beef Stew",
					TimeInMinutes: 240,
					Serves:        4,
					Categories:    []models.CategoryRef{},
				},
			},
		},
	}
}

// GET /menu

func TestMenuGet_NoToken_401(t *testing.T) {
	m := newMenuMocks(t)
	w := doMenuGet(m.router, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMenuGet_Success_200Envelope(t *testing.T) {
	m := newMenuMocks(t)
	m.repo.EXPECT().GetMenu(mock.Anything, menuUserID.String()).Return(fakeMenu(), nil)

	w := doMenuGet(m.router, menuAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)

	var body models.Menu
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Entries, 1)
	assert.Equal(t, 6, body.Entries[0].Serves)
}

func TestMenuGet_EmptyEntriesSerializesAsArray(t *testing.T) {
	m := newMenuMocks(t)
	m.repo.EXPECT().GetMenu(mock.Anything, menuUserID.String()).Return(&models.Menu{Entries: []models.MenuEntry{}}, nil)

	w := doMenuGet(m.router, menuAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"entries":[]}`, w.Body.String())
}

func TestMenuGet_RepoError_500(t *testing.T) {
	m := newMenuMocks(t)
	m.repo.EXPECT().GetMenu(mock.Anything, menuUserID.String()).Return(nil, errors.New("db down"))

	w := doMenuGet(m.router, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// POST /menu/entries

func TestMenuUpsertEntry_NoToken_401(t *testing.T) {
	m := newMenuMocks(t)
	w := doMenuUpsert(m.router, map[string]any{"recipeId": uuid.NewString(), "serves": 4}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMenuUpsertEntry_MalformedRecipeID_400(t *testing.T) {
	m := newMenuMocks(t)
	w := doMenuUpsert(m.router, map[string]any{"recipeId": "not-a-uuid", "serves": 4}, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", menuErr(t, w))
}

func TestMenuUpsertEntry_ServesTooLow_400(t *testing.T) {
	m := newMenuMocks(t)
	w := doMenuUpsert(m.router, map[string]any{"recipeId": uuid.NewString(), "serves": 0}, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_serves", menuErr(t, w))
}

func TestMenuUpsertEntry_ServesTooHigh_400(t *testing.T) {
	m := newMenuMocks(t)
	w := doMenuUpsert(m.router, map[string]any{"recipeId": uuid.NewString(), "serves": 51}, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_serves", menuErr(t, w))
}

func TestMenuUpsertEntry_Success_200MessageBody(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().UpsertEntry(mock.Anything, menuUserID.String(), id, 4, false).Return(nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, menuUserID.String()).Return(nil).Once()

	w := doMenuUpsert(m.router, map[string]any{"recipeId": id, "serves": 4}, menuAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "recipe added to menu", menuMsg(t, w))
}

func TestMenuUpsertEntry_RegenerateFails_500(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().UpsertEntry(mock.Anything, menuUserID.String(), id, 4, false).Return(nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, menuUserID.String()).Return(errors.New("db down"))

	w := doMenuUpsert(m.router, map[string]any{"recipeId": id, "serves": 4}, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestMenuUpsertEntry_HiddenRecipe_404(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().UpsertEntry(mock.Anything, menuUserID.String(), id, 4, false).
		Return(models.ErrRecipeNotFound)

	w := doMenuUpsert(m.router, map[string]any{"recipeId": id, "serves": 4}, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", menuErr(t, w))
}

func TestMenuUpsertEntry_ThreadsCallerIsAdmin(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().UpsertEntry(mock.Anything, menuUserID.String(), id, 4, true).Return(nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, menuUserID.String()).Return(nil).Once()

	w := doMenuUpsert(m.router, map[string]any{"recipeId": id, "serves": 4}, menuAuth(t, "ADMIN"))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMenuUpsertEntry_RepoError_500(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().UpsertEntry(mock.Anything, menuUserID.String(), id, 4, false).
		Return(errors.New("db down"))

	w := doMenuUpsert(m.router, map[string]any{"recipeId": id, "serves": 4}, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// PATCH /menu/entries/:recipeId

func TestMenuUpdateEntryServes_NoToken_401(t *testing.T) {
	m := newMenuMocks(t)
	w := doMenuPatch(m.router, uuid.NewString(), map[string]any{"serves": 4}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMenuUpdateEntryServes_MalformedID_400(t *testing.T) {
	m := newMenuMocks(t)
	w := doMenuPatch(m.router, "not-a-uuid", map[string]any{"serves": 4}, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", menuErr(t, w))
}

func TestMenuUpdateEntryServes_ServesOutOfRange_400(t *testing.T) {
	m := newMenuMocks(t)
	w := doMenuPatch(m.router, uuid.NewString(), map[string]any{"serves": 0}, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_serves", menuErr(t, w))
}

func TestMenuUpdateEntryServes_Success_200MessageBody(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().UpdateEntryServes(mock.Anything, menuUserID.String(), id, 10).Return(nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, menuUserID.String()).Return(nil).Once()

	w := doMenuPatch(m.router, id, map[string]any{"serves": 10}, menuAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "menu entry updated", menuMsg(t, w))
}

func TestMenuUpdateEntryServes_RegenerateFails_500(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().UpdateEntryServes(mock.Anything, menuUserID.String(), id, 10).Return(nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, menuUserID.String()).Return(errors.New("db down"))

	w := doMenuPatch(m.router, id, map[string]any{"serves": 10}, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestMenuUpdateEntryServes_NotOnMenu_404(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().UpdateEntryServes(mock.Anything, menuUserID.String(), id, 10).
		Return(models.ErrMenuEntryNotFound)

	w := doMenuPatch(m.router, id, map[string]any{"serves": 10}, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "menu_entry_not_found", menuErr(t, w))
}

func TestMenuUpdateEntryServes_RepoError_500(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().UpdateEntryServes(mock.Anything, menuUserID.String(), id, 10).
		Return(errors.New("db down"))

	w := doMenuPatch(m.router, id, map[string]any{"serves": 10}, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// DELETE /menu/entries/:recipeId

func TestMenuRemoveEntry_NoToken_401(t *testing.T) {
	m := newMenuMocks(t)
	w := doMenuDelete(m.router, uuid.NewString(), "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMenuRemoveEntry_MalformedID_400(t *testing.T) {
	m := newMenuMocks(t)
	w := doMenuDelete(m.router, "not-a-uuid", menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", menuErr(t, w))
}

func TestMenuRemoveEntry_Success_200MessageBody(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().RemoveEntry(mock.Anything, menuUserID.String(), id).Return(nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, menuUserID.String()).Return(nil).Once()

	w := doMenuDelete(m.router, id, menuAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "recipe removed from menu", menuMsg(t, w))
}

func TestMenuRemoveEntry_RegenerateFails_500(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().RemoveEntry(mock.Anything, menuUserID.String(), id).Return(nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, menuUserID.String()).Return(errors.New("db down"))

	w := doMenuDelete(m.router, id, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestMenuRemoveEntry_RepoError_500(t *testing.T) {
	m := newMenuMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().RemoveEntry(mock.Anything, menuUserID.String(), id).
		Return(errors.New("db down"))

	w := doMenuDelete(m.router, id, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestMenuClear_NoToken_401(t *testing.T) {
	m := newMenuMocks(t)
	w := doMenuClear(m.router, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMenuClear_Success_200MessageBody(t *testing.T) {
	m := newMenuMocks(t)
	m.repo.EXPECT().ClearMenu(mock.Anything, menuUserID.String()).Return(nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, menuUserID.String()).Return(nil).Once()

	w := doMenuClear(m.router, menuAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "menu cleared", menuMsg(t, w))
}

func TestMenuClear_RegenerateFails_500(t *testing.T) {
	m := newMenuMocks(t)
	m.repo.EXPECT().ClearMenu(mock.Anything, menuUserID.String()).Return(nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, menuUserID.String()).Return(errors.New("db down"))

	w := doMenuClear(m.router, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestMenuClear_RepoError_500(t *testing.T) {
	m := newMenuMocks(t)
	m.repo.EXPECT().ClearMenu(mock.Anything, menuUserID.String()).
		Return(errors.New("db down"))

	w := doMenuClear(m.router, menuAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
