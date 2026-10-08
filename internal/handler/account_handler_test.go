package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
)

type accountMocks struct {
	users         *genmocks.MockAccountUserRepository
	recipes       *genmocks.MockAccountRecipeRepository
	shoppingLists *genmocks.MockShoppingListRegenerator
	images        *genmocks.MockImageStore
	router        *gin.Engine
}

func newAccountMocks(t *testing.T) *accountMocks {
	m := &accountMocks{
		users:         genmocks.NewMockAccountUserRepository(t),
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
	h := handler.NewAccountHandler(m.users, m.recipes, m.shoppingLists, transactor, m.images,
		handler.ImageScope{UploadFolder: "dev/recipes"}, &config.Config{Environment: config.EnvDevelopment})
	m.router = gin.New()
	authed := m.router.Group("/")
	authed.Use(middleware.AuthMiddleware(testutil.TestAccessSecret))
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
