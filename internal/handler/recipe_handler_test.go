package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
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
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

var (
	recipeUserID = uuid.MustParse("77777777-7777-7777-7777-777777777777")
	recipeItemID = uuid.MustParse("88888888-8888-8888-8888-888888888888")
	recipeUnitID = uuid.MustParse("99999999-9999-9999-9999-999999999999")
	recipeCatID  = uuid.MustParse("a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1")
	recipeID     = uuid.MustParse("c3c3c3c3-c3c3-c3c3-c3c3-c3c3c3c3c3c3")
)

func fakeCreatedRecipe() *models.RecipeDetail {
	fn := "beef_stew_abc"
	fu := "https://res.cloudinary.com/demo/image/upload/beef_stew_abc.jpg"
	byName := "Cook Person"
	return &models.RecipeDetail{
		RecipeCard: models.RecipeCard{
			ID:            uuid.MustParse("b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2"),
			Name:          "Slow Cooker Beef Stew",
			TimeInMinutes: 240,
			Serves:        4,
			ImageURL:      &fu,
			ImageFilename: &fn,
			Approved:      false,
			Categories:    []models.CategoryRef{{ID: recipeCatID, Name: "Dinner"}},
			CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		Instructions:  []string{"Brown the beef", "Add everything else"},
		Notes:         []string{"Freezes well"},
		Ingredients:   []models.HydratedIngredient{{ItemID: recipeItemID, ItemName: "Beef", ItemCategoryID: uuid.New(), ItemCategoryName: "Meat", UnitID: &recipeUnitID, Quantity: 800}},
		CreatedByID:   recipeUserID,
		CreatedByName: &byName,
	}
}

type recipeMocks struct {
	repo          *genmocks.MockRecipeRepository
	shoppingLists *genmocks.MockShoppingListRegenerator
	transactor    *genmocks.MockTransactor
	images        *genmocks.MockImageStore
	committed     bool // set once a WithinTx callback returns nil
	router        *gin.Engine
}

func newRecipeMocks(t *testing.T) *recipeMocks {
	return newRecipeMocksWithPhotoLimit(t, 20)
}

func newRecipeMocksWithPhotoLimit(t *testing.T, photoLimit int64) *recipeMocks {
	m := &recipeMocks{
		repo:          genmocks.NewMockRecipeRepository(t),
		shoppingLists: genmocks.NewMockShoppingListRegenerator(t),
		transactor:    genmocks.NewMockTransactor(t),
		images:        genmocks.NewMockImageStore(t),
	}
	m.transactor.EXPECT().WithinTx(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			err := fn(ctx)
			if err == nil {
				m.committed = true
			}
			return err
		}).
		Maybe()
	ids := 0
	h := handler.NewRecipeHandler(m.repo, m.shoppingLists, m.transactor, handler.RecipeImages{
		Store:        m.images,
		Scope:        handler.ImageScope{UploadFolder: "dev/recipes"},
		PhotoLimiter: limiter.New(memory.NewStore(), limiter.Rate{Period: time.Hour, Limit: photoLimit}),
		NewID: func() string {
			ids++
			return fmt.Sprintf("id-%d", ids)
		},
	})
	m.router = gin.New()
	authed := m.router.Group("/")
	authed.Use(middleware.AuthMiddleware(testutil.TestAccessSecret))
	authed.POST("/recipes", h.Create)
	authed.PATCH("/recipes/:id", h.Update)
	authed.DELETE("/recipes/:id", h.Delete)
	authed.GET("/recipes/favourites", h.ListFavourites)
	authed.POST("/recipes/:id/favourite", h.AddFavourite)
	authed.DELETE("/recipes/:id/favourite", h.RemoveFavourite)
	optional := m.router.Group("/")
	optional.Use(middleware.OptionalAuthMiddleware(testutil.TestAccessSecret))
	optional.GET("/recipes", h.List)
	optional.GET("/recipes/:id", h.Get)
	m.router.GET("/recipes/time-range", h.GetTimeRange)
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

// recipeMultipart builds the recipe write body: a "recipe" JSON part (omitted when body is nil) and an optional "photo" part.
func recipeMultipart(t *testing.T, body any, photo []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		require.NoError(t, mw.WriteField("recipe", string(b)))
	}
	if photo != nil {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="photo"; filename="photo.jpg"`)
		h.Set("Content-Type", "image/jpeg")
		part, err := mw.CreatePart(h)
		require.NoError(t, err)
		_, err = part.Write(photo)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}

func doRecipeWrite(t *testing.T, r *gin.Engine, method, path string, body any, photo []byte, auth string) *httptest.ResponseRecorder {
	t.Helper()
	buf, contentType := recipeMultipart(t, body, photo)
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", contentType)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doRecipeCreate(t *testing.T, r *gin.Engine, body any, auth string) *httptest.ResponseRecorder {
	t.Helper()
	return doRecipeWrite(t, r, http.MethodPost, "/recipes", body, nil, auth)
}

func doRecipeUpdate(t *testing.T, r *gin.Engine, id string, body any, auth string) *httptest.ResponseRecorder {
	t.Helper()
	return doRecipeWrite(t, r, http.MethodPatch, "/recipes/"+id, body, nil, auth)
}

func doRecipeDelete(r *gin.Engine, id string, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/recipes/"+id, nil)
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

	w := doRecipeCreate(t, m.router, validRecipeBody(), "")

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRecipeCreate_NonAdmin_201_ApprovedFalse(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.RecipeDetail, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	w := doRecipeCreate(t, m.router, validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.False(t, captured.Approved)
	assert.Equal(t, recipeUserID, captured.CreatedByID)
}

func TestRecipeCreate_Admin_201_ApprovedTrue(t *testing.T) {
	m := newRecipeMocks(t)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.RecipeDetail, error) {
			captured = in
			r := fakeCreatedRecipe()
			r.Approved = true
			return r, nil
		})

	w := doRecipeCreate(t, m.router, validRecipeBody(), recipeAuth(t, "ADMIN"))

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

			w := doRecipeCreate(t, m.router, validRecipeBody(), recipeAuth(t, tc.role))

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

	w := doRecipeCreate(t, m.router, validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", recipeErr(t, w))
}

func TestRecipeCreate_Validation(t *testing.T) {
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)
			body := validRecipeBody()
			tc.mutate(body)

			w := doRecipeCreate(t, m.router, body, recipeAuth(t, "FREE"))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, tc.wantErr, recipeErr(t, w))
		})
	}
}

func TestRecipeWrite_RejectsBadRequestShape(t *testing.T) {
	jsonBody := func() (*bytes.Buffer, string) {
		b, _ := json.Marshal(validRecipeBody())
		return bytes.NewBuffer(b), "application/json"
	}
	malformedRecipePart := func() (*bytes.Buffer, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("recipe", "{not json")
		_ = mw.Close()
		return &buf, mw.FormDataContentType()
	}
	missingRecipePart := func() (*bytes.Buffer, string) { return recipeMultipart(t, nil, nil) }

	cases := []struct {
		name   string
		method string
		path   string
		build  func() (*bytes.Buffer, string)
	}{
		{"create: JSON body", http.MethodPost, "/recipes", jsonBody},
		{"update: JSON body", http.MethodPatch, "/recipes/" + uuid.NewString(), jsonBody},
		{"create: malformed recipe part", http.MethodPost, "/recipes", malformedRecipePart},
		{"create: missing recipe part", http.MethodPost, "/recipes", missingRecipePart},
		{"update: missing recipe part", http.MethodPatch, "/recipes/" + uuid.NewString(), missingRecipePart},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)
			buf, contentType := tc.build()
			req := httptest.NewRequest(tc.method, tc.path, buf)
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("Authorization", recipeAuth(t, "FREE"))
			w := httptest.NewRecorder()
			m.router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "invalid_request", recipeErr(t, w))
		})
	}
}

func TestRecipeCreate_NotesEmptyElementsDropped(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.RecipeDetail, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	body := validRecipeBody()
	body["notes"] = []string{"", "keep this", "   "}

	w := doRecipeCreate(t, m.router, body, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, []string{"keep this"}, captured.Notes)
}

var (
	jpegBytes = append([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"), make([]byte, 64)...)
	pngBytes  = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	webpBytes = append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 64)...)
)

func expectCreateSucceeds(m *recipeMocks) {
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).Return(fakeCreatedRecipe(), nil)
}

func TestRecipeCreate_AcceptsImagePhotos(t *testing.T) {
	for name, photo := range map[string][]byte{"jpeg": jpegBytes, "png": pngBytes, "webp": webpBytes} {
		t.Run(name, func(t *testing.T) {
			m := newRecipeMocks(t)
			expectCreateSucceeds(m)
			expectUploadSucceeds(m, "dev/recipes/id-1")

			w := doRecipeWrite(t, m.router, http.MethodPost, "/recipes", validRecipeBody(), photo, recipeAuth(t, "FREE"))

			assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		})
	}
}

func TestRecipeWrite_RejectsBadPhotos(t *testing.T) {
	notAnImage := []byte("hello, this is plain text pretending to be a jpeg")
	oversize := append(append([]byte{}, jpegBytes...), make([]byte, 5<<20)...)
	withRemove := func() map[string]any {
		b := validRecipeBody()
		b["removeImage"] = true
		return b
	}

	cases := []struct {
		name    string
		method  string
		path    string
		body    map[string]any
		photo   []byte
		wantErr string
	}{
		{"create: non-image content declared as jpeg", http.MethodPost, "/recipes", validRecipeBody(), notAnImage, "invalid_image"},
		{"update: non-image content declared as jpeg", http.MethodPatch, "/recipes/" + uuid.NewString(), validRecipeBody(), notAnImage, "invalid_image"},
		{"create: photo over 5 MB", http.MethodPost, "/recipes", validRecipeBody(), oversize, "image_too_large"},
		{"update: photo and removeImage together", http.MethodPatch, "/recipes/" + uuid.NewString(), withRemove(), jpegBytes, "invalid_image"},
		{"create: removeImage is meaningless", http.MethodPost, "/recipes", withRemove(), nil, "invalid_image"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)

			w := doRecipeWrite(t, m.router, tc.method, tc.path, tc.body, tc.photo, recipeAuth(t, "FREE"))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, tc.wantErr, recipeErr(t, w))
		})
	}
}

func TestRecipeCreate_StaleImageFieldIgnored(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.RecipeDetail, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	body := validRecipeBody()
	body["image"] = map[string]any{"url": "https://evil.example.com/x.jpg", "filename": "x"}

	w := doRecipeCreate(t, m.router, body, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Nil(t, captured.ImageURL)
	assert.Nil(t, captured.ImageFilename)
}

func TestRecipeCreate_PassesParsedInputToRepo(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.RecipeDetail, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	w := doRecipeCreate(t, m.router, validRecipeBody(), recipeAuth(t, "FREE"))
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
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.RecipeDetail, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	body := validRecipeBody()
	body["ingredients"] = []map[string]any{{"itemId": recipeItemID.String(), "quantity": 3}}

	w := doRecipeCreate(t, m.router, body, recipeAuth(t, "FREE"))

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
		{"non-ingredient item", models.ErrRecipeNonIngredientItem, http.StatusBadRequest, "item_not_ingredient"},
		{"duplicate ingredient", models.ErrRecipeDuplicateIngredient, http.StatusBadRequest, "duplicate_ingredient"},
		{"generic error", errors.New("kaboom"), http.StatusInternalServerError, "server_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)
			m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
			m.repo.EXPECT().Create(mock.Anything, mock.Anything).Return(nil, tc.repoErr)

			w := doRecipeCreate(t, m.router, validRecipeBody(), recipeAuth(t, "FREE"))

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantErr, recipeErr(t, w))
		})
	}
}

func TestRecipeCreate_ResponseShape(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).Return(fakeCreatedRecipe(), nil)

	w := doRecipeCreate(t, m.router, validRecipeBody(), recipeAuth(t, "FREE"))

	require.Equal(t, http.StatusCreated, w.Code)
	var got models.RecipeDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, fakeCreatedRecipe().ID, got.ID)
	assert.Equal(t, "Slow Cooker Beef Stew", got.Name)
	assert.False(t, got.Approved)
	require.Len(t, got.Categories, 1)
	assert.Equal(t, recipeCatID, got.Categories[0].ID)
	require.Len(t, got.Ingredients, 1)
	assert.Equal(t, recipeItemID, got.Ingredients[0].ItemID)
	require.NotNil(t, got.CreatedByName)
	assert.Equal(t, "Cook Person", *got.CreatedByName)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	assert.Contains(t, raw, "createdByName")
	assert.Contains(t, raw, "imageUrl")
	assert.Contains(t, raw, "description")
}

func doRecipeList(r *gin.Engine, rawQuery, auth string) *httptest.ResponseRecorder {
	url := "/recipes"
	if rawQuery != "" {
		url += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doRecipeTimeRange(r *gin.Engine) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/recipes/time-range", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doRecipeGet(r *gin.Engine, id, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/recipes/"+id, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doRecipeFavourite(r *gin.Engine, method, id, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/recipes/"+id+"/favourite", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doRecipeListFavourites(r *gin.Engine, rawQuery, auth string) *httptest.ResponseRecorder {
	url := "/recipes/favourites"
	if rawQuery != "" {
		url += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func recipeMsg(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body["message"]
}

func fakeRecipeCard() *models.RecipeCard {
	return &models.RecipeCard{
		ID:            uuid.MustParse("c3c3c3c3-c3c3-c3c3-c3c3-c3c3c3c3c3c3"),
		Name:          "Slow Cooker Beef Stew",
		TimeInMinutes: 240,
		Serves:        4,
		Approved:      true,
		Categories:    []models.CategoryRef{{ID: recipeCatID, Name: "Batch"}},
		CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestRecipeList_DefaultsWhenNoParams(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().List(mock.Anything, mock.MatchedBy(func(f models.RecipeListFilter) bool {
		return f.Page == 1 && f.Limit == 20 &&
			f.Query == "" && !f.Mine && !f.CallerIsAdmin && f.CallerID == nil &&
			f.MinTime == 0 && f.MaxTime == 0 &&
			len(f.IncludeCategoryIDs) == 0 && len(f.ExcludeCategoryIDs) == 0 && len(f.IngredientIDs) == 0
	})).Return([]*models.RecipeCard{}, 0, nil)

	w := doRecipeList(m.router, "", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecipeList_ParsesAllParams(t *testing.T) {
	m := newRecipeMocks(t)
	cat := uuid.NewString()
	ing := uuid.NewString()
	m.repo.EXPECT().List(mock.Anything, mock.MatchedBy(func(f models.RecipeListFilter) bool {
		return f.Query == "beef" &&
			len(f.ExcludeCategoryIDs) == 1 && f.ExcludeCategoryIDs[0].String() == cat &&
			len(f.IncludeCategoryIDs) == 0 &&
			len(f.IngredientIDs) == 1 && f.IngredientIDs[0].String() == ing &&
			f.MinTime == 20 && f.MaxTime == 90 && f.Mine &&
			f.CallerID != nil && *f.CallerID == recipeUserID.String() && f.CallerIsAdmin &&
			f.Page == 2 && f.Limit == 10
	})).Return([]*models.RecipeCard{}, 0, nil)

	q := "q=beef&categoryId=" + cat + "&categoryMode=exclude&ingredientId=" + ing +
		"&minTime=20&maxTime=90&mine=true&page=2&limit=10"
	w := doRecipeList(m.router, q, recipeAuth(t, "ADMIN"))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecipeList_ParsesSeedParam(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().List(mock.Anything, mock.MatchedBy(func(f models.RecipeListFilter) bool {
		return f.Seed == "abc123"
	})).Return([]*models.RecipeCard{}, 0, nil)

	w := doRecipeList(m.router, "seed=abc123", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecipeList_SeedDefaultsToEmpty(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().List(mock.Anything, mock.MatchedBy(func(f models.RecipeListFilter) bool {
		return f.Seed == ""
	})).Return([]*models.RecipeCard{}, 0, nil)

	w := doRecipeList(m.router, "", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecipeList_CategoryModeIncludeIsDefault(t *testing.T) {
	m := newRecipeMocks(t)
	cat := uuid.NewString()
	m.repo.EXPECT().List(mock.Anything, mock.MatchedBy(func(f models.RecipeListFilter) bool {
		return len(f.IncludeCategoryIDs) == 1 && f.IncludeCategoryIDs[0].String() == cat && len(f.ExcludeCategoryIDs) == 0
	})).Return([]*models.RecipeCard{}, 0, nil)

	w := doRecipeList(m.router, "categoryId="+cat, "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecipeList_ClampsPageLimitAndTime(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().List(mock.Anything, mock.MatchedBy(func(f models.RecipeListFilter) bool {
		return f.Page == 1 && f.Limit == 50 &&
			f.MinTime == 0 && f.MaxTime == 1_000_000
	})).Return([]*models.RecipeCard{}, 0, nil)

	w := doRecipeList(m.router, "page=0&limit=999&minTime=-5&maxTime=99999999999", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecipeList_BadInput400(t *testing.T) {
	cases := map[string]string{
		"malformed categoryId":   "categoryId=not-a-uuid",
		"malformed ingredientId": "ingredientId=nope",
		"unknown categoryMode":   "categoryMode=weird",
		"non-numeric page":       "page=abc",
		"non-numeric limit":      "limit=ten",
		"non-numeric minTime":    "minTime=soon",
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			m := newRecipeMocks(t)
			w := doRecipeList(m.router, q, "")
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "invalid_request", recipeErr(t, w))
		})
	}
}

func TestRecipeList_EnvelopeShape(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().List(mock.Anything, mock.Anything).
		Return([]*models.RecipeCard{fakeRecipeCard(), fakeRecipeCard()}, 25, nil)

	w := doRecipeList(m.router, "limit=10", "")
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Recipes    []map[string]json.RawMessage `json:"recipes"`
		Page       int                          `json:"page"`
		Limit      int                          `json:"limit"`
		Total      int                          `json:"total"`
		TotalPages int                          `json:"totalPages"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Len(t, body.Recipes, 2)
	assert.Equal(t, 1, body.Page)
	assert.Equal(t, 10, body.Limit)
	assert.Equal(t, 25, body.Total)
	assert.Equal(t, 3, body.TotalPages)
	assert.Contains(t, body.Recipes[0], "categories")
	assert.NotContains(t, body.Recipes[0], "ingredients")
	assert.NotContains(t, body.Recipes[0], "instructions")
}

func TestRecipeList_EmptyRecipesSerializesAsArray(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, 0, nil)

	w := doRecipeList(m.router, "", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"recipes":[]`)
	assert.Contains(t, w.Body.String(), `"totalPages":0`)
}

func TestRecipeList_RepoError500(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, 0, errors.New("db down"))

	w := doRecipeList(m.router, "", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", recipeErr(t, w))
}

func TestRecipeTimeRange_Success200(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().GetTimeRange(mock.Anything).
		Return(&models.RecipeTimeRange{MinTime: 15, MaxTime: 240}, nil)

	w := doRecipeTimeRange(m.router)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		MinTime int `json:"minTime"`
		MaxTime int `json:"maxTime"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, 15, body.MinTime)
	assert.Equal(t, 240, body.MaxTime)
}

func TestRecipeTimeRange_EmptyCatalogFallbackPassedThrough(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().GetTimeRange(mock.Anything).
		Return(&models.RecipeTimeRange{MinTime: 0, MaxTime: 120}, nil)

	w := doRecipeTimeRange(m.router)
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"minTime":0,"maxTime":120}`, w.Body.String())
}

func TestRecipeTimeRange_RepoError500(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().GetTimeRange(mock.Anything).Return(nil, errors.New("db down"))

	w := doRecipeTimeRange(m.router)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", recipeErr(t, w))
}

func TestRecipeGet_MalformedID400(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeGet(m.router, "not-a-uuid", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", recipeErr(t, w))
}

func TestRecipeGet_NotFound404(t *testing.T) {
	m := newRecipeMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().GetByID(mock.Anything, id, mock.Anything, mock.Anything).
		Return(nil, models.ErrRecipeNotFound)

	w := doRecipeGet(m.router, id, "")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", recipeErr(t, w))
}

func TestRecipeGet_RepoError500(t *testing.T) {
	m := newRecipeMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().GetByID(mock.Anything, id, mock.Anything, mock.Anything).
		Return(nil, errors.New("db down"))

	w := doRecipeGet(m.router, id, "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRecipeGet_Success200(t *testing.T) {
	m := newRecipeMocks(t)
	id := uuid.NewString()
	detail := &models.RecipeDetail{
		RecipeCard:   *fakeRecipeCard(),
		Instructions: []string{"step"},
		Notes:        []string{},
		Ingredients:  []models.HydratedIngredient{{ItemID: recipeItemID, ItemName: "beef", ItemCategoryName: "Meat", Quantity: 800}},
	}
	m.repo.EXPECT().GetByID(mock.Anything, id, mock.Anything, mock.Anything).Return(detail, nil)

	w := doRecipeGet(m.router, id, "")
	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	assert.Contains(t, raw, "ingredients")
	assert.Contains(t, raw, "description")
	assert.Contains(t, raw, "categories")
}

func TestRecipeGet_ThreadsCallerIdentity(t *testing.T) {
	t.Run("admin caller", func(t *testing.T) {
		m := newRecipeMocks(t)
		id := uuid.NewString()
		m.repo.EXPECT().GetByID(mock.Anything, id,
			mock.MatchedBy(func(cid *string) bool { return cid != nil && *cid == recipeUserID.String() }),
			true,
		).Return(&models.RecipeDetail{RecipeCard: *fakeRecipeCard()}, nil)

		w := doRecipeGet(m.router, id, recipeAuth(t, "ADMIN"))
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("anonymous caller", func(t *testing.T) {
		m := newRecipeMocks(t)
		id := uuid.NewString()
		m.repo.EXPECT().GetByID(mock.Anything, id,
			mock.MatchedBy(func(cid *string) bool { return cid == nil }),
			false,
		).Return(&models.RecipeDetail{RecipeCard: *fakeRecipeCard()}, nil)

		w := doRecipeGet(m.router, id, "")
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestRecipeAddFavourite_NoToken_401(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeFavourite(m.router, http.MethodPost, uuid.NewString(), "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRecipeAddFavourite_MalformedID_400(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeFavourite(m.router, http.MethodPost, "not-a-uuid", recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", recipeErr(t, w))
}

func TestRecipeAddFavourite_Success_200MessageBody(t *testing.T) {
	m := newRecipeMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().AddFavourite(mock.Anything, recipeUserID.String(), id, false).Return(nil)

	w := doRecipeFavourite(m.router, http.MethodPost, id, recipeAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "recipe favourited", recipeMsg(t, w))
}

func TestRecipeAddFavourite_NotFound_404(t *testing.T) {
	m := newRecipeMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().AddFavourite(mock.Anything, recipeUserID.String(), id, false).
		Return(models.ErrRecipeNotFound)

	w := doRecipeFavourite(m.router, http.MethodPost, id, recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", recipeErr(t, w))
}

func TestRecipeAddFavourite_RepoError_500(t *testing.T) {
	m := newRecipeMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().AddFavourite(mock.Anything, recipeUserID.String(), id, false).
		Return(errors.New("db down"))

	w := doRecipeFavourite(m.router, http.MethodPost, id, recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRecipeAddFavourite_ThreadsCallerIsAdmin(t *testing.T) {
	m := newRecipeMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().AddFavourite(mock.Anything, recipeUserID.String(), id, true).Return(nil)

	w := doRecipeFavourite(m.router, http.MethodPost, id, recipeAuth(t, "ADMIN"))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecipeRemoveFavourite_NoToken_401(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeFavourite(m.router, http.MethodDelete, uuid.NewString(), "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRecipeRemoveFavourite_MalformedID_400(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeFavourite(m.router, http.MethodDelete, "not-a-uuid", recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", recipeErr(t, w))
}

func TestRecipeRemoveFavourite_Success_200MessageBody(t *testing.T) {
	m := newRecipeMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().RemoveFavourite(mock.Anything, recipeUserID.String(), id).Return(nil)

	w := doRecipeFavourite(m.router, http.MethodDelete, id, recipeAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "recipe unfavourited", recipeMsg(t, w))
}

func TestRecipeRemoveFavourite_RepoError_500(t *testing.T) {
	m := newRecipeMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().RemoveFavourite(mock.Anything, recipeUserID.String(), id).
		Return(errors.New("db down"))

	w := doRecipeFavourite(m.router, http.MethodDelete, id, recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRecipeListFavourites_NoToken_401(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeListFavourites(m.router, "", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRecipeListFavourites_EnvelopeShape(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().ListFavourites(mock.Anything, recipeUserID.String(), 1, 10).
		Return([]*models.RecipeCard{fakeRecipeCard(), fakeRecipeCard()}, 25, nil)

	w := doRecipeListFavourites(m.router, "limit=10", recipeAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Recipes    []map[string]json.RawMessage `json:"recipes"`
		Page       int                          `json:"page"`
		Limit      int                          `json:"limit"`
		Total      int                          `json:"total"`
		TotalPages int                          `json:"totalPages"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Len(t, body.Recipes, 2)
	assert.Equal(t, 1, body.Page)
	assert.Equal(t, 10, body.Limit)
	assert.Equal(t, 25, body.Total)
	assert.Equal(t, 3, body.TotalPages)
}

func TestRecipeListFavourites_EmptyRecipesSerializesAsArray(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().ListFavourites(mock.Anything, recipeUserID.String(), 1, 20).
		Return(nil, 0, nil)

	w := doRecipeListFavourites(m.router, "", recipeAuth(t, "FREE"))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"recipes":[]`)
	assert.Contains(t, w.Body.String(), `"totalPages":0`)
}

func TestRecipeListFavourites_DefaultsPageLimit(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().ListFavourites(mock.Anything, recipeUserID.String(), 1, 20).
		Return([]*models.RecipeCard{}, 0, nil)

	w := doRecipeListFavourites(m.router, "", recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecipeListFavourites_ClampsPageAndLimit(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().ListFavourites(mock.Anything, recipeUserID.String(), 1, 50).
		Return([]*models.RecipeCard{}, 0, nil)

	w := doRecipeListFavourites(m.router, "page=0&limit=999", recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecipeListFavourites_BadInput400(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeListFavourites(m.router, "page=notanumber", recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_request", recipeErr(t, w))
}

func TestRecipeListFavourites_RepoError_500(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().ListFavourites(mock.Anything, recipeUserID.String(), 1, 20).
		Return(nil, 0, errors.New("db down"))

	w := doRecipeListFavourites(m.router, "", recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRecipeCreate_DescriptionAccepted(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.RecipeDetail, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	body := validRecipeBody()
	body["description"] = "  a hearty stew  "

	w := doRecipeCreate(t, m.router, body, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusCreated, w.Code)
	require.NotNil(t, captured.Description)
	assert.Equal(t, "a hearty stew", *captured.Description)
}

func TestRecipeCreate_DescriptionTooLong_400(t *testing.T) {
	m := newRecipeMocks(t)
	body := validRecipeBody()
	body["description"] = strings.Repeat("a", 501)

	w := doRecipeCreate(t, m.router, body, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "description_too_long", recipeErr(t, w))
}

func TestRecipeCreate_DescriptionOmitted(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.RecipeDetail, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	w := doRecipeCreate(t, m.router, validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Nil(t, captured.Description)
}

func TestRecipeUpdate_Unauthenticated_401(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeUpdate(t, m.router, recipeID.String(), validRecipeBody(), "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRecipeUpdate_InvalidID_400(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeUpdate(t, m.router, "not-a-uuid", validRecipeBody(), recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRecipeUpdate_ValidationError_400(t *testing.T) {
	m := newRecipeMocks(t)
	body := validRecipeBody()
	body["name"] = ""
	w := doRecipeUpdate(t, m.router, recipeID.String(), body, recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "name_required", recipeErr(t, w))
}

func TestRecipeUpdate_NotFound_404(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().Update(mock.Anything, recipeID.String(), mock.Anything, recipeUserID.String(), false).
		Return(nil, nil, models.ErrRecipeNotFound)

	w := doRecipeUpdate(t, m.router, recipeID.String(), validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", recipeErr(t, w))
}

func TestRecipeUpdate_Forbidden_403(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().Update(mock.Anything, recipeID.String(), mock.Anything, recipeUserID.String(), false).
		Return(nil, nil, models.ErrRecipeForbidden)

	w := doRecipeUpdate(t, m.router, recipeID.String(), validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "forbidden", recipeErr(t, w))
}

func TestRecipeUpdate_ApprovedLocked_403(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().Update(mock.Anything, recipeID.String(), mock.Anything, recipeUserID.String(), false).
		Return(nil, nil, models.ErrRecipeApprovedLocked)

	w := doRecipeUpdate(t, m.router, recipeID.String(), validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "recipe_approved_locked", recipeErr(t, w))
}

func TestRecipeUpdate_RegeneratesEachHoldersShoppingList(t *testing.T) {
	m := newRecipeMocks(t)
	first, second := uuid.NewString(), uuid.NewString()
	m.repo.EXPECT().Update(mock.Anything, recipeID.String(), mock.Anything, recipeUserID.String(), false).
		Return(fakeCreatedRecipe(), nil, nil)
	m.repo.EXPECT().MenuUserIDs(mock.Anything, recipeID.String()).Return([]string{first, second}, nil).Once()
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, first).Return(nil).Once()
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, second).Return(nil).Once()

	w := doRecipeUpdate(t, m.router, recipeID.String(), validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecipeUpdate_MenuUserIDsFails_500(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().Update(mock.Anything, recipeID.String(), mock.Anything, recipeUserID.String(), false).
		Return(fakeCreatedRecipe(), nil, nil)
	m.repo.EXPECT().MenuUserIDs(mock.Anything, recipeID.String()).Return(nil, errors.New("db down")).Once()

	w := doRecipeUpdate(t, m.router, recipeID.String(), validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", recipeErr(t, w))
}

func TestRecipeUpdate_RegenerateFails_500(t *testing.T) {
	m := newRecipeMocks(t)
	holder := uuid.NewString()
	m.repo.EXPECT().Update(mock.Anything, recipeID.String(), mock.Anything, recipeUserID.String(), false).
		Return(fakeCreatedRecipe(), nil, nil)
	m.repo.EXPECT().MenuUserIDs(mock.Anything, recipeID.String()).Return([]string{holder, uuid.NewString()}, nil).Once()
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, holder).Return(errors.New("db down")).Once()

	w := doRecipeUpdate(t, m.router, recipeID.String(), validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", recipeErr(t, w))
}

func TestRecipeUpdate_InvalidItemID_400(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().Update(mock.Anything, recipeID.String(), mock.Anything, recipeUserID.String(), false).
		Return(nil, nil, models.ErrRecipeInvalidItem)

	w := doRecipeUpdate(t, m.router, recipeID.String(), validRecipeBody(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_item_id", recipeErr(t, w))
}

func TestRecipeUpdate_Success_200_ReturnsDetail(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().Update(mock.Anything, recipeID.String(), mock.Anything, recipeUserID.String(), false).
		Return(fakeCreatedRecipe(), nil, nil)
	m.repo.EXPECT().MenuUserIDs(mock.Anything, recipeID.String()).Return(nil, nil).Maybe()

	w := doRecipeUpdate(t, m.router, recipeID.String(), validRecipeBody(), recipeAuth(t, "FREE"))

	require.Equal(t, http.StatusOK, w.Code)
	var got models.RecipeDetail
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, fakeCreatedRecipe().ID, got.ID)
}

func TestRecipeUpdate_PassesIDAndCallerToRepo(t *testing.T) {
	m := newRecipeMocks(t)
	var capturedID, capturedCaller string
	var capturedAdmin bool
	m.repo.EXPECT().Update(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, id string, _ models.CreateRecipeInput, callerID string, isAdmin bool) (*models.RecipeDetail, *string, error) {
			capturedID = id
			capturedCaller = callerID
			capturedAdmin = isAdmin
			return fakeCreatedRecipe(), nil, nil
		})
	m.repo.EXPECT().MenuUserIDs(mock.Anything, recipeID.String()).Return(nil, nil).Maybe()

	w := doRecipeUpdate(t, m.router, recipeID.String(), validRecipeBody(), recipeAuth(t, "ADMIN"))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, recipeID.String(), capturedID)
	assert.Equal(t, recipeUserID.String(), capturedCaller)
	assert.True(t, capturedAdmin)
}

func TestRecipeDelete_Unauthenticated_401(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeDelete(m.router, recipeID.String(), "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRecipeDelete_InvalidID_400(t *testing.T) {
	m := newRecipeMocks(t)
	w := doRecipeDelete(m.router, "not-a-uuid", recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRecipeDelete_NotFound_404(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, recipeID.String(), recipeUserID.String(), false).
		Return(nil, nil, models.ErrRecipeNotFound)

	w := doRecipeDelete(m.router, recipeID.String(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not_found", recipeErr(t, w))
}

func TestRecipeDelete_Forbidden_403(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, recipeID.String(), recipeUserID.String(), false).
		Return(nil, nil, models.ErrRecipeForbidden)

	w := doRecipeDelete(m.router, recipeID.String(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "forbidden", recipeErr(t, w))
}

func TestRecipeDelete_ApprovedLocked_403(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, recipeID.String(), recipeUserID.String(), false).
		Return(nil, nil, models.ErrRecipeApprovedLocked)

	w := doRecipeDelete(m.router, recipeID.String(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "recipe_approved_locked", recipeErr(t, w))
}

func TestRecipeDelete_Success_204(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().Delete(mock.Anything, recipeID.String(), recipeUserID.String(), false).
		Return(nil, nil, nil)

	w := doRecipeDelete(m.router, recipeID.String(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestRecipeDelete_RegeneratesEachAffectedUsersShoppingList(t *testing.T) {
	m := newRecipeMocks(t)
	first, second := uuid.NewString(), uuid.NewString()
	m.repo.EXPECT().Delete(mock.Anything, recipeID.String(), recipeUserID.String(), false).
		Return([]string{first, second}, nil, nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, first).Return(nil).Once()
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, second).Return(nil).Once()

	w := doRecipeDelete(m.router, recipeID.String(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestRecipeDelete_RegenerateFails_500(t *testing.T) {
	m := newRecipeMocks(t)
	affected := uuid.NewString()
	m.repo.EXPECT().Delete(mock.Anything, recipeID.String(), recipeUserID.String(), false).
		Return([]string{affected, uuid.NewString()}, nil, nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, affected).Return(errors.New("db down")).Once()

	w := doRecipeDelete(m.router, recipeID.String(), recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", recipeErr(t, w))
}
