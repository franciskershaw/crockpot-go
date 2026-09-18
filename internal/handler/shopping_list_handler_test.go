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

var shoppingListUserID = uuid.MustParse("e5e5e5e5-e5e5-e5e5-e5e5-e5e5e5e5e5e5")

type shoppingListMocks struct {
	repo       *genmocks.MockShoppingListRepository
	transactor *genmocks.MockTransactor
	router     *gin.Engine
}

func newShoppingListMocks(t *testing.T) *shoppingListMocks {
	m := &shoppingListMocks{
		repo:       genmocks.NewMockShoppingListRepository(t),
		transactor: genmocks.NewMockTransactor(t),
	}
	m.transactor.EXPECT().WithinTx(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }).
		Maybe()
	h := handler.NewShoppingListHandler(m.repo, m.transactor)
	m.router = gin.New()
	authed := m.router.Group("/shopping-list")
	authed.Use(middleware.AuthMiddleware(testutil.TestAccessSecret))
	{
		authed.GET("", h.Get)
		authed.POST("/items", h.AddItem)
		authed.PATCH("/items/:id", h.UpdateItem)
		authed.DELETE("/items/:id", h.DeleteItem)
	}
	return m
}

func shoppingListErr(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.Error
}

func shoppingListMsg(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.Message
}

func doShoppingListAddItem(r *gin.Engine, body any, auth string) *httptest.ResponseRecorder {
	var reqBody *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(http.MethodPost, "/shopping-list/items", reqBody)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doShoppingListUpdateItem(r *gin.Engine, id string, body any, auth string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/shopping-list/items/"+id, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doShoppingListDeleteItem(r *gin.Engine, id string, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/shopping-list/items/"+id, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func shoppingListAuth(t *testing.T, role string) string {
	t.Helper()
	return testutil.AuthHeader(t, "cook@example.com", shoppingListUserID.String(), role)
}

func doShoppingListGet(r *gin.Engine, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/shopping-list", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func fakeShoppingList() *models.ShoppingList {
	unitID := uuid.MustParse("f6f6f6f6-f6f6-f6f6-f6f6-f6f6f6f6f6f6")
	abbreviation := "g"
	return &models.ShoppingList{
		Items: []models.ShoppingListItem{
			{
				ID:               uuid.MustParse("a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1"),
				ItemID:           uuid.MustParse("b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2"),
				ItemName:         "Flour (White)",
				ItemCategoryID:   uuid.MustParse("c3c3c3c3-c3c3-c3c3-c3c3-c3c3c3c3c3c3"),
				ItemCategoryName: "Baking",
				UnitID:           &unitID,
				UnitAbbreviation: &abbreviation,
				Quantity:         250,
				Obtained:         false,
				IsManual:         false,
			},
		},
	}
}

func TestShoppingListGet_NoToken_401(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListGet(m.router, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestShoppingListGet_Success_200Envelope(t *testing.T) {
	m := newShoppingListMocks(t)
	m.repo.EXPECT().Get(mock.Anything, shoppingListUserID.String()).Return(fakeShoppingList(), nil)

	w := doShoppingListGet(m.router, shoppingListAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)

	var body models.ShoppingList
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	assert.Equal(t, "Flour (White)", body.Items[0].ItemName)
	assert.Equal(t, "Baking", body.Items[0].ItemCategoryName)
	assert.Equal(t, 250.0, body.Items[0].Quantity)
}

func TestShoppingListGet_EmptyItemsSerializesAsArray(t *testing.T) {
	m := newShoppingListMocks(t)
	m.repo.EXPECT().Get(mock.Anything, shoppingListUserID.String()).Return(&models.ShoppingList{Items: []models.ShoppingListItem{}}, nil)

	w := doShoppingListGet(m.router, shoppingListAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"items":[]}`, w.Body.String())
}

func TestShoppingListAddItem_NoToken_401(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListAddItem(m.router, map[string]any{"itemId": uuid.NewString(), "quantity": 2}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestShoppingListAddItem_MalformedItemID_400(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListAddItem(m.router, map[string]any{"itemId": "not-a-uuid", "quantity": 2}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", shoppingListErr(t, w))
}

func TestShoppingListAddItem_MissingQuantity_400(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListAddItem(m.router, map[string]any{"itemId": uuid.NewString()}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_quantity", shoppingListErr(t, w))
}

func TestShoppingListAddItem_ZeroQuantity_400(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListAddItem(m.router, map[string]any{"itemId": uuid.NewString(), "quantity": 0}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_quantity", shoppingListErr(t, w))
}

func TestShoppingListAddItem_MalformedUnitID_400(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListAddItem(m.router, map[string]any{"itemId": uuid.NewString(), "unitId": "not-a-uuid", "quantity": 2}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", shoppingListErr(t, w))
}

func TestShoppingListAddItem_Success_200MessageBody(t *testing.T) {
	m := newShoppingListMocks(t)
	itemID := uuid.NewString()
	unitID := uuid.NewString()
	m.repo.EXPECT().AddManualItem(mock.Anything, shoppingListUserID.String(), itemID, &unitID, 2.0).Return(nil)

	w := doShoppingListAddItem(m.router, map[string]any{"itemId": itemID, "unitId": unitID, "quantity": 2}, shoppingListAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "item added to shopping list", shoppingListMsg(t, w))
}

func TestShoppingListAddItem_NoUnit_PassesNilUnitID(t *testing.T) {
	m := newShoppingListMocks(t)
	itemID := uuid.NewString()
	m.repo.EXPECT().AddManualItem(mock.Anything, shoppingListUserID.String(), itemID, (*string)(nil), 2.0).Return(nil)

	w := doShoppingListAddItem(m.router, map[string]any{"itemId": itemID, "quantity": 2}, shoppingListAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestShoppingListAddItem_UnknownItem_400(t *testing.T) {
	m := newShoppingListMocks(t)
	itemID := uuid.NewString()
	m.repo.EXPECT().AddManualItem(mock.Anything, shoppingListUserID.String(), itemID, (*string)(nil), 2.0).
		Return(models.ErrShoppingListInvalidItem)

	w := doShoppingListAddItem(m.router, map[string]any{"itemId": itemID, "quantity": 2}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_item", shoppingListErr(t, w))
}

func TestShoppingListAddItem_UnitNotAllowed_400(t *testing.T) {
	m := newShoppingListMocks(t)
	itemID := uuid.NewString()
	unitID := uuid.NewString()
	m.repo.EXPECT().AddManualItem(mock.Anything, shoppingListUserID.String(), itemID, &unitID, 2.0).
		Return(models.ErrIngredientUnitNotAllowed)

	w := doShoppingListAddItem(m.router, map[string]any{"itemId": itemID, "unitId": unitID, "quantity": 2}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "unit_not_allowed", shoppingListErr(t, w))
}

func TestShoppingListAddItem_RepoError_500(t *testing.T) {
	m := newShoppingListMocks(t)
	itemID := uuid.NewString()
	m.repo.EXPECT().AddManualItem(mock.Anything, shoppingListUserID.String(), itemID, (*string)(nil), 2.0).
		Return(errors.New("db down"))

	w := doShoppingListAddItem(m.router, map[string]any{"itemId": itemID, "quantity": 2}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestShoppingListUpdateItem_NoToken_401(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListUpdateItem(m.router, uuid.NewString(), map[string]any{"obtained": true}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestShoppingListUpdateItem_MalformedID_400(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListUpdateItem(m.router, "not-a-uuid", map[string]any{"obtained": true}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", shoppingListErr(t, w))
}

func TestShoppingListUpdateItem_NeitherFieldPresent_400(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListUpdateItem(m.router, uuid.NewString(), map[string]any{}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", shoppingListErr(t, w))
}

func TestShoppingListUpdateItem_ZeroQuantity_400(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListUpdateItem(m.router, uuid.NewString(), map[string]any{"quantity": 0}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_quantity", shoppingListErr(t, w))
}

func TestShoppingListUpdateItem_ObtainedOnly_Success_200(t *testing.T) {
	m := newShoppingListMocks(t)
	id := uuid.NewString()
	obtained := true
	m.repo.EXPECT().UpdateItem(mock.Anything, shoppingListUserID.String(), id, &obtained, (*float64)(nil)).Return(nil)

	w := doShoppingListUpdateItem(m.router, id, map[string]any{"obtained": true}, shoppingListAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "shopping list item updated", shoppingListMsg(t, w))
}

func TestShoppingListUpdateItem_QuantityOnly_Success_200(t *testing.T) {
	m := newShoppingListMocks(t)
	id := uuid.NewString()
	quantity := 6.0
	m.repo.EXPECT().UpdateItem(mock.Anything, shoppingListUserID.String(), id, (*bool)(nil), &quantity).Return(nil)

	w := doShoppingListUpdateItem(m.router, id, map[string]any{"quantity": 6}, shoppingListAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestShoppingListUpdateItem_BothFields_Success_200(t *testing.T) {
	m := newShoppingListMocks(t)
	id := uuid.NewString()
	obtained := false
	quantity := 9.0
	m.repo.EXPECT().UpdateItem(mock.Anything, shoppingListUserID.String(), id, &obtained, &quantity).Return(nil)

	w := doShoppingListUpdateItem(m.router, id, map[string]any{"obtained": false, "quantity": 9}, shoppingListAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestShoppingListUpdateItem_NotFound_404(t *testing.T) {
	m := newShoppingListMocks(t)
	id := uuid.NewString()
	obtained := true
	m.repo.EXPECT().UpdateItem(mock.Anything, shoppingListUserID.String(), id, &obtained, (*float64)(nil)).
		Return(models.ErrShoppingListItemNotFound)

	w := doShoppingListUpdateItem(m.router, id, map[string]any{"obtained": true}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "shopping_list_item_not_found", shoppingListErr(t, w))
}

func TestShoppingListUpdateItem_RepoError_500(t *testing.T) {
	m := newShoppingListMocks(t)
	id := uuid.NewString()
	obtained := true
	m.repo.EXPECT().UpdateItem(mock.Anything, shoppingListUserID.String(), id, &obtained, (*float64)(nil)).
		Return(errors.New("db down"))

	w := doShoppingListUpdateItem(m.router, id, map[string]any{"obtained": true}, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestShoppingListDeleteItem_NoToken_401(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListDeleteItem(m.router, uuid.NewString(), "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestShoppingListDeleteItem_MalformedID_400(t *testing.T) {
	m := newShoppingListMocks(t)
	w := doShoppingListDeleteItem(m.router, "not-a-uuid", shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", shoppingListErr(t, w))
}

func TestShoppingListDeleteItem_Success_200MessageBody(t *testing.T) {
	m := newShoppingListMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().DeleteItem(mock.Anything, shoppingListUserID.String(), id).Return(nil)

	w := doShoppingListDeleteItem(m.router, id, shoppingListAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "item removed from shopping list", shoppingListMsg(t, w))
}

func TestShoppingListDeleteItem_NotFound_404(t *testing.T) {
	m := newShoppingListMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().DeleteItem(mock.Anything, shoppingListUserID.String(), id).
		Return(models.ErrShoppingListItemNotFound)

	w := doShoppingListDeleteItem(m.router, id, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "shopping_list_item_not_found", shoppingListErr(t, w))
}

func TestShoppingListDeleteItem_RepoError_500(t *testing.T) {
	m := newShoppingListMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().DeleteItem(mock.Anything, shoppingListUserID.String(), id).
		Return(errors.New("db down"))

	w := doShoppingListDeleteItem(m.router, id, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestShoppingListGet_RepoError_500(t *testing.T) {
	m := newShoppingListMocks(t)
	m.repo.EXPECT().Get(mock.Anything, shoppingListUserID.String()).Return(nil, errors.New("db down"))

	w := doShoppingListGet(m.router, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
