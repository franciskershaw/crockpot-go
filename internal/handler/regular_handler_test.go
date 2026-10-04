package handler_test

import (
	"bytes"
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

var regularUserID = uuid.MustParse("d4d4d4d4-d4d4-d4d4-d4d4-d4d4d4d4d4d4")

func newRegularMocks(t *testing.T) (*genmocks.MockRegularRepository, *gin.Engine) {
	repo := genmocks.NewMockRegularRepository(t)
	h := handler.NewRegularHandler(repo)
	r := gin.New()
	authed := r.Group("/regulars")
	authed.Use(middleware.AuthMiddleware(testutil.TestAccessSecret))
	{
		authed.GET("", h.List)
		authed.POST("", h.Create)
		authed.PATCH("/:id", h.Update)
		authed.DELETE("/:id", h.Delete)
	}
	return repo, r
}

func regularAuth(t *testing.T) string {
	t.Helper()
	return testutil.AuthHeader(t, "cook@example.com", regularUserID.String(), "FREE")
}

func doRegularRequest(r *gin.Engine, method, path string, body any, auth string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func fakeRegular() *models.Regular {
	unitID := uuid.MustParse("f7f7f7f7-f7f7-f7f7-f7f7-f7f7f7f7f7f7")
	abbreviation := "rolls"
	return &models.Regular{
		ID:               uuid.MustParse("a8a8a8a8-a8a8-a8a8-a8a8-a8a8a8a8a8a8"),
		ItemID:           uuid.MustParse("b9b9b9b9-b9b9-b9b9-b9b9-b9b9b9b9b9b9"),
		ItemName:         "Toilet Paper",
		CategoryID:       uuid.MustParse("c0c0c0c0-c0c0-c0c0-c0c0-c0c0c0c0c0c0"),
		CategoryName:     "House",
		UnitID:           &unitID,
		UnitAbbreviation: &abbreviation,
		Quantity:         9,
	}
}

func TestRegularRoutes_NoToken_401(t *testing.T) {
	_, r := newRegularMocks(t)
	id := uuid.NewString()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/regulars"},
		{http.MethodPost, "/regulars"},
		{http.MethodPatch, "/regulars/" + id},
		{http.MethodDelete, "/regulars/" + id},
	} {
		w := doRegularRequest(r, tc.method, tc.path, nil, "")
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s %s", tc.method, tc.path)
	}
}

func TestRegularList_200WithRegulars(t *testing.T) {
	repo, r := newRegularMocks(t)
	repo.EXPECT().List(mock.Anything, regularUserID.String()).Return([]models.Regular{*fakeRegular()}, nil)

	w := doRegularRequest(r, http.MethodGet, "/regulars", nil, regularAuth(t))

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `[{
		"id": "a8a8a8a8-a8a8-a8a8-a8a8-a8a8a8a8a8a8",
		"itemId": "b9b9b9b9-b9b9-b9b9-b9b9-b9b9b9b9b9b9",
		"itemName": "Toilet Paper",
		"categoryId": "c0c0c0c0-c0c0-c0c0-c0c0-c0c0c0c0c0c0",
		"categoryName": "House",
		"unitId": "f7f7f7f7-f7f7-f7f7-f7f7-f7f7f7f7f7f7",
		"unitAbbreviation": "rolls",
		"quantity": 9
	}]`, w.Body.String())
}

func TestRegularList_EmptySerializesAsArray(t *testing.T) {
	repo, r := newRegularMocks(t)
	repo.EXPECT().List(mock.Anything, regularUserID.String()).Return([]models.Regular{}, nil)

	w := doRegularRequest(r, http.MethodGet, "/regulars", nil, regularAuth(t))

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `[]`, w.Body.String())
}

func TestRegularList_RepoError_500(t *testing.T) {
	repo, r := newRegularMocks(t)
	repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errors.New("db down"))

	w := doRegularRequest(r, http.MethodGet, "/regulars", nil, regularAuth(t))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRegularCreate_201WithRegularAndPassesTheCap(t *testing.T) {
	repo, r := newRegularMocks(t)
	itemID := uuid.NewString()
	unitID := uuid.NewString()
	repo.EXPECT().Create(mock.Anything, regularUserID.String(), itemID, &unitID, 9.0, 50).Return(fakeRegular(), nil)

	w := doRegularRequest(r, http.MethodPost, "/regulars",
		map[string]any{"itemId": itemID, "unitId": unitID, "quantity": 9}, regularAuth(t))

	require.Equal(t, http.StatusCreated, w.Code)
	var body models.Regular
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "Toilet Paper", body.ItemName)
}

func TestRegularCreate_NoUnitPassesNil(t *testing.T) {
	repo, r := newRegularMocks(t)
	itemID := uuid.NewString()
	repo.EXPECT().Create(mock.Anything, regularUserID.String(), itemID, (*string)(nil), 1.0, 50).Return(fakeRegular(), nil)

	w := doRegularRequest(r, http.MethodPost, "/regulars", map[string]any{"itemId": itemID, "quantity": 1}, regularAuth(t))

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestRegularCreate_InvalidBody_400(t *testing.T) {
	for name, body := range map[string]map[string]any{
		"malformed item id": {"itemId": "nope", "quantity": 1},
		"malformed unit id": {"itemId": uuid.NewString(), "unitId": "nope", "quantity": 1},
		"missing quantity":  {"itemId": uuid.NewString()},
		"zero quantity":     {"itemId": uuid.NewString(), "quantity": 0},
	} {
		t.Run(name, func(t *testing.T) {
			_, r := newRegularMocks(t)
			w := doRegularRequest(r, http.MethodPost, "/regulars", body, regularAuth(t))
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestRegularCreate_RepoErrorsMapToContract(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{models.ErrRegularInvalidItem, http.StatusBadRequest, "invalid_item_id"},
		{models.ErrRegularInvalidUnit, http.StatusBadRequest, "invalid_unit_id"},
		{models.ErrIngredientUnitNotAllowed, http.StatusBadRequest, "unit_not_allowed_for_item"},
		{models.ErrRegularExists, http.StatusConflict, "regular_exists"},
		{models.ErrRegularsLimitReached, http.StatusConflict, "regulars_limit_reached"},
		{errors.New("db down"), http.StatusInternalServerError, "server_error"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			repo, r := newRegularMocks(t)
			repo.EXPECT().Create(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, tc.err)

			w := doRegularRequest(r, http.MethodPost, "/regulars", map[string]any{"itemId": uuid.NewString(), "quantity": 1}, regularAuth(t))

			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.code, decodeJSONBody(t, w)["error"])
		})
	}
}

func TestRegularUpdate_200WithRegular(t *testing.T) {
	repo, r := newRegularMocks(t)
	id := uuid.NewString()
	unitID := uuid.NewString()
	repo.EXPECT().Update(mock.Anything, regularUserID.String(), id, &unitID, 4.0).Return(fakeRegular(), nil)

	w := doRegularRequest(r, http.MethodPatch, "/regulars/"+id, map[string]any{"unitId": unitID, "quantity": 4}, regularAuth(t))

	require.Equal(t, http.StatusOK, w.Code)
	var body models.Regular
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "Toilet Paper", body.ItemName)
}

func TestRegularUpdate_NullOrAbsentUnitClearsIt(t *testing.T) {
	for name, body := range map[string]map[string]any{
		"null unit":   {"unitId": nil, "quantity": 4},
		"absent unit": {"quantity": 4},
		"empty unit":  {"unitId": "", "quantity": 4},
	} {
		t.Run(name, func(t *testing.T) {
			repo, r := newRegularMocks(t)
			id := uuid.NewString()
			repo.EXPECT().Update(mock.Anything, regularUserID.String(), id, (*string)(nil), 4.0).Return(fakeRegular(), nil)

			w := doRegularRequest(r, http.MethodPatch, "/regulars/"+id, body, regularAuth(t))

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestRegularUpdate_InvalidRequest_400(t *testing.T) {
	for name, tc := range map[string]struct {
		id   string
		body map[string]any
	}{
		"malformed id":      {"nope", map[string]any{"quantity": 1}},
		"malformed unit id": {uuid.NewString(), map[string]any{"unitId": "nope", "quantity": 1}},
		"missing quantity":  {uuid.NewString(), map[string]any{"unitId": uuid.NewString()}},
		"negative quantity": {uuid.NewString(), map[string]any{"quantity": -1}},
	} {
		t.Run(name, func(t *testing.T) {
			_, r := newRegularMocks(t)
			w := doRegularRequest(r, http.MethodPatch, "/regulars/"+tc.id, tc.body, regularAuth(t))
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestRegularUpdate_RepoErrorsMapToContract(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{models.ErrRegularNotFound, http.StatusNotFound, "regular_not_found"},
		{models.ErrRegularInvalidUnit, http.StatusBadRequest, "invalid_unit_id"},
		{models.ErrIngredientUnitNotAllowed, http.StatusBadRequest, "unit_not_allowed_for_item"},
		{errors.New("db down"), http.StatusInternalServerError, "server_error"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			repo, r := newRegularMocks(t)
			repo.EXPECT().Update(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, tc.err)

			w := doRegularRequest(r, http.MethodPatch, "/regulars/"+uuid.NewString(), map[string]any{"quantity": 1}, regularAuth(t))

			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.code, decodeJSONBody(t, w)["error"])
		})
	}
}

func TestRegularDelete_204(t *testing.T) {
	repo, r := newRegularMocks(t)
	id := uuid.NewString()
	repo.EXPECT().Delete(mock.Anything, regularUserID.String(), id).Return(nil)

	w := doRegularRequest(r, http.MethodDelete, "/regulars/"+id, nil, regularAuth(t))

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String())
}

func TestRegularDelete_MalformedID_400(t *testing.T) {
	_, r := newRegularMocks(t)

	w := doRegularRequest(r, http.MethodDelete, "/regulars/nope", nil, regularAuth(t))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRegularDelete_RepoErrorsMapToContract(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{models.ErrRegularNotFound, http.StatusNotFound, "regular_not_found"},
		{errors.New("db down"), http.StatusInternalServerError, "server_error"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			repo, r := newRegularMocks(t)
			repo.EXPECT().Delete(mock.Anything, mock.Anything, mock.Anything).Return(tc.err)

			w := doRegularRequest(r, http.MethodDelete, "/regulars/"+uuid.NewString(), nil, regularAuth(t))

			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.code, decodeJSONBody(t, w)["error"])
		})
	}
}
