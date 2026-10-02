package handler_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/franciskershaw/crockpot-go/internal/cloudinary"
	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func uploadedURL(publicID string) string {
	return "https://res.cloudinary.com/test/image/upload/v1/" + publicID + ".jpg"
}

func expectUploadSucceeds(m *recipeMocks, publicID string) {
	m.images.EXPECT().Upload(mock.Anything, mock.Anything, publicID).
		Return(cloudinary.UploadedImage{SecureURL: uploadedURL(publicID), PublicID: publicID}, nil).Once()
}

// expectDestroyAfterCommit fails the test if publicID is destroyed before the transaction committed.
func expectDestroyAfterCommit(t *testing.T, m *recipeMocks, publicID string, err error) {
	m.images.EXPECT().Destroy(mock.Anything, publicID).
		RunAndReturn(func(context.Context, string) error {
			assert.True(t, m.committed, "destroyed %s before commit", publicID)
			return err
		}).Once()
}

func TestRecipeCreate_Photo_UploadsToEnvFolderAndStoresImage(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	var uploaded []byte
	m.images.EXPECT().Upload(mock.Anything, mock.Anything, "dev/recipes/id-1").
		RunAndReturn(func(_ context.Context, r io.Reader, id string) (cloudinary.UploadedImage, error) {
			uploaded, _ = io.ReadAll(r)
			return cloudinary.UploadedImage{SecureURL: uploadedURL(id), PublicID: id}, nil
		}).Once()
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in models.CreateRecipeInput) (*models.RecipeDetail, error) {
			captured = in
			return fakeCreatedRecipe(), nil
		})

	w := doRecipeWrite(t, m.router, http.MethodPost, "/recipes", validRecipeBody(), jpegBytes, recipeAuth(t, "FREE"))

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, jpegBytes, uploaded)
	require.NotNil(t, captured.ImageURL)
	require.NotNil(t, captured.ImageFilename)
	assert.Equal(t, uploadedURL("dev/recipes/id-1"), *captured.ImageURL)
	assert.Equal(t, "dev/recipes/id-1", *captured.ImageFilename)
}

func TestRecipeCreate_Photo_UploadFails_502NothingSaved(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	m.images.EXPECT().Upload(mock.Anything, mock.Anything, mock.Anything).
		Return(cloudinary.UploadedImage{}, errors.New("cloudinary down")).Once()

	w := doRecipeWrite(t, m.router, http.MethodPost, "/recipes", validRecipeBody(), jpegBytes, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Equal(t, "image_upload_failed", recipeErr(t, w))
}

func TestRecipeCreate_Photo_SaveFails_DestroysNewUpload(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	expectUploadSucceeds(m, "dev/recipes/id-1")
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).Return(nil, models.ErrRecipeInvalidItem)
	m.repo.EXPECT().ImageInUse(mock.Anything, "dev/recipes/id-1").Return(false, nil).Once()
	m.images.EXPECT().Destroy(mock.Anything, "dev/recipes/id-1").Return(nil).Once()

	w := doRecipeWrite(t, m.router, http.MethodPost, "/recipes", validRecipeBody(), jpegBytes, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid_item_id", recipeErr(t, w))
}

func TestRecipeCreate_Photo_AtRecipeCap_RefusedBeforeUpload(t *testing.T) {
	m := newRecipeMocks(t)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(5, nil)

	w := doRecipeWrite(t, m.router, http.MethodPost, "/recipes", validRecipeBody(), jpegBytes, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "recipe_limit_reached", recipeErr(t, w))
}

func TestRecipeUpdate_ImageActionWithoutPhoto(t *testing.T) {
	cases := []struct {
		name        string
		removeImage bool
		want        models.ImageUpdate
	}{
		{"neither photo nor removeImage keeps the image", false, models.ImageKeep},
		{"removeImage removes it", true, models.ImageRemove},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)
			id := uuid.NewString()
			var captured models.CreateRecipeInput
			m.repo.EXPECT().Update(mock.Anything, id, mock.Anything, recipeUserID.String(), false).
				RunAndReturn(func(_ context.Context, _ string, in models.CreateRecipeInput, _ string, _ bool) (*models.RecipeDetail, *string, error) {
					captured = in
					return fakeCreatedRecipe(), nil, nil
				})
			m.repo.EXPECT().MenuUserIDs(mock.Anything, id).Return(nil, nil)
			body := validRecipeBody()
			if tc.removeImage {
				body["removeImage"] = true
			}

			w := doRecipeUpdate(t, m.router, id, body, recipeAuth(t, "FREE"))

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, tc.want, captured.Image)
		})
	}
}

func TestRecipeUpdate_Photo_ChecksPermissionThenUploadsAndReplaces(t *testing.T) {
	m := newRecipeMocks(t)
	id := uuid.NewString()
	checked := false
	m.repo.EXPECT().CheckWritable(mock.Anything, id, recipeUserID.String(), false).
		RunAndReturn(func(context.Context, string, string, bool) error {
			checked = true
			return nil
		}).Once()
	m.images.EXPECT().Upload(mock.Anything, mock.Anything, "dev/recipes/id-1").
		RunAndReturn(func(_ context.Context, _ io.Reader, pid string) (cloudinary.UploadedImage, error) {
			assert.True(t, checked, "uploaded before the permission check")
			return cloudinary.UploadedImage{SecureURL: uploadedURL(pid), PublicID: pid}, nil
		}).Once()
	var captured models.CreateRecipeInput
	m.repo.EXPECT().Update(mock.Anything, id, mock.Anything, recipeUserID.String(), false).
		RunAndReturn(func(_ context.Context, _ string, in models.CreateRecipeInput, _ string, _ bool) (*models.RecipeDetail, *string, error) {
			captured = in
			return fakeCreatedRecipe(), nil, nil
		})
	m.repo.EXPECT().MenuUserIDs(mock.Anything, id).Return(nil, nil)

	w := doRecipeWrite(t, m.router, http.MethodPatch, "/recipes/"+id, validRecipeBody(), jpegBytes, recipeAuth(t, "FREE"))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, models.ImageReplace, captured.Image)
	require.NotNil(t, captured.ImageFilename)
	assert.Equal(t, "dev/recipes/id-1", *captured.ImageFilename)
	require.NotNil(t, captured.ImageURL)
	assert.Equal(t, uploadedURL("dev/recipes/id-1"), *captured.ImageURL)
}

func TestRecipeUpdate_Photo_RefusedBeforeUpload(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{models.ErrRecipeForbidden, http.StatusForbidden, "forbidden"},
		{models.ErrRecipeNotFound, http.StatusNotFound, "not_found"},
		{models.ErrRecipeApprovedLocked, http.StatusForbidden, "recipe_approved_locked"},
	}
	for _, tc := range cases {
		t.Run(tc.wantCode, func(t *testing.T) {
			m := newRecipeMocks(t)
			id := uuid.NewString()
			m.repo.EXPECT().CheckWritable(mock.Anything, id, recipeUserID.String(), false).Return(tc.err).Once()

			w := doRecipeWrite(t, m.router, http.MethodPatch, "/recipes/"+id, validRecipeBody(), jpegBytes, recipeAuth(t, "FREE"))

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCode, recipeErr(t, w))
		})
	}
}

func TestRecipeUpdate_Photo_SaveFails_DestroysNewUpload(t *testing.T) {
	m := newRecipeMocks(t)
	id := uuid.NewString()
	m.repo.EXPECT().CheckWritable(mock.Anything, id, recipeUserID.String(), false).Return(nil).Once()
	expectUploadSucceeds(m, "dev/recipes/id-1")
	m.repo.EXPECT().Update(mock.Anything, id, mock.Anything, recipeUserID.String(), false).
		Return(nil, nil, models.ErrRecipeForbidden)
	m.repo.EXPECT().ImageInUse(mock.Anything, "dev/recipes/id-1").Return(false, nil).Once()
	m.images.EXPECT().Destroy(mock.Anything, "dev/recipes/id-1").Return(nil).Once()

	w := doRecipeWrite(t, m.router, http.MethodPatch, "/recipes/"+id, validRecipeBody(), jpegBytes, recipeAuth(t, "FREE"))

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRecipeUpdate_OrphanCleanupAfterCommit(t *testing.T) {
	cases := []struct {
		name        string
		orphan      *string
		wantDestroy bool
		destroyErr  error
	}{
		{"orphan in this environment's folder is destroyed", ptr("dev/recipes/old"), true, nil},
		{"destroy failure still succeeds", ptr("dev/recipes/old"), true, errors.New("cloudinary down")},
		{"orphan outside this environment's folders is kept", ptr("Crockpot/old"), false, nil},
		{"no orphan, nothing destroyed", nil, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)
			id := uuid.NewString()
			m.repo.EXPECT().Update(mock.Anything, id, mock.Anything, recipeUserID.String(), false).
				Return(fakeCreatedRecipe(), tc.orphan, nil)
			m.repo.EXPECT().MenuUserIDs(mock.Anything, id).Return(nil, nil)
			if tc.wantDestroy {
				expectDestroyAfterCommit(t, m, *tc.orphan, tc.destroyErr)
			}
			body := validRecipeBody()
			body["removeImage"] = true

			w := doRecipeUpdate(t, m.router, id, body, recipeAuth(t, "FREE"))

			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		})
	}
}

func TestRecipeDelete_OrphanCleanupAfterCommit(t *testing.T) {
	cases := []struct {
		name        string
		orphan      *string
		wantDestroy bool
		destroyErr  error
	}{
		{"orphan in this environment's folder is destroyed", ptr("dev/recipes/old"), true, nil},
		{"destroy failure still succeeds", ptr("dev/recipes/old"), true, errors.New("cloudinary down")},
		{"orphan outside this environment's folders is kept", ptr("recipes/old"), false, nil},
		{"no orphan, nothing destroyed", nil, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)
			id := uuid.NewString()
			m.repo.EXPECT().Delete(mock.Anything, id, recipeUserID.String(), false).Return(nil, tc.orphan, nil)
			if tc.wantDestroy {
				expectDestroyAfterCommit(t, m, *tc.orphan, tc.destroyErr)
			}

			w := doRecipeDelete(m.router, id, recipeAuth(t, "FREE"))

			assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
		})
	}
}

func TestRecipeWrite_PhotoLimitPerUser(t *testing.T) {
	m := newRecipeMocksWithPhotoLimit(t, 1)
	m.repo.EXPECT().CountByCreator(mock.Anything, mock.Anything).Return(0, nil)
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).Return(fakeCreatedRecipe(), nil)
	expectUploadSucceeds(m, "dev/recipes/id-1")
	expectUploadSucceeds(m, "dev/recipes/id-2")
	other := testutil.AuthHeader(t, "other@example.com", uuid.NewString(), "FREE")

	first := doRecipeWrite(t, m.router, http.MethodPost, "/recipes", validRecipeBody(), jpegBytes, recipeAuth(t, "FREE"))
	second := doRecipeWrite(t, m.router, http.MethodPost, "/recipes", validRecipeBody(), jpegBytes, recipeAuth(t, "FREE"))
	otherUser := doRecipeWrite(t, m.router, http.MethodPost, "/recipes", validRecipeBody(), jpegBytes, other)

	assert.Equal(t, http.StatusCreated, first.Code)
	assert.Equal(t, http.StatusTooManyRequests, second.Code)
	assert.Equal(t, "rate_limit_exceeded", recipeErr(t, second))
	retry, err := strconv.Atoi(second.Header().Get("Retry-After"))
	assert.NoError(t, err)
	assert.Positive(t, retry)
	assert.Equal(t, http.StatusCreated, otherUser.Code, "the limit is per user")
}

func TestRecipeWrite_PhotoLimitIgnoresSavesWithoutPhoto(t *testing.T) {
	m := newRecipeMocksWithPhotoLimit(t, 1)
	m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
	m.repo.EXPECT().Create(mock.Anything, mock.Anything).Return(fakeCreatedRecipe(), nil)
	expectUploadSucceeds(m, "dev/recipes/id-1")

	for range 3 {
		w := doRecipeCreate(t, m.router, validRecipeBody(), recipeAuth(t, "FREE"))
		require.Equal(t, http.StatusCreated, w.Code)
	}
	w := doRecipeWrite(t, m.router, http.MethodPost, "/recipes", validRecipeBody(), jpegBytes, recipeAuth(t, "FREE"))
	assert.Equal(t, http.StatusCreated, w.Code, "saves without a photo don't use up the photo limit")
}

// A commit can report an error after it actually succeeded; the new upload is then only destroyed when no recipe points at it.
func TestRecipeWrite_SaveFails_KeepsUploadThatARecipeUses(t *testing.T) {
	cases := []struct {
		name     string
		inUse    bool
		inUseErr error
		isUpdate bool
	}{
		{"create: a recipe uses it", true, nil, false},
		{"create: can't tell, keep it", false, errors.New("db down"), false},
		{"update: a recipe uses it", true, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRecipeMocks(t)
			commitErr := errors.New("conn closed during commit")
			path := "/recipes"
			method := http.MethodPost
			if tc.isUpdate {
				id := uuid.NewString()
				path, method = "/recipes/"+id, http.MethodPatch
				m.repo.EXPECT().CheckWritable(mock.Anything, id, recipeUserID.String(), false).Return(nil).Once()
				m.repo.EXPECT().Update(mock.Anything, id, mock.Anything, recipeUserID.String(), false).Return(nil, nil, commitErr)
			} else {
				m.repo.EXPECT().CountByCreator(mock.Anything, recipeUserID.String()).Return(0, nil)
				m.repo.EXPECT().Create(mock.Anything, mock.Anything).Return(nil, commitErr)
			}
			expectUploadSucceeds(m, "dev/recipes/id-1")
			m.repo.EXPECT().ImageInUse(mock.Anything, "dev/recipes/id-1").Return(tc.inUse, tc.inUseErr).Once()

			w := doRecipeWrite(t, m.router, method, path, validRecipeBody(), jpegBytes, recipeAuth(t, "FREE"))

			assert.Equal(t, http.StatusInternalServerError, w.Code)
		})
	}
}

func TestRecipeWrite_OversizeChunkedBody_413(t *testing.T) {
	m := newRecipeMocks(t)
	r := gin.New()
	r.Use(middleware.BodySizeLimit(1024, nil), middleware.AuthMiddleware(testutil.TestAccessSecret))
	r.POST("/recipes", m.handler.Create)

	buf, contentType := recipeMultipart(t, validRecipeBody(), append(append([]byte{}, jpegBytes...), make([]byte, 4096)...))
	req := httptest.NewRequest(http.MethodPost, "/recipes", buf)
	req.ContentLength = -1
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", recipeAuth(t, "FREE"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Equal(t, "request_too_large", recipeErr(t, w))
}
