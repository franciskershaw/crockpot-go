package handler_test

import (
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
	repo   *genmocks.MockShoppingListRepository
	router *gin.Engine
}

func newShoppingListMocks(t *testing.T) *shoppingListMocks {
	m := &shoppingListMocks{repo: genmocks.NewMockShoppingListRepository(t)}
	h := handler.NewShoppingListHandler(m.repo)
	m.router = gin.New()
	authed := m.router.Group("/shopping-list")
	authed.Use(middleware.AuthMiddleware(testutil.TestAccessSecret))
	{
		authed.GET("", h.Get)
	}
	return m
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

func TestShoppingListGet_RepoError_500(t *testing.T) {
	m := newShoppingListMocks(t)
	m.repo.EXPECT().Get(mock.Anything, shoppingListUserID.String()).Return(nil, errors.New("db down"))

	w := doShoppingListGet(m.router, shoppingListAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
