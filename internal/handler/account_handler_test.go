package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/config"
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
	"golang.org/x/crypto/bcrypt"
)

type accountMocks struct {
	users         *genmocks.MockAccountUserRepository
	refreshTokens *genmocks.MockRefreshTokenRepository
	emailSender   *genmocks.MockEmailSender
	recipes       *genmocks.MockAccountRecipeRepository
	shoppingLists *genmocks.MockShoppingListRegenerator
	images        *genmocks.MockImageStore
	router        *gin.Engine
}

func newAccountMocks(t *testing.T) *accountMocks {
	m := &accountMocks{
		users:         genmocks.NewMockAccountUserRepository(t),
		refreshTokens: genmocks.NewMockRefreshTokenRepository(t),
		emailSender:   genmocks.NewMockEmailSender(t),
		recipes:       genmocks.NewMockAccountRecipeRepository(t),
		shoppingLists: genmocks.NewMockShoppingListRegenerator(t),
		images:        genmocks.NewMockImageStore(t),
	}
	transactor := genmocks.NewMockTransactor(t)
	transactor.EXPECT().WithinTx(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(context.WithValue(ctx, inTxKey{}, true))
		}).
		Maybe()
	h := handler.NewAccountHandler(m.users, m.refreshTokens, m.emailSender, m.recipes, m.shoppingLists, transactor, m.images,
		handler.ImageScope{UploadFolder: "dev/recipes"}, &config.Config{
			Environment:      config.EnvDevelopment,
			JWTSecretAccess:  testutil.TestAccessSecret,
			JWTSecretRefresh: testutil.TestRefreshSecret,
			FrontendURL:      "http://localhost:5173",
		})
	m.router = gin.New()
	authed := m.router.Group("/")
	authed.Use(middleware.AuthMiddleware(testutil.TestAccessSecret))
	authed.GET("/me", h.Me)
	authed.PATCH("/me", h.UpdateMe)
	authed.POST("/me/password", h.ChangePassword)
	authed.DELETE("/me", h.DeleteMe)
	return m
}

var (
	deletingUserID = uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	deletingUser   = &models.User{ID: deletingUserID, Email: "leaving@example.com", PasswordHash: ptr(loginUserPasswordHash), Role: "FREE"}
	deletingGoogle = &models.User{ID: deletingUserID, Email: "leaving@example.com", GoogleID: ptr("google-leaving"), Role: "FREE"}
	deletingAdmin  = &models.User{ID: deletingUserID, Email: "leaving@example.com", PasswordHash: ptr(loginUserPasswordHash), Role: "ADMIN"}
	menuHolderID   = uuid.MustParse("eeeeeeee-0000-0000-0000-000000000001").String()
)

// doDeleteMe sends no body when body is nil, as a Google account's client would.
func doDeleteMe(t *testing.T, r *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/me", nil)
	if body != nil {
		req = httptest.NewRequest(http.MethodDelete, "/me", jsonRequestBody(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", testutil.AuthHeader(t, deletingUser.Email, deletingUserID.String(), "FREE"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func assertRefreshCookieCleared(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	cookie := refreshCookieFrom(w)
	require.NotNil(t, cookie, "expected the refresh cookie to be cleared")
	assert.Empty(t, cookie.Value)
	assert.Less(t, cookie.MaxAge, 0)
}

func TestDeleteMe_PasswordAccountDeletesAndCleansUp(t *testing.T) {
	m := newAccountMocks(t)
	userID := deletingUserID.String()
	var order []string
	var deleteInTx bool

	m.users.EXPECT().FindByID(mock.Anything, userID).
		Run(func(context.Context, string) { order = append(order, "FindByID") }).
		Return(deletingUser, nil)
	m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).
		Run(func(context.Context, string) { order = append(order, "FindByIDForUpdate") }).
		Return(deletingUser, nil)
	m.recipes.EXPECT().DeleteUnapprovedByCreator(mock.Anything, userID).
		Run(func(context.Context, string) { order = append(order, "DeleteUnapprovedByCreator") }).
		Return([]string{menuHolderID, userID}, []string{"dev/recipes/draft-photo", "legacy/other-env-photo"}, nil)
	m.shoppingLists.EXPECT().Regenerate(mock.Anything, menuHolderID).
		Run(func(context.Context, string) { order = append(order, "Regenerate") }).
		Return(nil)
	m.users.EXPECT().Delete(mock.Anything, userID).
		Run(func(ctx context.Context, _ string) {
			order = append(order, "Delete")
			deleteInTx = inTx(ctx)
		}).
		Return(nil)
	m.images.EXPECT().Destroy(mock.Anything, "dev/recipes/draft-photo").
		Run(func(context.Context, string) { order = append(order, "Destroy") }).
		Return(nil)

	w := doDeleteMe(t, m.router, map[string]string{"password": loginUserPassword})

	assert.Equal(t, http.StatusNoContent, w.Code)
	assertRefreshCookieCleared(t, w)
	assert.Equal(t, []string{"FindByID", "FindByIDForUpdate", "DeleteUnapprovedByCreator", "Regenerate", "Delete", "Destroy"}, order,
		"the leaving user's own list isn't rebuilt (it cascades), and photos are destroyed only after the writes")
	assert.True(t, deleteInTx)
}

func TestDeleteMe_GoogleAccountNeedsNoBody(t *testing.T) {
	m := newAccountMocks(t)
	userID := deletingUserID.String()
	m.users.EXPECT().FindByID(mock.Anything, userID).Return(deletingGoogle, nil)
	m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).Return(deletingGoogle, nil)
	m.recipes.EXPECT().DeleteUnapprovedByCreator(mock.Anything, userID).Return(nil, nil, nil)
	m.users.EXPECT().Delete(mock.Anything, userID).Return(nil)

	w := doDeleteMe(t, m.router, nil)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assertRefreshCookieCleared(t, w)
}

// An empty body of unknown length (chunked, or HTTP/2 without END_STREAM on the headers) is still no body.
func TestDeleteMe_GoogleAccountEmptyBodyOfUnknownLength(t *testing.T) {
	m := newAccountMocks(t)
	userID := deletingUserID.String()
	m.users.EXPECT().FindByID(mock.Anything, userID).Return(deletingGoogle, nil).Maybe()
	m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).Return(deletingGoogle, nil).Maybe()
	m.recipes.EXPECT().DeleteUnapprovedByCreator(mock.Anything, userID).Return(nil, nil, nil).Maybe()
	m.users.EXPECT().Delete(mock.Anything, userID).Return(nil).Maybe()

	req := httptest.NewRequest(http.MethodDelete, "/me", strings.NewReader(""))
	req.ContentLength = -1
	req.Header.Set("Authorization", testutil.AuthHeader(t, deletingUser.Email, userID, "FREE"))
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteMe_PhotoDestroyFailureStillSucceeds(t *testing.T) {
	m := newAccountMocks(t)
	userID := deletingUserID.String()
	m.users.EXPECT().FindByID(mock.Anything, userID).Return(deletingUser, nil)
	m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).Return(deletingUser, nil)
	m.recipes.EXPECT().DeleteUnapprovedByCreator(mock.Anything, userID).Return(nil, []string{"dev/recipes/draft-photo"}, nil)
	m.users.EXPECT().Delete(mock.Anything, userID).Return(nil)
	m.images.EXPECT().Destroy(mock.Anything, "dev/recipes/draft-photo").Return(errors.New("cloudinary down"))

	w := doDeleteMe(t, m.router, map[string]string{"password": loginUserPassword})

	assert.Equal(t, http.StatusNoContent, w.Code)
	assertRefreshCookieCleared(t, w)
}

func TestDeleteMe_Fails(t *testing.T) {
	userID := deletingUserID.String()
	changedMeanwhile := &models.User{ID: deletingUserID, Email: deletingUser.Email, PasswordHash: ptr(mustHash("someone-elses-new-password")), Role: "FREE"}
	// rejectedBeforeLock serves the unlocked read and records whether the locking read was ever reached.
	rejectedBeforeLock := func(u *models.User, err error) func(m *accountMocks, locked *bool) {
		return func(m *accountMocks, locked *bool) {
			m.users.EXPECT().FindByID(mock.Anything, userID).Return(u, err).Maybe()
			m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).
				Run(func(context.Context, string) { *locked = true }).
				Return(u, err).Maybe()
		}
	}

	cases := []struct {
		name       string
		body       any
		setup      func(m *accountMocks, locked *bool)
		wantCode   int
		wantError  string
		wantNoLock bool
	}{
		{name: "malformed body", body: "{not json", wantCode: http.StatusBadRequest, wantError: "invalid_request"},
		{
			name: "wrong password", body: map[string]string{"password": "not-the-password"},
			setup: rejectedBeforeLock(deletingUser, nil), wantCode: http.StatusForbidden, wantError: "invalid_password", wantNoLock: true,
		},
		{
			name: "password account sends no password", body: nil,
			setup: rejectedBeforeLock(deletingUser, nil), wantCode: http.StatusForbidden, wantError: "invalid_password", wantNoLock: true,
		},
		{
			name: "admin", body: map[string]string{"password": loginUserPassword},
			setup: rejectedBeforeLock(deletingAdmin, nil), wantCode: http.StatusForbidden, wantError: "admin_cannot_self_delete", wantNoLock: true,
		},
		{
			name: "user no longer exists", body: map[string]string{"password": loginUserPassword},
			setup: rejectedBeforeLock(nil, models.ErrUserNotFound), wantCode: http.StatusUnauthorized, wantError: "unauthorized", wantNoLock: true,
		},
		{
			name: "password changed between the check and the lock", body: map[string]string{"password": loginUserPassword},
			setup: func(m *accountMocks, _ *bool) {
				m.users.EXPECT().FindByID(mock.Anything, userID).Return(deletingUser, nil).Maybe()
				m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).Return(changedMeanwhile, nil)
			},
			wantCode: http.StatusForbidden, wantError: "invalid_password",
		},
		{
			name: "promoted to admin between the check and the lock", body: map[string]string{"password": loginUserPassword},
			setup: func(m *accountMocks, _ *bool) {
				m.users.EXPECT().FindByID(mock.Anything, userID).Return(deletingUser, nil).Maybe()
				m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).Return(deletingAdmin, nil)
			},
			wantCode: http.StatusForbidden, wantError: "admin_cannot_self_delete",
		},
		{
			name: "user delete fails", body: map[string]string{"password": loginUserPassword},
			setup: func(m *accountMocks, _ *bool) {
				m.users.EXPECT().FindByID(mock.Anything, userID).Return(deletingUser, nil).Maybe()
				m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).Return(deletingUser, nil)
				m.recipes.EXPECT().DeleteUnapprovedByCreator(mock.Anything, userID).Return(nil, []string{"dev/recipes/draft-photo"}, nil)
				m.users.EXPECT().Delete(mock.Anything, userID).Return(errors.New("db exploded"))
			},
			wantCode: http.StatusInternalServerError, wantError: "server_error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newAccountMocks(t)
			var locked bool
			if tc.setup != nil {
				tc.setup(m, &locked)
			}

			w := doDeleteMe(t, m.router, tc.body)

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantError, decodeJSONBody(t, w)["error"])
			assert.Nil(t, refreshCookieFrom(w), "a failed delete must leave the session alone")
			if tc.wantNoLock {
				assert.False(t, locked, "rejected before taking the user row lock")
			}
		})
	}
}

// --- Me ---

var (
	meTestUserID = uuid.MustParse("55555555-5555-5555-5555-555555555555")
	meTestName   = "Me Test User"
	meTestUser   = &models.User{
		ID:           meTestUserID,
		Email:        "me@example.com",
		Name:         &meTestName,
		Role:         "FREE",
		PasswordHash: ptr("bcrypt-hash-placeholder"),
	}
	meTestGoogleUser = &models.User{
		ID:       meTestUserID,
		Email:    "me@example.com",
		Name:     &meTestName,
		Role:     "FREE",
		GoogleID: ptr("google-me"),
	}
)

func doMe(r *gin.Engine, authHeaderValue string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	if authHeaderValue != "" {
		req.Header.Set("Authorization", authHeaderValue)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMe_ReturnsProfile(t *testing.T) {
	m := newAccountMocks(t)
	m.users.EXPECT().FindByID(mock.Anything, meTestUserID.String()).Return(meTestUser, nil)

	w := doMe(m.router, testutil.AuthHeader(t, meTestUser.Email, meTestUserID.String(), meTestUser.Role))

	assert.Equal(t, http.StatusOK, w.Code)
	body := decodeJSONBodyAny(t, w)
	assert.Equal(t, meTestUserID.String(), body["id"])
	assert.Equal(t, meTestUser.Email, body["email"])
	assert.Equal(t, meTestName, body["name"])
	assert.NotContains(t, body, "image")
	assert.Equal(t, meTestUser.Role, body["role"])
	assert.Equal(t, "password", body["authProvider"])
}

func TestMe_ReportsGoogleAuthProvider(t *testing.T) {
	m := newAccountMocks(t)
	m.users.EXPECT().FindByID(mock.Anything, meTestUserID.String()).Return(meTestGoogleUser, nil)

	w := doMe(m.router, testutil.AuthHeader(t, meTestUser.Email, meTestUserID.String(), meTestUser.Role))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "google", decodeJSONBodyAny(t, w)["authProvider"])
}

func TestMe_ReturnsUnauthorizedWhenUserNotFound(t *testing.T) {
	m := newAccountMocks(t)
	m.users.EXPECT().FindByID(mock.Anything, meTestUserID.String()).Return(nil, models.ErrUserNotFound)

	w := doMe(m.router, testutil.AuthHeader(t, meTestUser.Email, meTestUserID.String(), meTestUser.Role))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "unauthorized", decodeJSONBody(t, w)["error"])
}

func TestMe_ReturnsServerErrorOnOtherRepositoryError(t *testing.T) {
	m := newAccountMocks(t)
	m.users.EXPECT().FindByID(mock.Anything, meTestUserID.String()).Return(nil, errors.New("db exploded"))

	w := doMe(m.router, testutil.AuthHeader(t, meTestUser.Email, meTestUserID.String(), meTestUser.Role))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", decodeJSONBody(t, w)["error"])
}

// Defensive branch: unreachable through the wired router (AuthMiddleware always sets userID
// before Me runs), exercised by calling the handler directly against a bare context instead.
func TestMe_ReturnsUnauthorizedWhenUserIDMissingFromContext(t *testing.T) {
	m := newAccountMocks(t)
	h := handler.NewAccountHandler(m.users, m.refreshTokens, m.emailSender, m.recipes, m.shoppingLists, genmocks.NewMockTransactor(t), m.images, handler.ImageScope{}, &config.Config{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/me", nil)

	h.Me(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "unauthorized", decodeJSONBody(t, w)["error"])
}

// --- UpdateMe ---

func doUpdateMe(t *testing.T, r *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/me", jsonRequestBody(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", testutil.AuthHeader(t, meTestUser.Email, meTestUserID.String(), meTestUser.Role))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestUpdateMe_SavesTrimmedNameAndReturnsProfile(t *testing.T) {
	m := newAccountMocks(t)
	renamed := *meTestGoogleUser
	renamed.Name = ptr("New Name")
	m.users.EXPECT().UpdateName(mock.Anything, meTestUserID.String(), "New Name").Return(&renamed, nil)

	w := doUpdateMe(t, m.router, map[string]string{"name": "  New Name  "})

	assert.Equal(t, http.StatusOK, w.Code)
	body := decodeJSONBodyAny(t, w)
	assert.Equal(t, meTestUserID.String(), body["id"])
	assert.Equal(t, "New Name", body["name"])
	assert.Equal(t, "google", body["authProvider"])
}

func TestUpdateMe_Fails(t *testing.T) {
	cases := []struct {
		name      string
		body      any
		setup     func(userRepo *genmocks.MockAccountUserRepository)
		wantCode  int
		wantError string
	}{
		{name: "malformed body", body: "{not json", wantCode: http.StatusBadRequest, wantError: "invalid_request"},
		{name: "name missing", body: map[string]string{}, wantCode: http.StatusBadRequest, wantError: "invalid_name"},
		{name: "empty name", body: map[string]string{"name": ""}, wantCode: http.StatusBadRequest, wantError: "invalid_name"},
		{name: "whitespace-only name", body: map[string]string{"name": "   "}, wantCode: http.StatusBadRequest, wantError: "invalid_name"},
		{name: "51-character name", body: map[string]string{"name": strings.Repeat("a", 51)}, wantCode: http.StatusBadRequest, wantError: "invalid_name"},
		{
			name: "user no longer exists",
			body: map[string]string{"name": "New Name"},
			setup: func(userRepo *genmocks.MockAccountUserRepository) {
				userRepo.EXPECT().UpdateName(mock.Anything, meTestUserID.String(), "New Name").Return(nil, models.ErrUserNotFound)
			},
			wantCode:  http.StatusUnauthorized,
			wantError: "unauthorized",
		},
		{
			name: "repository error",
			body: map[string]string{"name": "New Name"},
			setup: func(userRepo *genmocks.MockAccountUserRepository) {
				userRepo.EXPECT().UpdateName(mock.Anything, meTestUserID.String(), "New Name").Return(nil, errors.New("db exploded"))
			},
			wantCode:  http.StatusInternalServerError,
			wantError: "server_error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newAccountMocks(t)
			if tc.setup != nil {
				tc.setup(m.users)
			}

			w := doUpdateMe(t, m.router, tc.body)

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantError, decodeJSONBody(t, w)["error"])
		})
	}
}

// --- ChangePassword ---

const changedPasswordPlaintext = "brand-new-horse"

var changePasswordUser = &models.User{
	ID: uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"), Email: "change@example.com",
	Name: ptr("Change Me"), PasswordHash: ptr(loginUserPasswordHash), Role: "FREE",
}

func doChangePassword(t *testing.T, r *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/me/password", jsonRequestBody(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", testutil.AuthHeader(t, changePasswordUser.Email, changePasswordUser.ID.String(), changePasswordUser.Role))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// expectPasswordChangeWrites records the order of the transaction's writes, the hash it saved, and whether the write ran inside the transaction.
func expectPasswordChangeWrites(m *accountMocks, order *[]string, savedHash *string, writeInTx *bool) {
	userID := changePasswordUser.ID.String()
	m.users.EXPECT().UpdatePassword(mock.Anything, userID, mock.AnythingOfType("string")).
		Run(func(ctx context.Context, _ string, hash string) {
			*order = append(*order, "UpdatePassword")
			*savedHash = hash
			*writeInTx = inTx(ctx)
		}).
		Return(changePasswordUser, nil)
	m.refreshTokens.EXPECT().RevokeAllFamiliesForUser(mock.Anything, userID).
		Run(func(context.Context, string) { *order = append(*order, "RevokeAllFamiliesForUser") }).
		Return(nil)
	m.refreshTokens.EXPECT().DeleteStaleFamiliesForUser(mock.Anything, userID).
		Run(func(context.Context, string) { *order = append(*order, "DeleteStaleFamiliesForUser") }).
		Return(nil)
	m.refreshTokens.EXPECT().CreateFamily(mock.Anything, mock.AnythingOfType("string"), userID, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).
		Run(func(context.Context, string, string, string, time.Time) { *order = append(*order, "CreateFamily") }).
		Return(&models.RefreshTokenFamily{ID: uuid.New(), UserID: changePasswordUser.ID}, nil)
}

func TestChangePassword_Success(t *testing.T) {
	m := newAccountMocks(t)
	var order []string
	var savedHash string
	var lookupInTx, writeInTx bool
	m.users.EXPECT().FindByID(mock.Anything, changePasswordUser.ID.String()).Return(changePasswordUser, nil)
	m.users.EXPECT().FindByIDForUpdate(mock.Anything, changePasswordUser.ID.String()).
		Run(func(ctx context.Context, _ string) { lookupInTx = inTx(ctx) }).
		Return(changePasswordUser, nil)
	expectPasswordChangeWrites(m, &order, &savedHash, &writeInTx)
	m.emailSender.EXPECT().SendPasswordChanged(mock.Anything, changePasswordUser.Email, "http://localhost:5173/forgot-password").
		Run(func(context.Context, string, string) { order = append(order, "SendPasswordChanged") }).
		Return(nil)

	w := doChangePassword(t, m.router, map[string]string{"currentPassword": loginUserPassword, "newPassword": changedPasswordPlaintext})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, decodeJSONBody(t, w)["accessToken"])
	require.NotNil(t, refreshCookieFrom(w), "expected refreshToken cookie to be set")
	assert.Equal(t, []string{"UpdatePassword", "RevokeAllFamiliesForUser", "DeleteStaleFamiliesForUser", "CreateFamily", "SendPasswordChanged"}, order)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(savedHash), []byte(changedPasswordPlaintext)), "saved hash should match the new password")
	assert.True(t, lookupInTx, "the current-password check must read the user inside the transaction that writes")
	assert.True(t, writeInTx)
}

func TestChangePassword_EmailFailureStillSucceeds(t *testing.T) {
	m := newAccountMocks(t)
	var order []string
	var savedHash string
	var writeInTx bool
	m.users.EXPECT().FindByID(mock.Anything, changePasswordUser.ID.String()).Return(changePasswordUser, nil)
	m.users.EXPECT().FindByIDForUpdate(mock.Anything, changePasswordUser.ID.String()).Return(changePasswordUser, nil)
	expectPasswordChangeWrites(m, &order, &savedHash, &writeInTx)
	m.emailSender.EXPECT().SendPasswordChanged(mock.Anything, changePasswordUser.Email, mock.AnythingOfType("string")).Return(errors.New("resend unreachable"))

	w := doChangePassword(t, m.router, map[string]string{"currentPassword": loginUserPassword, "newPassword": changedPasswordPlaintext})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, decodeJSONBody(t, w)["accessToken"])
	assert.NotNil(t, refreshCookieFrom(w))
}

func TestChangePassword_Fails(t *testing.T) {
	userID := changePasswordUser.ID.String()
	googleUser := &models.User{ID: changePasswordUser.ID, Email: changePasswordUser.Email, GoogleID: ptr("google-change"), Role: "FREE"}
	changedMeanwhile := &models.User{ID: changePasswordUser.ID, Email: changePasswordUser.Email, PasswordHash: ptr(mustHash("someone-elses-new-password")), Role: "FREE"}
	// rejectedBeforeLock serves the unlocked read and records whether the locking read was ever reached.
	rejectedBeforeLock := func(u *models.User) func(m *accountMocks, locked *bool) {
		return func(m *accountMocks, locked *bool) {
			m.users.EXPECT().FindByID(mock.Anything, userID).Return(u, nil).Maybe()
			m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).
				Run(func(context.Context, string) { *locked = true }).
				Return(u, nil).Maybe()
		}
	}
	valid := map[string]string{"currentPassword": loginUserPassword, "newPassword": changedPasswordPlaintext}

	cases := []struct {
		name       string
		body       any
		setup      func(m *accountMocks, locked *bool)
		wantCode   int
		wantError  string
		wantNoLock bool
	}{
		{name: "malformed body", body: "{not json", wantCode: http.StatusBadRequest, wantError: "invalid_request"},
		{name: "current password missing", body: map[string]string{"newPassword": changedPasswordPlaintext}, wantCode: http.StatusBadRequest, wantError: "invalid_request"},
		{name: "new password missing", body: map[string]string{"currentPassword": loginUserPassword}, wantCode: http.StatusBadRequest, wantError: "invalid_request"},
		{
			name: "new password too short", body: map[string]string{"currentPassword": loginUserPassword, "newPassword": "short"},
			setup: rejectedBeforeLock(changePasswordUser), wantCode: http.StatusBadRequest, wantError: "password_too_short", wantNoLock: true,
		},
		{
			name: "new password too long", body: map[string]string{"currentPassword": loginUserPassword, "newPassword": strings.Repeat("a", 73)},
			setup: rejectedBeforeLock(changePasswordUser), wantCode: http.StatusBadRequest, wantError: "password_too_long", wantNoLock: true,
		},
		{
			name: "wrong current password", body: map[string]string{"currentPassword": "not-the-password", "newPassword": changedPasswordPlaintext},
			setup: rejectedBeforeLock(changePasswordUser), wantCode: http.StatusForbidden, wantError: "invalid_password", wantNoLock: true,
		},
		{name: "google account", body: valid, setup: rejectedBeforeLock(googleUser), wantCode: http.StatusConflict, wantError: "no_password", wantNoLock: true},
		{
			name: "user no longer exists", body: valid,
			setup: func(m *accountMocks, locked *bool) {
				m.users.EXPECT().FindByID(mock.Anything, userID).Return(nil, models.ErrUserNotFound).Maybe()
				m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).
					Run(func(context.Context, string) { *locked = true }).
					Return(nil, models.ErrUserNotFound).Maybe()
			},
			wantCode: http.StatusUnauthorized, wantError: "unauthorized", wantNoLock: true,
		},
		{
			name: "password changed between the check and the lock", body: valid,
			setup: func(m *accountMocks, _ *bool) {
				m.users.EXPECT().FindByID(mock.Anything, userID).Return(changePasswordUser, nil).Maybe()
				m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).Return(changedMeanwhile, nil)
			},
			wantCode: http.StatusForbidden, wantError: "invalid_password",
		},
		{
			name: "write fails", body: valid,
			setup: func(m *accountMocks, _ *bool) {
				m.users.EXPECT().FindByID(mock.Anything, userID).Return(changePasswordUser, nil).Maybe()
				m.users.EXPECT().FindByIDForUpdate(mock.Anything, userID).Return(changePasswordUser, nil)
				m.users.EXPECT().UpdatePassword(mock.Anything, userID, mock.AnythingOfType("string")).Return(nil, errors.New("db exploded"))
			},
			wantCode: http.StatusInternalServerError, wantError: "server_error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newAccountMocks(t)
			var locked bool
			if tc.setup != nil {
				tc.setup(m, &locked)
			}

			w := doChangePassword(t, m.router, tc.body)

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantError, decodeJSONBody(t, w)["error"])
			assert.Nil(t, refreshCookieFrom(w), "a failed change must not set a refresh cookie")
			if tc.wantNoLock {
				assert.False(t, locked, "rejected before taking the user row lock")
			}
		})
	}
}
