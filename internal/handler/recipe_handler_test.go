package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

var (
	recipeUserID = uuid.MustParse("77777777-7777-7777-7777-777777777777")
	recipeItemID = uuid.MustParse("88888888-8888-8888-8888-888888888888")
	recipeUnitID = uuid.MustParse("99999999-9999-9999-9999-999999999999")
	recipeCatID  = uuid.MustParse("a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1")
)

func fakeCreatedRecipe() *models.Recipe {
	fn := "beef_stew_abc"
	fu := "https://res.cloudinary.com/demo/image/upload/beef_stew_abc.jpg"
	byName := "Cook Person"
	return &models.Recipe{
		ID:            uuid.MustParse("b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2"),
		Name:          "Slow Cooker Beef Stew",
		TimeInMinutes: 240,
		Serves:        4,
		Instructions:  []string{"Brown the beef", "Add everything else"},
		Notes:         []string{"Freezes well"},
		ImageURL:      &fu,
		ImageFilename: &fn,
		Approved:      false,
		CategoryIDs:   []uuid.UUID{recipeCatID},
		Ingredients:   []models.Ingredient{{ItemID: recipeItemID, UnitID: &recipeUnitID, Quantity: 800}},
		CreatedByID:   recipeUserID,
		CreatedByName: &byName,
		CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

type recipeMocks struct {
	repo       *genmocks.MockRecipeRepository
	transactor *genmocks.MockTransactor
	router     *gin.Engine
}

func newRecipeMocks(t *testing.T) *recipeMocks {
	m := &recipeMocks{
		repo:       genmocks.NewMockRecipeRepository(t),
		transactor: genmocks.NewMockTransactor(t),
	}
	m.transactor.EXPECT().WithinTx(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }).
		Maybe()
	h := handler.NewRecipeHandler(m.repo, m.transactor)
	m.router = gin.New()
	authed := m.router.Group("/")
	authed.Use(middleware.AuthMiddleware(testutil.TestAccessSecret))
	authed.POST("/recipes", h.Create)
	return m
}

func recipeAuth(t *testing.T, role string) string {
	t.Helper()
	return testutil.AuthHeader(t, "cook@example.com", recipeUserID.String(), role)
}

func validRecipeBody() map[string]any {
	return map[string]any{
		"name":          "Slow Cooker Beef Stew",
		"timeInMinutes": 240,
		"serves":        4,
		"instructions":  []string{"Brown the beef", "Add everything else"},
		"notes":         []string{"Freezes well"},
		"categoryIds":   []string{recipeCatID.String()},
		"ingredients": []map[string]any{
			{"itemId": recipeItemID.String(), "unitId": recipeUnitID.String(), "quantity": 800},
		},
	}
}

func doRecipeCreate(r *gin.Engine, body any, auth string) *httptest.ResponseRecorder {
	var reqBody *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(http.MethodPost, "/recipes", reqBody)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func recipeErr(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body["error"]
}

func TestRecipeCreate_NoToken(t *testing.T) {
	m := newRecipeMocks(t)

	w := doRecipeCreate(m.router, validRecipeBody(), "")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRecipeCreate_NonAdmin_201_ApprovedFalse(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.Recipe, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	w := doRecipeCreate(m.router, validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.False(t, captured.Approved)
	assert.Equal(t, recipeUserID, captured.CreatedByID)
}

func TestRecipeCreate_Admin_201_ApprovedTrue(t *testing.T) {
	m := newRecipeMocks(t)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.Recipe, error) {
			captured = in
			r := fakeCreatedRecipe()
			r.Approved = true
			return r, nil
		})

	w := doRecipeCreate(m.router, validRecipeBody(), recipeAuth(t, "ADMIN"))

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.True(t, captured.Approved)
}

func TestRecipeCreate_CapEnforcement(t *testing.T) {
	cases := []struct {
		name       string
		role       string
		count      int
		expectCall bool
		wantCode   int
		wantErr    string
	}{
		{"free at limit", "FREE", 5, true, http.StatusConflict, "recipe_limit_reached"},
		{"free over limit", "FREE", 9, true, http.StatusConflict, "recipe_limit_reached"},
		{"free under limit", "FREE", 4, true, http.StatusCreated, ""},
		{"premium never counted", "PREMIUM", 0, false, http.StatusCreated, ""},
		{"admin never counted", "ADMIN", 0, false, http.StatusCreated, ""},
		{"pro never counted", "PRO", 0, false, http.StatusCreated, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)
			if tc.expectCall {
				m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(tc.count, nil)
			}
			if tc.wantCode == http.StatusCreated {
				m.repo.EXPECT().Create(mock.Anything, mock.Anything).Return(fakeCreatedRecipe(), nil)
			}

			w := doRecipeCreate(m.router, validRecipeBody(), recipeAuth(t, tc.role))

			assert.Equal(t, tc.wantCode, w.Code)
			if tc.wantErr != "" {
				assert.Equal(t, tc.wantErr, recipeErr(t, w))
			}
		})
	}
}

func TestRecipeCreate_CountError_500(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, errors.New("db down"))

	w := doRecipeCreate(m.router, validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", recipeErr(t, w))
}

func TestRecipeCreate_Validation(t *testing.T) {
	cloudinary := "https://res.cloudinary.com/demo/image/upload/x.jpg"
	cases := []struct {
		name    string
		mutate  func(b map[string]any)
		wantErr string
	}{
		{"blank name", func(b map[string]any) { b["name"] = "  " }, "name_required"},
		{"name too short", func(b map[string]any) { b["name"] = "ab" }, "name_too_short"},
		{"name too long", func(b map[string]any) { b["name"] = strings.Repeat("a", 101) }, "name_too_long"},
		{"time zero", func(b map[string]any) { b["timeInMinutes"] = 0 }, "invalid_time"},
		{"time over max", func(b map[string]any) { b["timeInMinutes"] = 1441 }, "invalid_time"},
		{"serves zero", func(b map[string]any) { b["serves"] = 0 }, "invalid_serves"},
		{"serves over max", func(b map[string]any) { b["serves"] = 51 }, "invalid_serves"},
		{"instructions empty", func(b map[string]any) { b["instructions"] = []string{} }, "instructions_required"},
		{"too many instructions", func(b map[string]any) { b["instructions"] = make([]string, 51) }, "too_many_instructions"},
		{"blank instruction element", func(b map[string]any) { b["instructions"] = []string{"ok", "   "} }, "invalid_instruction"},
		{"too many notes", func(b map[string]any) { b["notes"] = make([]string, 11) }, "too_many_notes"},
		{"categories empty", func(b map[string]any) { b["categoryIds"] = []string{} }, "categories_required"},
		{"too many categories", func(b map[string]any) {
			b["categoryIds"] = []string{uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()}
		}, "too_many_categories"},
		{"duplicate category", func(b map[string]any) {
			id := uuid.NewString()
			b["categoryIds"] = []string{id, id}
		}, "duplicate_category"},
		{"malformed category id", func(b map[string]any) { b["categoryIds"] = []string{"not-a-uuid"} }, "invalid_request"},
		{"ingredients empty", func(b map[string]any) { b["ingredients"] = []map[string]any{} }, "ingredients_required"},
		{"too many ingredients", func(b map[string]any) {
			ings := make([]map[string]any, 51)
			for i := range ings {
				ings[i] = map[string]any{"itemId": uuid.NewString(), "quantity": 1}
			}
			b["ingredients"] = ings
		}, "too_many_ingredients"},
		{"duplicate ingredient", func(b map[string]any) {
			id := uuid.NewString()
			b["ingredients"] = []map[string]any{
				{"itemId": id, "quantity": 1},
				{"itemId": id, "quantity": 2},
			}
		}, "duplicate_ingredient"},
		{"malformed item id", func(b map[string]any) {
			b["ingredients"] = []map[string]any{{"itemId": "nope", "quantity": 1}}
		}, "invalid_request"},
		{"malformed unit id", func(b map[string]any) {
			b["ingredients"] = []map[string]any{{"itemId": uuid.NewString(), "unitId": "nope", "quantity": 1}}
		}, "invalid_request"},
		{"zero quantity", func(b map[string]any) {
			b["ingredients"] = []map[string]any{{"itemId": uuid.NewString(), "quantity": 0}}
		}, "invalid_quantity"},
		{"negative quantity", func(b map[string]any) {
			b["ingredients"] = []map[string]any{{"itemId": uuid.NewString(), "quantity": -3}}
		}, "invalid_quantity"},
		{"missing quantity", func(b map[string]any) {
			b["ingredients"] = []map[string]any{{"itemId": uuid.NewString()}}
		}, "invalid_quantity"},
		{"image url only", func(b map[string]any) { b["image"] = map[string]any{"url": cloudinary} }, "invalid_image"},
		{"image filename only", func(b map[string]any) { b["image"] = map[string]any{"filename": "x"} }, "invalid_image"},
		{"image non-cloudinary host", func(b map[string]any) {
			b["image"] = map[string]any{"url": "https://evil.example.com/x.jpg", "filename": "x"}
		}, "invalid_image"},
		{"image http scheme", func(b map[string]any) {
			b["image"] = map[string]any{"url": "http://res.cloudinary.com/demo/x.jpg", "filename": "x"}
		}, "invalid_image"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)
			body := validRecipeBody()
			tc.mutate(body)

			w := doRecipeCreate(m.router, body, recipeAuth(t, "FREE"))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, tc.wantErr, recipeErr(t, w))
		})
	}
}

func TestRecipeCreate_InvalidJSON(t *testing.T) {
	m := newRecipeMocks(t)

	req := httptest.NewRequest(http.MethodPost, "/recipes", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", recipeAuth(t, "FREE"))
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", recipeErr(t, w))
}

func TestRecipeCreate_NotesEmptyElementsDropped(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.Recipe, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	body := validRecipeBody()
	body["notes"] = []string{"", "keep this", "   "}

	w := doRecipeCreate(m.router, body, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, []string{"keep this"}, captured.Notes)
}

func TestRecipeCreate_ImageAccepted(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.Recipe, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	body := validRecipeBody()
	body["image"] = map[string]any{
		"url":      "https://res.cloudinary.com/demo/image/upload/v1/beef.jpg",
		"filename": "beef",
	}

	w := doRecipeCreate(m.router, body, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusCreated, w.Code)
	require.NotNil(t, captured.ImageURL)
	require.NotNil(t, captured.ImageFilename)
	assert.Equal(t, "https://res.cloudinary.com/demo/image/upload/v1/beef.jpg", *captured.ImageURL)
	assert.Equal(t, "beef", *captured.ImageFilename)
}

func TestRecipeCreate_NoImage(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.Recipe, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	body := validRecipeBody()
	delete(body, "image")

	w := doRecipeCreate(m.router, body, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Nil(t, captured.ImageURL)
	assert.Nil(t, captured.ImageFilename)
}

func TestRecipeCreate_PassesParsedInputToRepo(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.Recipe, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	w := doRecipeCreate(m.router, validRecipeBody(), recipeAuth(t, "FREE"))
	require.Equal(t, http.StatusCreated, w.Code)

	assert.Equal(t, "Slow Cooker Beef Stew", captured.Name)
	assert.Equal(t, 240, captured.TimeInMinutes)
	assert.Equal(t, 4, captured.Serves)
	assert.Equal(t, []string{"Brown the beef", "Add everything else"}, captured.Instructions)
	assert.Equal(t, []uuid.UUID{recipeCatID}, captured.CategoryIDs)
	require.Len(t, captured.Ingredients, 1)
	assert.Equal(t, recipeItemID, captured.Ingredients[0].ItemID)
	require.NotNil(t, captured.Ingredients[0].UnitID)
	assert.Equal(t, recipeUnitID, *captured.Ingredients[0].UnitID)
	assert.Equal(t, 800.0, captured.Ingredients[0].Quantity)
}

func TestRecipeCreate_IngredientWithoutUnit(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.Recipe, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	body := validRecipeBody()
	body["ingredients"] = []map[string]any{{"itemId": recipeItemID.String(), "quantity": 3}}

	w := doRecipeCreate(m.router, body, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusCreated, w.Code)
	require.Len(t, captured.Ingredients, 1)
	assert.Nil(t, captured.Ingredients[0].UnitID)
}

func TestRecipeCreate_RepoErrorTranslation(t *testing.T) {
	cases := []struct {
		name     string
		repoErr  error
		wantCode int
		wantErr  string
	}{
		{"invalid item", models.ErrRecipeInvalidItem, http.StatusBadRequest, "invalid_item_id"},
		{"invalid unit", models.ErrRecipeInvalidUnit, http.StatusBadRequest, "invalid_unit_id"},
		{"invalid category", models.ErrRecipeInvalidCategory, http.StatusBadRequest, "invalid_category_id"},
		{"unit not allowed", models.ErrIngredientUnitNotAllowed, http.StatusBadRequest, "unit_not_allowed_for_item"},
		{"duplicate ingredient", models.ErrRecipeDuplicateIngredient, http.StatusBadRequest, "duplicate_ingredient"},
		{"generic error", errors.New("kaboom"), http.StatusInternalServerError, "server_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)
			m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
			m.repo.EXPECT().Create(mock.Anything, mock.Anything).Return(nil, tc.repoErr)

			w := doRecipeCreate(m.router, validRecipeBody(), recipeAuth(t, "FREE"))

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantErr, recipeErr(t, w))
		})
	}
}

func TestRecipeCreate_ResponseShape(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).Return(fakeCreatedRecipe(), nil)

	w := doRecipeCreate(m.router, validRecipeBody(), recipeAuth(t, "FREE"))

	require.Equal(t, http.StatusCreated, w.Code)
	var got models.Recipe
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, fakeCreatedRecipe().ID, got.ID)
	assert.Equal(t, "Slow Cooker Beef Stew", got.Name)
	assert.False(t, got.Approved)
	assert.Equal(t, []uuid.UUID{recipeCatID}, got.CategoryIDs)
	require.Len(t, got.Ingredients, 1)
	assert.Equal(t, recipeItemID, got.Ingredients[0].ItemID)
	require.NotNil(t, got.CreatedByName)
	assert.Equal(t, "Cook Person", *got.CreatedByName)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	assert.Contains(t, raw, "createdByName")
	assert.Contains(t, raw, "imageUrl")
	assert.NotContains(t, raw, "description")
}
