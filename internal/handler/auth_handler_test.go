package handler_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

// mocks bundles the collaborator mocks (generated via `go tool mockery`, see mock_*_test.go) plus a router wired to a handler built from them.
type mocks struct {
	userRepo         *handler.MockUserRepository
	oauthMgr         *handler.MockOAuthManager
	refreshTokenRepo *handler.MockRefreshTokenRepository
	emailTokenRepo   *handler.MockEmailVerificationTokenRepository
	emailSender      *handler.MockEmailSender
	router           *gin.Engine
}

func newMocks(t *testing.T, env config.Environment) *mocks {
	m := &mocks{
		userRepo:         handler.NewMockUserRepository(t),
		oauthMgr:         handler.NewMockOAuthManager(t),
		refreshTokenRepo: handler.NewMockRefreshTokenRepository(t),
		emailTokenRepo:   handler.NewMockEmailVerificationTokenRepository(t),
		emailSender:      handler.NewMockEmailSender(t),
	}
	h := handler.NewAuthHandler(m.userRepo, m.oauthMgr, m.refreshTokenRepo, m.emailTokenRepo, m.emailSender, &config.Config{
		Environment:         env,
		JWTSecretRefresh:    testutil.TestRefreshSecret,
		JWTSecretOAuthState: testutil.TestOAuthStateSecret,
		FrontendURL:         "http://localhost:5173",
	})
	m.router = gin.New()
	m.router.GET("/auth/google/login", h.LoginWithGoogle)
	m.router.GET("/auth/google/callback", h.GoogleCallback)
	m.router.POST("/auth/register", h.Register)
	return m
}

// mockSuccessfulExchange wires the state-valid/exchange/verify chain to succeed.
func mockSuccessfulExchange(oauthMgr *handler.MockOAuthManager) {
	oauthMgr.EXPECT().ValidateState("valid-state").Return(true)
	oauthMgr.EXPECT().ExchangeCodeForToken(mock.Anything, "auth-code").Return(fakeToken, nil)
	oauthMgr.EXPECT().VerifyIDToken(mock.Anything, fakeToken).Return(fakeClaims, nil)
}

// mockSuccessfulUserAndFamily wires GetOrCreateUser/DeleteStaleFamiliesForUser/CreateFamily to all succeed.
func mockSuccessfulUserAndFamily(userRepo *handler.MockUserRepository, refreshTokenRepo *handler.MockRefreshTokenRepository) {
	userRepo.EXPECT().GetOrCreateUser(mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).Return(fakeUser, nil)
	refreshTokenRepo.EXPECT().DeleteStaleFamiliesForUser(mock.Anything, fakeUser.ID.String()).Return(nil)
	refreshTokenRepo.EXPECT().CreateFamily(mock.Anything, mock.AnythingOfType("string"), fakeUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).
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

func doRegister(r *gin.Engine, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeJSONBody(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
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
			m := newMocks(t, tc.env)
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
		setup       func(oauthMgr *handler.MockOAuthManager)
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
			setup: func(oauthMgr *handler.MockOAuthManager) {
				oauthMgr.EXPECT().ValidateState("bad-state").Return(false)
			},
			wantError: "invalid_state",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t, config.EnvDevelopment)
			if tc.setup != nil {
				tc.setup(m.oauthMgr)
			}

			w := doCallback(m.router, tc.code, tc.state, tc.cookieValue)

			assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
			assert.Equal(t, tc.wantError, errorFromLocation(t, w))
			assert.True(t, oauthStateCookieCleared(w), "expected oauthState cookie to be cleared on every rejection path")
		})
	}
}

// --- GoogleCallback: fails after state validation succeeds ---

func TestGoogleCallback_FailsAfterStateValidation(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(oauthMgr *handler.MockOAuthManager, userRepo *handler.MockUserRepository, refreshTokenRepo *handler.MockRefreshTokenRepository)
		wantError string
	}{
		{
			name: "exchange fails",
			setup: func(oauthMgr *handler.MockOAuthManager, _ *handler.MockUserRepository, _ *handler.MockRefreshTokenRepository) {
				oauthMgr.EXPECT().ValidateState("valid-state").Return(true)
				oauthMgr.EXPECT().ExchangeCodeForToken(mock.Anything, "auth-code").Return(nil, errors.New("exchange failed"))
			},
			wantError: "exchange_failed",
		},
		{
			name: "verify fails",
			setup: func(oauthMgr *handler.MockOAuthManager, _ *handler.MockUserRepository, _ *handler.MockRefreshTokenRepository) {
				oauthMgr.EXPECT().ValidateState("valid-state").Return(true)
				oauthMgr.EXPECT().ExchangeCodeForToken(mock.Anything, "auth-code").Return(fakeToken, nil)
				oauthMgr.EXPECT().VerifyIDToken(mock.Anything, fakeToken).Return(nil, errors.New("verify failed"))
			},
			wantError: "verify_failed",
		},
		{
			name: "email not verified",
			setup: func(oauthMgr *handler.MockOAuthManager, _ *handler.MockUserRepository, _ *handler.MockRefreshTokenRepository) {
				oauthMgr.EXPECT().ValidateState("valid-state").Return(true)
				oauthMgr.EXPECT().ExchangeCodeForToken(mock.Anything, "auth-code").Return(fakeToken, nil)
				unverifiedClaims := &auth.IDTokenClaims{
					Email: fakeClaims.Email, EmailVerified: false,
					GoogleID: fakeClaims.GoogleID, DisplayName: fakeClaims.DisplayName, AvatarURL: fakeClaims.AvatarURL,
				}
				oauthMgr.EXPECT().VerifyIDToken(mock.Anything, fakeToken).Return(unverifiedClaims, nil)
			},
			wantError: "email_not_verified",
		},
		{
			name: "email already registered with password",
			setup: func(oauthMgr *handler.MockOAuthManager, userRepo *handler.MockUserRepository, _ *handler.MockRefreshTokenRepository) {
				mockSuccessfulExchange(oauthMgr)
				userRepo.EXPECT().GetOrCreateUser(mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).
					Return(nil, models.ErrEmailRegisteredWithPassword)
			},
			wantError: "email_registered_with_password",
		},
		{
			name: "GetOrCreateUser generic error",
			setup: func(oauthMgr *handler.MockOAuthManager, userRepo *handler.MockUserRepository, _ *handler.MockRefreshTokenRepository) {
				mockSuccessfulExchange(oauthMgr)
				userRepo.EXPECT().GetOrCreateUser(mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).
					Return(nil, errors.New("db exploded"))
			},
			wantError: "server_error",
		},
		{
			name: "DeleteStaleFamiliesForUser fails",
			setup: func(oauthMgr *handler.MockOAuthManager, userRepo *handler.MockUserRepository, refreshTokenRepo *handler.MockRefreshTokenRepository) {
				mockSuccessfulExchange(oauthMgr)
				userRepo.EXPECT().GetOrCreateUser(mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).Return(fakeUser, nil)
				refreshTokenRepo.EXPECT().DeleteStaleFamiliesForUser(mock.Anything, fakeUser.ID.String()).Return(errors.New("delete failed"))
			},
			wantError: "server_error",
		},
		{
			name: "CreateFamily fails",
			setup: func(oauthMgr *handler.MockOAuthManager, userRepo *handler.MockUserRepository, refreshTokenRepo *handler.MockRefreshTokenRepository) {
				mockSuccessfulExchange(oauthMgr)
				userRepo.EXPECT().GetOrCreateUser(mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).Return(fakeUser, nil)
				refreshTokenRepo.EXPECT().DeleteStaleFamiliesForUser(mock.Anything, fakeUser.ID.String()).Return(nil)
				refreshTokenRepo.EXPECT().CreateFamily(mock.Anything, mock.AnythingOfType("string"), fakeUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).
					Return(nil, errors.New("insert failed"))
			},
			wantError: "server_error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t, config.EnvDevelopment)
			tc.setup(m.oauthMgr, m.userRepo, m.refreshTokenRepo)

			cookie := "valid-state"
			w := doCallback(m.router, "auth-code", "valid-state", &cookie)

			assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
			assert.Equal(t, tc.wantError, errorFromLocation(t, w))
			assert.Nil(t, refreshCookieFrom(w), "no refresh cookie must be set on a failed callback")
		})
	}
}

// --- LoginWithGoogle ---

func TestLoginWithGoogle_RedirectsToGoogleAuthURL(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	m.oauthMgr.EXPECT().GenerateState().Return("generated-state", nil)
	m.oauthMgr.EXPECT().GetAuthURL("generated-state").Return("https://accounts.google.com/o/oauth2/v2/auth?state=generated-state")

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
}

func TestLoginWithGoogle_GenerateStateError(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	m.oauthMgr.EXPECT().GenerateState().Return("", errors.New("signing failed"))

	req := httptest.NewRequest(http.MethodGet, "/auth/google/login", nil)
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
	assert.Equal(t, "server_error", errorFromLocation(t, w))
}

// --- Register: succeeds ---

func TestRegister_Success(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	newUser := &models.User{ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Email: "new@example.com"}

	m.userRepo.EXPECT().CreateUnconfirmedUser(mock.Anything, "new@example.com", mock.AnythingOfType("string"), "New User").Return(newUser, nil)
	m.emailTokenRepo.EXPECT().DeleteActiveForUser(mock.Anything, newUser.ID.String()).Return(nil)
	m.emailTokenRepo.EXPECT().Create(mock.Anything, newUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(&models.EmailVerificationToken{}, nil)
	m.emailSender.EXPECT().SendConfirmationCode(mock.Anything, "new@example.com", mock.AnythingOfType("string")).Return(nil)

	w := doRegister(m.router, map[string]string{"email": "new@example.com", "password": "correcthorse", "name": "New User"})

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestRegister_UnconfirmedRetry_Succeeds(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	existingUser := &models.User{ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), Email: "retry@example.com"}

	m.userRepo.EXPECT().CreateUnconfirmedUser(mock.Anything, "retry@example.com", mock.AnythingOfType("string"), "New Name").Return(nil, models.ErrEmailUnconfirmed)
	m.userRepo.EXPECT().UpdatePasswordAndClearConfirmation(mock.Anything, "retry@example.com", mock.AnythingOfType("string"), "New Name").Return(existingUser, nil)
	m.emailTokenRepo.EXPECT().DeleteActiveForUser(mock.Anything, existingUser.ID.String()).Return(nil)
	m.emailTokenRepo.EXPECT().Create(mock.Anything, existingUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(&models.EmailVerificationToken{}, nil)
	m.emailSender.EXPECT().SendConfirmationCode(mock.Anything, "retry@example.com", mock.AnythingOfType("string")).Return(nil)

	w := doRegister(m.router, map[string]string{"email": "retry@example.com", "password": "correcthorse", "name": "New Name"})

	assert.Equal(t, http.StatusCreated, w.Code)
}

// --- Register: fails ---

func TestRegister_Fails(t *testing.T) {
	cases := []struct {
		name      string
		body      map[string]string
		setup     func(userRepo *handler.MockUserRepository)
		wantCode  int
		wantError string
	}{
		{
			name:      "password too short",
			body:      map[string]string{"email": "new@example.com", "password": "short", "name": "New User"},
			wantCode:  http.StatusBadRequest,
			wantError: "password_too_short",
		},
		{
			name:      "password too long",
			body:      map[string]string{"email": "new@example.com", "password": strings.Repeat("a", 73), "name": "New User"},
			wantCode:  http.StatusBadRequest,
			wantError: "password_too_long",
		},
		{
			name:      "invalid email",
			body:      map[string]string{"email": "not-an-email", "password": "correcthorse", "name": "New User"},
			wantCode:  http.StatusBadRequest,
			wantError: "invalid_request",
		},
		{
			name: "email already registered with google",
			body: map[string]string{"email": "taken@example.com", "password": "correcthorse", "name": "New User"},
			setup: func(userRepo *handler.MockUserRepository) {
				userRepo.EXPECT().CreateUnconfirmedUser(mock.Anything, "taken@example.com", mock.AnythingOfType("string"), "New User").
					Return(nil, models.ErrEmailRegisteredWithGoogle)
			},
			wantCode:  http.StatusConflict,
			wantError: "email_registered_with_google",
		},
		{
			name: "email already registered with password",
			body: map[string]string{"email": "taken@example.com", "password": "correcthorse", "name": "New User"},
			setup: func(userRepo *handler.MockUserRepository) {
				userRepo.EXPECT().CreateUnconfirmedUser(mock.Anything, "taken@example.com", mock.AnythingOfType("string"), "New User").
					Return(nil, models.ErrEmailRegisteredWithPassword)
			},
			wantCode:  http.StatusConflict,
			wantError: "email_already_registered",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t, config.EnvDevelopment)
			if tc.setup != nil {
				tc.setup(m.userRepo)
			}

			w := doRegister(m.router, tc.body)

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantError, decodeJSONBody(t, w)["error"])
		})
	}
}
