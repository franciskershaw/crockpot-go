package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/config"
	"github.com/franciskershaw/crockpot-go/internal/auth"
	"github.com/franciskershaw/crockpot-go/internal/handler"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// --- Mocks ---

type MockUserRepository struct {
	mock.Mock
}

func (m *MockUserRepository) GetOrCreateUser(ctx context.Context, email, googleID, displayName, avatarURL string) (*models.User, error) {
	args := m.Called(ctx, email, googleID, displayName, avatarURL)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

type MockOAuthManager struct {
	mock.Mock
}

func (m *MockOAuthManager) GenerateState() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockOAuthManager) ValidateState(state string) bool {
	args := m.Called(state)
	return args.Bool(0)
}

func (m *MockOAuthManager) GetAuthURL(state string) string {
	args := m.Called(state)
	return args.String(0)
}

func (m *MockOAuthManager) ExchangeCodeForToken(ctx context.Context, code string) (*oauth2.Token, error) {
	args := m.Called(ctx, code)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*oauth2.Token), args.Error(1)
}

func (m *MockOAuthManager) VerifyIDToken(ctx context.Context, token *oauth2.Token) (*auth.IDTokenClaims, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*auth.IDTokenClaims), args.Error(1)
}

type MockRefreshTokenRepository struct {
	mock.Mock
}

func (m *MockRefreshTokenRepository) CreateFamily(ctx context.Context, id, userID, tokenHash string, expiresAt time.Time) (*models.RefreshTokenFamily, error) {
	args := m.Called(ctx, id, userID, tokenHash, expiresAt)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.RefreshTokenFamily), args.Error(1)
}

func (m *MockRefreshTokenRepository) DeleteStaleFamiliesForUser(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// --- Shared fixtures ---

var (
	fakeToken = &oauth2.Token{}
	fakeUser  = &models.User{
		ID:       uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Email:    "test@example.com",
		Name:     ptr("Test User"),
		Image:    ptr("https://example.com/avatar.png"),
		GoogleID: ptr("google-123"),
		Role:     "FREE",
	}
	fakeClaims = &auth.IDTokenClaims{
		Email:         fakeUser.Email,
		EmailVerified: true,
		GoogleID:      *fakeUser.GoogleID,
		DisplayName:   *fakeUser.Name,
		AvatarURL:     *fakeUser.Image,
	}
)

func ptr[T any](v T) *T { return &v }

// --- Helpers ---

// mocks bundles the three collaborator mocks plus a router wired to a
// handler built from them, so each test only names what it needs.
type mocks struct {
	userRepo         *MockUserRepository
	oauthMgr         *MockOAuthManager
	refreshTokenRepo *MockRefreshTokenRepository
	router           *gin.Engine
}

func newMocks(env config.Environment) *mocks {
	m := &mocks{
		userRepo:         &MockUserRepository{},
		oauthMgr:         &MockOAuthManager{},
		refreshTokenRepo: &MockRefreshTokenRepository{},
	}
	h := handler.NewAuthHandler(m.userRepo, m.oauthMgr, m.refreshTokenRepo, &config.Config{
		Environment:         env,
		JWTSecretRefresh:    testutil.TestRefreshSecret,
		JWTSecretOAuthState: testutil.TestOAuthStateSecret,
		FrontendURL:         "http://localhost:5173",
	})
	m.router = gin.New()
	m.router.GET("/auth/google/login", h.LoginWithGoogle)
	m.router.GET("/auth/google/callback", h.GoogleCallback)
	return m
}

func (m *mocks) assertExpectations(t *testing.T) {
	t.Helper()
	m.userRepo.AssertExpectations(t)
	m.oauthMgr.AssertExpectations(t)
	m.refreshTokenRepo.AssertExpectations(t)
}

// mockSuccessfulExchange wires the state-valid/exchange/verify chain to succeed.
func mockSuccessfulExchange(oauthMgr *MockOAuthManager) {
	oauthMgr.On("ValidateState", "valid-state").Return(true)
	oauthMgr.On("ExchangeCodeForToken", mock.Anything, "auth-code").Return(fakeToken, nil)
	oauthMgr.On("VerifyIDToken", mock.Anything, fakeToken).Return(fakeClaims, nil)
}

// mockSuccessfulUserAndFamily wires GetOrCreateUser/DeleteStaleFamiliesForUser/CreateFamily to all succeed.
func mockSuccessfulUserAndFamily(userRepo *MockUserRepository, refreshTokenRepo *MockRefreshTokenRepository) {
	userRepo.On("GetOrCreateUser", mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).Return(fakeUser, nil)
	refreshTokenRepo.On("DeleteStaleFamiliesForUser", mock.Anything, fakeUser.ID.String()).Return(nil)
	refreshTokenRepo.On("CreateFamily", mock.Anything, mock.AnythingOfType("string"), fakeUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).
		Return(&models.RefreshTokenFamily{ID: uuid.New(), UserID: fakeUser.ID}, nil)
}

// doCallback performs a GET against /auth/google/callback with the given
// query values and, if cookieValue is non-nil, an oauthState cookie.
func doCallback(r *gin.Engine, code, state string, cookieValue *string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code="+code+"&state="+state, nil)
	if cookieValue != nil {
		req.AddCookie(&http.Cookie{Name: "oauthState", Value: *cookieValue})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func refreshCookieFrom(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == "refreshToken" {
			return c
		}
	}
	return nil
}

func errorFromLocation(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	loc, err := w.Result().Location()
	require.NoError(t, err)
	return loc.Query().Get("error")
}

func oauthStateCookieCleared(w *httptest.ResponseRecorder) bool {
	for _, c := range w.Result().Cookies() {
		if c.Name == "oauthState" {
			return c.MaxAge < 0
		}
	}
	return false
}

// --- GoogleCallback: happy path ---

func TestGoogleCallback_HappyPath(t *testing.T) {
	cases := []struct {
		env          config.Environment
		wantSecure   bool
		wantSameSite http.SameSite
	}{
		{config.EnvDevelopment, false, http.SameSiteLaxMode},
		{config.EnvProduction, true, http.SameSiteNoneMode},
	}

	for _, tc := range cases {
		t.Run(string(tc.env), func(t *testing.T) {
			m := newMocks(tc.env)
			mockSuccessfulExchange(m.oauthMgr)
			mockSuccessfulUserAndFamily(m.userRepo, m.refreshTokenRepo)

			cookie := "valid-state"
			w := doCallback(m.router, "auth-code", "valid-state", &cookie)

			assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
			assert.Equal(t, "http://localhost:5173/auth/callback", w.Header().Get("Location"))

			refreshCookie := refreshCookieFrom(w)
			require.NotNil(t, refreshCookie, "expected refreshToken cookie to be set")
			assert.True(t, refreshCookie.HttpOnly)
			assert.Equal(t, tc.wantSecure, refreshCookie.Secure)
			assert.Equal(t, tc.wantSameSite, refreshCookie.SameSite)

			m.assertExpectations(t)
		})
	}
}

// --- GoogleCallback: rejected before or during state validation ---

func TestGoogleCallback_RejectedAtStateValidation(t *testing.T) {
	cases := []struct {
		name        string
		code        string
		state       string
		cookieValue *string
		setup       func(oauthMgr *MockOAuthManager)
		wantError   string
	}{
		{
			name:        "missing code",
			code:        "",
			state:       "valid-state",
			cookieValue: ptr("valid-state"),
			wantError:   "missing_code",
		},
		{
			name:        "missing state",
			code:        "auth-code",
			state:       "",
			cookieValue: ptr("valid-state"),
			wantError:   "missing_state",
		},
		{
			name:        "missing cookie",
			code:        "auth-code",
			state:       "valid-state",
			cookieValue: nil,
			wantError:   "invalid_state",
		},
		{
			name:        "cookie mismatch",
			code:        "auth-code",
			state:       "valid-state",
			cookieValue: ptr("different-state"),
			wantError:   "invalid_state",
		},
		{
			name:        "state signature invalid",
			code:        "auth-code",
			state:       "bad-state",
			cookieValue: ptr("bad-state"),
			setup: func(oauthMgr *MockOAuthManager) {
				oauthMgr.On("ValidateState", "bad-state").Return(false)
			},
			wantError: "invalid_state",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(config.EnvDevelopment)
			if tc.setup != nil {
				tc.setup(m.oauthMgr)
			}

			w := doCallback(m.router, tc.code, tc.state, tc.cookieValue)

			assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
			assert.Equal(t, tc.wantError, errorFromLocation(t, w))
			assert.True(t, oauthStateCookieCleared(w), "expected oauthState cookie to be cleared on every rejection path")
			m.assertExpectations(t)
		})
	}
}

// --- GoogleCallback: fails after state validation succeeds ---

func TestGoogleCallback_FailsAfterStateValidation(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(oauthMgr *MockOAuthManager, userRepo *MockUserRepository, refreshTokenRepo *MockRefreshTokenRepository)
		wantError string
	}{
		{
			name: "exchange fails",
			setup: func(oauthMgr *MockOAuthManager, _ *MockUserRepository, _ *MockRefreshTokenRepository) {
				oauthMgr.On("ValidateState", "valid-state").Return(true)
				oauthMgr.On("ExchangeCodeForToken", mock.Anything, "auth-code").Return(nil, errors.New("exchange failed"))
			},
			wantError: "exchange_failed",
		},
		{
			name: "verify fails",
			setup: func(oauthMgr *MockOAuthManager, _ *MockUserRepository, _ *MockRefreshTokenRepository) {
				oauthMgr.On("ValidateState", "valid-state").Return(true)
				oauthMgr.On("ExchangeCodeForToken", mock.Anything, "auth-code").Return(fakeToken, nil)
				oauthMgr.On("VerifyIDToken", mock.Anything, fakeToken).Return(nil, errors.New("verify failed"))
			},
			wantError: "verify_failed",
		},
		{
			name: "email not verified",
			setup: func(oauthMgr *MockOAuthManager, _ *MockUserRepository, _ *MockRefreshTokenRepository) {
				oauthMgr.On("ValidateState", "valid-state").Return(true)
				oauthMgr.On("ExchangeCodeForToken", mock.Anything, "auth-code").Return(fakeToken, nil)
				unverifiedClaims := &auth.IDTokenClaims{
					Email: fakeClaims.Email, EmailVerified: false,
					GoogleID: fakeClaims.GoogleID, DisplayName: fakeClaims.DisplayName, AvatarURL: fakeClaims.AvatarURL,
				}
				oauthMgr.On("VerifyIDToken", mock.Anything, fakeToken).Return(unverifiedClaims, nil)
			},
			wantError: "email_not_verified",
		},
		{
			name: "email already registered with password",
			setup: func(oauthMgr *MockOAuthManager, userRepo *MockUserRepository, _ *MockRefreshTokenRepository) {
				mockSuccessfulExchange(oauthMgr)
				userRepo.On("GetOrCreateUser", mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).
					Return(nil, models.ErrEmailRegisteredWithPassword)
			},
			wantError: "email_registered_with_password",
		},
		{
			name: "GetOrCreateUser generic error",
			setup: func(oauthMgr *MockOAuthManager, userRepo *MockUserRepository, _ *MockRefreshTokenRepository) {
				mockSuccessfulExchange(oauthMgr)
				userRepo.On("GetOrCreateUser", mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).
					Return(nil, errors.New("db exploded"))
			},
			wantError: "server_error",
		},
		{
			name: "DeleteStaleFamiliesForUser fails",
			setup: func(oauthMgr *MockOAuthManager, userRepo *MockUserRepository, refreshTokenRepo *MockRefreshTokenRepository) {
				mockSuccessfulExchange(oauthMgr)
				userRepo.On("GetOrCreateUser", mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).Return(fakeUser, nil)
				refreshTokenRepo.On("DeleteStaleFamiliesForUser", mock.Anything, fakeUser.ID.String()).Return(errors.New("delete failed"))
			},
			wantError: "server_error",
		},
		{
			name: "CreateFamily fails",
			setup: func(oauthMgr *MockOAuthManager, userRepo *MockUserRepository, refreshTokenRepo *MockRefreshTokenRepository) {
				mockSuccessfulExchange(oauthMgr)
				userRepo.On("GetOrCreateUser", mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).Return(fakeUser, nil)
				refreshTokenRepo.On("DeleteStaleFamiliesForUser", mock.Anything, fakeUser.ID.String()).Return(nil)
				refreshTokenRepo.On("CreateFamily", mock.Anything, mock.AnythingOfType("string"), fakeUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).
					Return(nil, errors.New("insert failed"))
			},
			wantError: "server_error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(config.EnvDevelopment)
			tc.setup(m.oauthMgr, m.userRepo, m.refreshTokenRepo)

			cookie := "valid-state"
			w := doCallback(m.router, "auth-code", "valid-state", &cookie)

			assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
			assert.Equal(t, tc.wantError, errorFromLocation(t, w))
			assert.Nil(t, refreshCookieFrom(w), "no refresh cookie must be set on a failed callback")
			m.assertExpectations(t)
		})
	}
}

// --- LoginWithGoogle ---

func TestLoginWithGoogle_RedirectsToGoogleAuthURL(t *testing.T) {
	m := newMocks(config.EnvDevelopment)
	m.oauthMgr.On("GenerateState").Return("generated-state", nil)
	m.oauthMgr.On("GetAuthURL", "generated-state").Return("https://accounts.google.com/o/oauth2/v2/auth?state=generated-state")

	req := httptest.NewRequest(http.MethodGet, "/auth/google/login", nil)
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
	assert.Equal(t, "https://accounts.google.com/o/oauth2/v2/auth?state=generated-state", w.Header().Get("Location"))

	var stateCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "oauthState" {
			stateCookie = c
		}
	}
	require.NotNil(t, stateCookie, "expected oauthState cookie to be set")
	assert.Equal(t, "generated-state", stateCookie.Value)

	m.assertExpectations(t)
}

func TestLoginWithGoogle_GenerateStateError(t *testing.T) {
	m := newMocks(config.EnvDevelopment)
	m.oauthMgr.On("GenerateState").Return("", errors.New("signing failed"))

	req := httptest.NewRequest(http.MethodGet, "/auth/google/login", nil)
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
	assert.Equal(t, "server_error", errorFromLocation(t, w))
	m.assertExpectations(t)
}
