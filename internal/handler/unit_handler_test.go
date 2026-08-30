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

var fakeUnit = &models.Unit{
	ID:           uuid.MustParse("33333333-3333-3333-3333-333333333333"),
	Name:         "grams",
	Abbreviation: "g",
	CreatedAt:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
}

type unitMocks struct {
	repo   *genmocks.MockUnitRepository
	router *gin.Engine
}

func newUnitMocks(t *testing.T) *unitMocks {
	m := &unitMocks{
		repo: genmocks.NewMockUnitRepository(t),
	}
	h := handler.NewUnitHandler(m.repo)
	m.router = gin.New()
	m.router.GET("/units", h.List)
	m.router.POST("/units", h.Create)
	m.router.PATCH("/units/:id", h.Update)
	m.router.DELETE("/units/:id", h.Delete)
	return m
}

func doUnitRequest(r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
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

func decodeUnitErrorBody(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

// --- List ---

func TestUnitList_Success(t *testing.T) {
	m := newUnitMocks(t)
	m.repo.EXPECT().List(mock.Anything).Return([]*models.Unit{fakeUnit}, nil)

	w := doUnitRequest(m.router, http.MethodGet, "/units", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	var got []models.Unit
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, fakeUnit.ID, got[0].ID)
	assert.Equal(t, fakeUnit.Name, got[0].Name)
	assert.Equal(t, fakeUnit.Abbreviation, got[0].Abbreviation)
}

// --- Create ---

func TestUnitCreate_InvalidJSON(t *testing.T) {
	m := newUnitMocks(t)

	req := httptest.NewRequest(http.MethodPost, "/units", strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", decodeUnitErrorBody(t, w)["error"])
}

func TestUnitCreate_NameRequired(t *testing.T) {
	m := newUnitMocks(t)

	w := doUnitRequest(m.router, http.MethodPost, "/units", map[string]string{"name": "  ", "abbreviation": "g"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "name_required", decodeUnitErrorBody(t, w)["error"])
}

func TestUnitCreate_NameTooLong(t *testing.T) {
	m := newUnitMocks(t)

	w := doUnitRequest(m.router, http.MethodPost, "/units", map[string]string{"name": strings.Repeat("a", 101), "abbreviation": "g"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "name_too_long", decodeUnitErrorBody(t, w)["error"])
}

func TestUnitCreate_AbbreviationRequired(t *testing.T) {
	m := newUnitMocks(t)

	w := doUnitRequest(m.router, http.MethodPost, "/units", map[string]string{"name": "grams", "abbreviation": " "})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "abbreviation_required", decodeUnitErrorBody(t, w)["error"])
}

func TestUnitCreate_AbbreviationTooLong(t *testing.T) {
	m := newUnitMocks(t)

	w := doUnitRequest(m.router, http.MethodPost, "/units", map[string]string{"name": "grams", "abbreviation": strings.Repeat("a", 33)})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "abbreviation_too_long", decodeUnitErrorBody(t, w)["error"])
}

func TestUnitCreate_NameTaken(t *testing.T) {
	m := newUnitMocks(t)
	m.repo.EXPECT().Create(mock.Anything, "grams", "g").Return(nil, models.ErrUnitNameTaken)

	w := doUnitRequest(m.router, http.MethodPost, "/units", map[string]string{"name": "grams", "abbreviation": "g"})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "name_taken", decodeUnitErrorBody(t, w)["error"])
}

func TestUnitCreate_AbbreviationTaken(t *testing.T) {
	m := newUnitMocks(t)
	m.repo.EXPECT().Create(mock.Anything, "grams", "g").Return(nil, models.ErrUnitAbbreviationTaken)

	w := doUnitRequest(m.router, http.MethodPost, "/units", map[string]string{"name": "grams", "abbreviation": "g"})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "abbreviation_taken", decodeUnitErrorBody(t, w)["error"])
}

func TestUnitCreate_Success(t *testing.T) {
	m := newUnitMocks(t)
	m.repo.EXPECT().Create(mock.Anything, "grams", "g").Return(fakeUnit, nil)

	w := doUnitRequest(m.router, http.MethodPost, "/units", map[string]string{"name": "grams", "abbreviation": "g"})

	assert.Equal(t, http.StatusCreated, w.Code)
	var got models.Unit
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, fakeUnit.ID, got.ID)
	assert.Equal(t, fakeUnit.Name, got.Name)
	assert.Equal(t, fakeUnit.Abbreviation, got.Abbreviation)
}

// --- Update ---

func TestUnitUpdate_NeitherFieldProvided(t *testing.T) {
	m := newUnitMocks(t)

	w := doUnitRequest(m.router, http.MethodPatch, "/units/"+fakeUnit.ID.String(), map[string]string{})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", decodeUnitErrorBody(t, w)["error"])
}

func TestUnitUpdate_PartialNameOnly(t *testing.T) {
	m := newUnitMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeUnit.ID.String(), "kilograms", "").Return(fakeUnit, nil)

	w := doUnitRequest(m.router, http.MethodPatch, "/units/"+fakeUnit.ID.String(), map[string]string{"name": "kilograms"})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUnitUpdate_PartialAbbreviationOnly(t *testing.T) {
	m := newUnitMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeUnit.ID.String(), "", "kg").Return(fakeUnit, nil)

	w := doUnitRequest(m.router, http.MethodPatch, "/units/"+fakeUnit.ID.String(), map[string]string{"abbreviation": "kg"})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUnitUpdate_NotFound(t *testing.T) {
	m := newUnitMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeUnit.ID.String(), "kilograms", "").Return(nil, models.ErrUnitNotFound)

	w := doUnitRequest(m.router, http.MethodPatch, "/units/"+fakeUnit.ID.String(), map[string]string{"name": "kilograms"})

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", decodeUnitErrorBody(t, w)["error"])
}

func TestUnitUpdate_NameTaken(t *testing.T) {
	m := newUnitMocks(t)
	m.repo.EXPECT().Update(mock.Anything, fakeUnit.ID.String(), "kilograms", "").Return(nil, models.ErrUnitNameTaken)

	w := doUnitRequest(m.router, http.MethodPatch, "/units/"+fakeUnit.ID.String(), map[string]string{"name": "kilograms"})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "name_taken", decodeUnitErrorBody(t, w)["error"])
}

// --- Delete ---

func TestUnitDelete_Success(t *testing.T) {
	m := newUnitMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeUnit.ID.String()).Return(nil)

	w := doUnitRequest(m.router, http.MethodDelete, "/units/"+fakeUnit.ID.String(), nil)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.Bytes())
}

func TestUnitDelete_NotFound(t *testing.T) {
	m := newUnitMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeUnit.ID.String()).Return(models.ErrUnitNotFound)

	w := doUnitRequest(m.router, http.MethodDelete, "/units/"+fakeUnit.ID.String(), nil)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", decodeUnitErrorBody(t, w)["error"])
}

func TestUnitDelete_InUse(t *testing.T) {
	m := newUnitMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, fakeUnit.ID.String()).Return(models.ErrUnitInUse)

	w := doUnitRequest(m.router, http.MethodDelete, "/units/"+fakeUnit.ID.String(), nil)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "unit_in_use", decodeUnitErrorBody(t, w)["error"])
}
