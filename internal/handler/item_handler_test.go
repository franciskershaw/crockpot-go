package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/handler"
	genmocks "github.com/franciskershaw/crockpot-go/internal/handler/mocks"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var fakeItem = &models.Item{
	ID:             uuid.MustParse("44444444-4444-4444-4444-444444444444"),
	Name:           "Fresh Basil",
	CategoryID:     uuid.MustParse("55555555-5555-5555-5555-555555555555"),
	AllowedUnitIDs: []uuid.UUID{},
	CreatedAt:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
}

type itemMocks struct {
	repo       *genmocks.MockItemRepository
	transactor *genmocks.MockTransactor
	router     *gin.Engine
}

func newItemMocks(t *testing.T) *itemMocks {
	m := &itemMocks{
		repo:       genmocks.NewMockItemRepository(t),
		transactor: genmocks.NewMockTransactor(t),
	}
	m.transactor.EXPECT().WithinTx(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }).
		Maybe()
	h := handler.NewItemHandler(m.repo, m.transactor)
	m.router = gin.New()
	m.router.GET("/items", h.List)
	m.router.POST("/items", h.Create)
	m.router.PATCH("/items/:id", h.Update)
	m.router.DELETE("/items/:id", h.Delete)
	return m
}

func doItemRequest(r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	var reqBody *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeItemErrorBody(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

// --- List ---

func TestItemList_Success(t *testing.T) {
	m := newItemMocks(t)
	m.repo.EXPECT().List(mock.Anything).Return([]*models.Item{fakeItem}, nil)

	w := doItemRequest(m.router, http.MethodGet, "/items", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	var got []models.Item
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, fakeItem.ID, got[0].ID)
	assert.Equal(t, fakeItem.Name, got[0].Name)
	assert.Equal(t, fakeItem.CategoryID, got[0].CategoryID)
}

// --- Create ---

func TestItemCreate_InvalidJSON(t *testing.T) {
	m := newItemMocks(t)

	req := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", decodeItemErrorBody(t, w)["error"])
}

func TestItemCreate_NameRequired(t *testing.T) {
	m := newItemMocks(t)

	w := doItemRequest(m.router, http.MethodPost, "/items", map[string]any{"name": "  ", "categoryId": fakeItem.CategoryID.String()})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "name_required", decodeItemErrorBody(t, w)["error"])
}

func TestItemCreate_NameTooLong(t *testing.T) {
	m := newItemMocks(t)

	w := doItemRequest(m.router, http.MethodPost, "/items", map[string]any{"name": strings.Repeat("a", 101), "categoryId": fakeItem.CategoryID.String()})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "name_too_long", decodeItemErrorBody(t, w)["error"])
}

func TestItemCreate_CategoryIDRequired(t *testing.T) {
	m := newItemMocks(t)

	w := doItemRequest(m.router, http.MethodPost, "/items", map[string]any{"name": "Fresh Basil", "categoryId": " "})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "category_id_required", decodeItemErrorBody(t, w)["error"])
}

func TestItemCreate_CategoryIDMalformed(t *testing.T) {
	m := newItemMocks(t)

	w := doItemRequest(m.router, http.MethodPost, "/items", map[string]any{"name": "Fresh Basil", "categoryId": "not-a-uuid"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", decodeItemErrorBody(t, w)["error"])
}

func TestItemCreate_AllowedUnitIDMalformed(t *testing.T) {
	m := newItemMocks(t)

	w := doItemRequest(m.router, http.MethodPost, "/items", map[string]any{
		"name": "Fresh Basil", "categoryId": fakeItem.CategoryID.String(), "allowedUnitIds": []string{"not-a-uuid"},
	})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", decodeItemErrorBody(t, w)["error"])
}

func TestItemCreate_NameTaken(t *testing.T) {
	m := newItemMocks(t)
	m.repo.EXPECT().Create(mock.Anything, "Fresh Basil", fakeItem.CategoryID.String(), []string(nil)).Return(nil, models.ErrItemNameTaken)

	w := doItemRequest(m.router, http.MethodPost, "/items", map[string]any{"name": "Fresh Basil", "categoryId": fakeItem.CategoryID.String()})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "name_taken", decodeItemErrorBody(t, w)["error"])
}

func TestItemCreate_InvalidCategory(t *testing.T) {
	m := newItemMocks(t)
	m.repo.EXPECT().Create(mock.Anything, "Fresh Basil", fakeItem.CategoryID.String(), []string(nil)).Return(nil, models.ErrItemInvalidCategory)

	w := doItemRequest(m.router, http.MethodPost, "/items", map[string]any{"name": "Fresh Basil", "categoryId": fakeItem.CategoryID.String()})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_category_id", decodeItemErrorBody(t, w)["error"])
}

func TestItemCreate_InvalidUnit(t *testing.T) {
	m := newItemMocks(t)
	unitID := uuid.NewString()
	m.repo.EXPECT().Create(mock.Anything, "Fresh Basil", fakeItem.CategoryID.String(), []string{unitID}).Return(nil, models.ErrItemInvalidUnit)

	w := doItemRequest(m.router, http.MethodPost, "/items", map[string]any{
		"name": "Fresh Basil", "categoryId": fakeItem.CategoryID.String(), "allowedUnitIds": []string{unitID},
	})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_unit_id", decodeItemErrorBody(t, w)["error"])
}

func TestItemCreate_Success(t *testing.T) {
	m := newItemMocks(t)
	m.repo.EXPECT().Create(mock.Anything, "Fresh Basil", fakeItem.CategoryID.String(), []string(nil)).Return(fakeItem, nil)

	w := doItemRequest(m.router, http.MethodPost, "/items", map[string]any{"name": "Fresh Basil", "categoryId": fakeItem.CategoryID.String()})

	assert.Equal(t, http.StatusCreated, w.Code)
	var got models.Item
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, fakeItem.ID, got.ID)
	assert.Equal(t, fakeItem.Name, got.Name)
}

// --- Update ---

func TestItemUpdate_NoFieldsProvided(t *testing.T) {
	m := newItemMocks(t)

	w := doItemRequest(m.router, http.MethodPatch, "/items/"+fakeItem.ID.String(), map[string]any{})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", decodeItemErrorBody(t, w)["error"])
}

func TestItemUpdate_PartialNameOnly(t *testing.T) {
	m := newItemMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeItem.ID.String(), "New Name", "", []string(nil)).Return(fakeItem, nil)

	w := doItemRequest(m.router, http.MethodPatch, "/items/"+fakeItem.ID.String(), map[string]any{"name": "New Name"})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestItemUpdate_ReplaceAllowedUnits(t *testing.T) {
	m := newItemMocks(t)
	unitID := uuid.NewString()
	m.repo.EXPECT().Update(mock.Anything, fakeItem.ID.String(), "", "", []string{unitID}).Return(fakeItem, nil)

	w := doItemRequest(m.router, http.MethodPatch, "/items/"+fakeItem.ID.String(), map[string]any{"allowedUnitIds": []string{unitID}})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestItemUpdate_ClearAllowedUnits(t *testing.T) {
	m := newItemMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeItem.ID.String(), "", "", []string{}).Return(fakeItem, nil)

	w := doItemRequest(m.router, http.MethodPatch, "/items/"+fakeItem.ID.String(), map[string]any{"allowedUnitIds": []string{}})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestItemUpdate_NotFound(t *testing.T) {
	m := newItemMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeItem.ID.String(), "New Name", "", []string(nil)).Return(nil, models.ErrItemNotFound)

	w := doItemRequest(m.router, http.MethodPatch, "/items/"+fakeItem.ID.String(), map[string]any{"name": "New Name"})

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", decodeItemErrorBody(t, w)["error"])
}

func TestItemUpdate_NameTaken(t *testing.T) {
	m := newItemMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeItem.ID.String(), "New Name", "", []string(nil)).Return(nil, models.ErrItemNameTaken)

	w := doItemRequest(m.router, http.MethodPatch, "/items/"+fakeItem.ID.String(), map[string]any{"name": "New Name"})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "name_taken", decodeItemErrorBody(t, w)["error"])
}

// --- Delete ---

func TestItemDelete_Success(t *testing.T) {
	m := newItemMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeItem.ID.String()).Return(nil)

	w := doItemRequest(m.router, http.MethodDelete, "/items/"+fakeItem.ID.String(), nil)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.Bytes())
}

func TestItemDelete_NotFound(t *testing.T) {
	m := newItemMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeItem.ID.String()).Return(models.ErrItemNotFound)

	w := doItemRequest(m.router, http.MethodDelete, "/items/"+fakeItem.ID.String(), nil)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", decodeItemErrorBody(t, w)["error"])
}

func TestItemDelete_InUse(t *testing.T) {
	m := newItemMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeItem.ID.String()).Return(models.ErrItemInUse)

	w := doItemRequest(m.router, http.MethodDelete, "/items/"+fakeItem.ID.String(), nil)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "item_in_use", decodeItemErrorBody(t, w)["error"])
}
