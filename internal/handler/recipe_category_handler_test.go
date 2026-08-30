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

var fakeRecipeCategory = &models.RecipeCategory{
	ID:        uuid.MustParse("33333333-3333-3333-3333-333333333333"),
	Name:      "Batch",
	CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
}

type recipeCategoryMocks struct {
	repo   *genmocks.MockRecipeCategoryRepository
	router *gin.Engine
}

func newRecipeCategoryMocks(t *testing.T) *recipeCategoryMocks {
	m := &recipeCategoryMocks{
		repo: genmocks.NewMockRecipeCategoryRepository(t),
	}
	h := handler.NewRecipeCategoryHandler(m.repo)
	m.router = gin.New()
	m.router.GET("/recipe-categories", h.List)
	m.router.POST("/recipe-categories", h.Create)
	m.router.PATCH("/recipe-categories/:id", h.Update)
	m.router.DELETE("/recipe-categories/:id", h.Delete)
	return m
}

func doRecipeCategoryRequest(r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
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

func decodeRecipeCategoryErrorBody(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

// --- List ---

func TestRecipeCategoryList_Success(t *testing.T) {
	m := newRecipeCategoryMocks(t)
	m.repo.EXPECT().List(mock.Anything).Return([]*models.RecipeCategory{fakeRecipeCategory}, nil)

	w := doRecipeCategoryRequest(m.router, http.MethodGet, "/recipe-categories", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	var got []models.RecipeCategory
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, fakeRecipeCategory.ID, got[0].ID)
	assert.Equal(t, fakeRecipeCategory.Name, got[0].Name)
}

// --- Create ---

func TestRecipeCategoryCreate_InvalidJSON(t *testing.T) {
	m := newRecipeCategoryMocks(t)

	req := httptest.NewRequest(http.MethodPost, "/recipe-categories", strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", decodeRecipeCategoryErrorBody(t, w)["error"])
}

func TestRecipeCategoryCreate_NameRequired(t *testing.T) {
	m := newRecipeCategoryMocks(t)

	w := doRecipeCategoryRequest(m.router, http.MethodPost, "/recipe-categories", map[string]string{"name": "  "})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "name_required", decodeRecipeCategoryErrorBody(t, w)["error"])
}

func TestRecipeCategoryCreate_NameTooLong(t *testing.T) {
	m := newRecipeCategoryMocks(t)

	w := doRecipeCategoryRequest(m.router, http.MethodPost, "/recipe-categories", map[string]string{"name": strings.Repeat("a", 101)})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "name_too_long", decodeRecipeCategoryErrorBody(t, w)["error"])
}

func TestRecipeCategoryCreate_NameTaken(t *testing.T) {
	m := newRecipeCategoryMocks(t)
	m.repo.EXPECT().Create(mock.Anything, "Batch").Return(nil, models.ErrRecipeCategoryNameTaken)

	w := doRecipeCategoryRequest(m.router, http.MethodPost, "/recipe-categories", map[string]string{"name": "Batch"})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "name_taken", decodeRecipeCategoryErrorBody(t, w)["error"])
}

func TestRecipeCategoryCreate_Success(t *testing.T) {
	m := newRecipeCategoryMocks(t)
	m.repo.EXPECT().Create(mock.Anything, "Batch").Return(fakeRecipeCategory, nil)

	w := doRecipeCategoryRequest(m.router, http.MethodPost, "/recipe-categories", map[string]string{"name": "Batch"})

	assert.Equal(t, http.StatusCreated, w.Code)
	var got models.RecipeCategory
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, fakeRecipeCategory.ID, got.ID)
	assert.Equal(t, fakeRecipeCategory.Name, got.Name)
}

// --- Update ---

func TestRecipeCategoryUpdate_NameRequired(t *testing.T) {
	m := newRecipeCategoryMocks(t)

	w := doRecipeCategoryRequest(m.router, http.MethodPatch, "/recipe-categories/"+fakeRecipeCategory.ID.String(), map[string]string{"name": " "})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "name_required", decodeRecipeCategoryErrorBody(t, w)["error"])
}

func TestRecipeCategoryUpdate_Success(t *testing.T) {
	m := newRecipeCategoryMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeRecipeCategory.ID.String(), "New Name").Return(fakeRecipeCategory, nil)

	w := doRecipeCategoryRequest(m.router, http.MethodPatch, "/recipe-categories/"+fakeRecipeCategory.ID.String(), map[string]string{"name": "New Name"})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecipeCategoryUpdate_NotFound(t *testing.T) {
	m := newRecipeCategoryMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeRecipeCategory.ID.String(), "New Name").Return(nil, models.ErrRecipeCategoryNotFound)

	w := doRecipeCategoryRequest(m.router, http.MethodPatch, "/recipe-categories/"+fakeRecipeCategory.ID.String(), map[string]string{"name": "New Name"})

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", decodeRecipeCategoryErrorBody(t, w)["error"])
}

func TestRecipeCategoryUpdate_NameTaken(t *testing.T) {
	m := newRecipeCategoryMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeRecipeCategory.ID.String(), "New Name").Return(nil, models.ErrRecipeCategoryNameTaken)

	w := doRecipeCategoryRequest(m.router, http.MethodPatch, "/recipe-categories/"+fakeRecipeCategory.ID.String(), map[string]string{"name": "New Name"})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "name_taken", decodeRecipeCategoryErrorBody(t, w)["error"])
}

// --- Delete ---

func TestRecipeCategoryDelete_Success(t *testing.T) {
	m := newRecipeCategoryMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeRecipeCategory.ID.String()).Return(nil)

	w := doRecipeCategoryRequest(m.router, http.MethodDelete, "/recipe-categories/"+fakeRecipeCategory.ID.String(), nil)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.Bytes())
}

func TestRecipeCategoryDelete_NotFound(t *testing.T) {
	m := newRecipeCategoryMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeRecipeCategory.ID.String()).Return(models.ErrRecipeCategoryNotFound)

	w := doRecipeCategoryRequest(m.router, http.MethodDelete, "/recipe-categories/"+fakeRecipeCategory.ID.String(), nil)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", decodeRecipeCategoryErrorBody(t, w)["error"])
}

func TestRecipeCategoryDelete_InUse(t *testing.T) {
	m := newRecipeCategoryMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeRecipeCategory.ID.String()).Return(models.ErrRecipeCategoryInUse)

	w := doRecipeCategoryRequest(m.router, http.MethodDelete, "/recipe-categories/"+fakeRecipeCategory.ID.String(), nil)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "category_in_use", decodeRecipeCategoryErrorBody(t, w)["error"])
}
