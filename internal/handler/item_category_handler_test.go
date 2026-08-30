package handler_test

import (
	"bytes"
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

var fakeCategory = &models.ItemCategory{
	ID:        uuid.MustParse("22222222-2222-2222-2222-222222222222"),
	Name:      "Cupboard",
	Icon:      "Package",
	CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
}

type itemCategoryMocks struct {
	repo   *genmocks.MockItemCategoryRepository
	router *gin.Engine
}

func newItemCategoryMocks(t *testing.T) *itemCategoryMocks {
	m := &itemCategoryMocks{
		repo: genmocks.NewMockItemCategoryRepository(t),
	}
	h := handler.NewItemCategoryHandler(m.repo)
	m.router = gin.New()
	m.router.GET("/item-categories", h.List)
	m.router.POST("/item-categories", h.Create)
	m.router.PATCH("/item-categories/:id", h.Update)
	m.router.DELETE("/item-categories/:id", h.Delete)
	return m
}

func doItemCategoryRequest(r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
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

func decodeItemCategoryErrorBody(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

// --- List ---

func TestItemCategoryList_Success(t *testing.T) {
	m := newItemCategoryMocks(t)
	m.repo.EXPECT().List(mock.Anything).Return([]*models.ItemCategory{fakeCategory}, nil)

	w := doItemCategoryRequest(m.router, http.MethodGet, "/item-categories", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	var got []models.ItemCategory
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, fakeCategory.ID, got[0].ID)
	assert.Equal(t, fakeCategory.Name, got[0].Name)
	assert.Equal(t, fakeCategory.Icon, got[0].Icon)
}

// --- Create ---

func TestItemCategoryCreate_InvalidJSON(t *testing.T) {
	m := newItemCategoryMocks(t)

	req := httptest.NewRequest(http.MethodPost, "/item-categories", strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", decodeItemCategoryErrorBody(t, w)["error"])
}

func TestItemCategoryCreate_NameRequired(t *testing.T) {
	m := newItemCategoryMocks(t)

	w := doItemCategoryRequest(m.router, http.MethodPost, "/item-categories", map[string]string{"name": "  ", "icon": "Package"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "name_required", decodeItemCategoryErrorBody(t, w)["error"])
}

func TestItemCategoryCreate_NameTooLong(t *testing.T) {
	m := newItemCategoryMocks(t)

	w := doItemCategoryRequest(m.router, http.MethodPost, "/item-categories", map[string]string{"name": strings.Repeat("a", 101), "icon": "Package"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "name_too_long", decodeItemCategoryErrorBody(t, w)["error"])
}

func TestItemCategoryCreate_IconRequired(t *testing.T) {
	m := newItemCategoryMocks(t)

	w := doItemCategoryRequest(m.router, http.MethodPost, "/item-categories", map[string]string{"name": "Cupboard", "icon": " "})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "icon_required", decodeItemCategoryErrorBody(t, w)["error"])
}

func TestItemCategoryCreate_IconTooLong(t *testing.T) {
	m := newItemCategoryMocks(t)

	w := doItemCategoryRequest(m.router, http.MethodPost, "/item-categories", map[string]string{"name": "Cupboard", "icon": strings.Repeat("a", 65)})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "icon_too_long", decodeItemCategoryErrorBody(t, w)["error"])
}

func TestItemCategoryCreate_NameTaken(t *testing.T) {
	m := newItemCategoryMocks(t)
	m.repo.EXPECT().Create(mock.Anything, "Cupboard", "Package").Return(nil, models.ErrItemCategoryNameTaken)

	w := doItemCategoryRequest(m.router, http.MethodPost, "/item-categories", map[string]string{"name": "Cupboard", "icon": "Package"})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "name_taken", decodeItemCategoryErrorBody(t, w)["error"])
}

func TestItemCategoryCreate_IconTaken(t *testing.T) {
	m := newItemCategoryMocks(t)
	m.repo.EXPECT().Create(mock.Anything, "Cupboard", "Package").Return(nil, models.ErrItemCategoryIconTaken)

	w := doItemCategoryRequest(m.router, http.MethodPost, "/item-categories", map[string]string{"name": "Cupboard", "icon": "Package"})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "icon_taken", decodeItemCategoryErrorBody(t, w)["error"])
}

func TestItemCategoryCreate_Success(t *testing.T) {
	m := newItemCategoryMocks(t)
	m.repo.EXPECT().Create(mock.Anything, "Cupboard", "Package").Return(fakeCategory, nil)

	w := doItemCategoryRequest(m.router, http.MethodPost, "/item-categories", map[string]string{"name": "Cupboard", "icon": "Package"})

	assert.Equal(t, http.StatusCreated, w.Code)
	var got models.ItemCategory
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, fakeCategory.ID, got.ID)
	assert.Equal(t, fakeCategory.Name, got.Name)
	assert.Equal(t, fakeCategory.Icon, got.Icon)
}

// --- Update ---

func TestItemCategoryUpdate_NeitherFieldProvided(t *testing.T) {
	m := newItemCategoryMocks(t)

	w := doItemCategoryRequest(m.router, http.MethodPatch, "/item-categories/"+fakeCategory.ID.String(), map[string]string{})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", decodeItemCategoryErrorBody(t, w)["error"])
}

func TestItemCategoryUpdate_PartialNameOnly(t *testing.T) {
	m := newItemCategoryMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeCategory.ID.String(), "New Name", "").Return(fakeCategory, nil)

	w := doItemCategoryRequest(m.router, http.MethodPatch, "/item-categories/"+fakeCategory.ID.String(), map[string]string{"name": "New Name"})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestItemCategoryUpdate_PartialIconOnly(t *testing.T) {
	m := newItemCategoryMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeCategory.ID.String(), "", "NewIcon").Return(fakeCategory, nil)

	w := doItemCategoryRequest(m.router, http.MethodPatch, "/item-categories/"+fakeCategory.ID.String(), map[string]string{"icon": "NewIcon"})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestItemCategoryUpdate_NotFound(t *testing.T) {
	m := newItemCategoryMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeCategory.ID.String(), "New Name", "").Return(nil, models.ErrItemCategoryNotFound)

	w := doItemCategoryRequest(m.router, http.MethodPatch, "/item-categories/"+fakeCategory.ID.String(), map[string]string{"name": "New Name"})

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", decodeItemCategoryErrorBody(t, w)["error"])
}

func TestItemCategoryUpdate_NameTaken(t *testing.T) {
	m := newItemCategoryMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeCategory.ID.String(), "New Name", "").Return(nil, models.ErrItemCategoryNameTaken)

	w := doItemCategoryRequest(m.router, http.MethodPatch, "/item-categories/"+fakeCategory.ID.String(), map[string]string{"name": "New Name"})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "name_taken", decodeItemCategoryErrorBody(t, w)["error"])
}

// --- Delete ---

func TestItemCategoryDelete_Success(t *testing.T) {
	m := newItemCategoryMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeCategory.ID.String()).Return(nil)

	w := doItemCategoryRequest(m.router, http.MethodDelete, "/item-categories/"+fakeCategory.ID.String(), nil)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.Bytes())
}

func TestItemCategoryDelete_NotFound(t *testing.T) {
	m := newItemCategoryMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeCategory.ID.String()).Return(models.ErrItemCategoryNotFound)

	w := doItemCategoryRequest(m.router, http.MethodDelete, "/item-categories/"+fakeCategory.ID.String(), nil)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", decodeItemCategoryErrorBody(t, w)["error"])
}

func TestItemCategoryDelete_InUse(t *testing.T) {
	m := newItemCategoryMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeCategory.ID.String()).Return(models.ErrItemCategoryInUse)

	w := doItemCategoryRequest(m.router, http.MethodDelete, "/item-categories/"+fakeCategory.ID.String(), nil)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "category_in_use", decodeItemCategoryErrorBody(t, w)["error"])
}
