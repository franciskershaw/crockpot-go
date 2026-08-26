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

	"github.com/franciskershaw/crockpot-go/config"
	"github.com/franciskershaw/crockpot-go/internal/auth"
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

func mustHash(password string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(hash)
}

const loginUserPassword = "correcthorse"

var loginUserPasswordHash = mustHash(loginUserPassword)

// --- Helpers ---

// mocks bundles the collaborator mocks (generated via `go tool mockery`, see internal/handler/mocks/) plus a router wired to a handler built from them.
type mocks struct {
	userRepo         *genmocks.MockUserRepository
	oauthMgr         *genmocks.MockOAuthManager
	refreshTokenRepo *genmocks.MockRefreshTokenRepository
	emailTokenRepo   *genmocks.MockEmailVerificationTokenRepository
	resetTokenRepo   *genmocks.MockPasswordResetTokenRepository
	emailSender      *genmocks.MockEmailSender
	transactor       *genmocks.MockTransactor
	router           *gin.Engine
}

func newMocks(t *testing.T, env config.Environment) *mocks {
	m := &mocks{
		userRepo:         genmocks.NewMockUserRepository(t),
		oauthMgr:         genmocks.NewMockOAuthManager(t),
		refreshTokenRepo: genmocks.NewMockRefreshTokenRepository(t),
		emailTokenRepo:   genmocks.NewMockEmailVerificationTokenRepository(t),
		resetTokenRepo:   genmocks.NewMockPasswordResetTokenRepository(t),
		emailSender:      genmocks.NewMockEmailSender(t),
		transactor:       genmocks.NewMockTransactor(t),
	}
	// Every test gets a transactor that just runs the wrapped function directly — real transaction
	// behavior is covered by repository-layer tests against the real DB, not these handler mocks.
	m.transactor.EXPECT().WithinTx(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }).
		Maybe()
	h := handler.NewAuthHandler(m.userRepo, m.oauthMgr, m.refreshTokenRepo, m.emailTokenRepo, m.resetTokenRepo, m.emailSender, m.transactor, &config.Config{
		Environment:         env,
		JWTSecretAccess:     testutil.TestAccessSecret,
		JWTSecretRefresh:    testutil.TestRefreshSecret,
		JWTSecretOAuthState: testutil.TestOAuthStateSecret,
		FrontendURL:         "http://localhost:5173",
	})
	m.router = gin.New()
	m.router.GET("/auth/google/login", h.LoginWithGoogle)
	m.router.GET("/auth/google/callback", h.GoogleCallback)
	m.router.POST("/auth/register", h.Register)
	m.router.POST("/auth/confirm", h.ConfirmEmail)
	m.router.POST("/auth/resend-confirmation", h.ResendConfirmation)
	m.router.POST("/auth/login", h.Login)
	m.router.POST("/auth/forgot-password", h.ForgotPassword)
	m.router.POST("/auth/reset-password", h.ResetPassword)
	m.router.POST("/auth/refresh", h.RefreshToken)
	m.router.POST("/auth/logout", h.Logout)
	authed := m.router.Group("/")
	authed.Use(middleware.AuthMiddleware(testutil.TestAccessSecret))
	authed.GET("/me", h.Me)
	return m
}

// mockSuccessfulExchange wires the state-valid/exchange/verify chain to succeed.
func mockSuccessfulExchange(oauthMgr *genmocks.MockOAuthManager) {
	oauthMgr.EXPECT().ValidateState("valid-state").Return(true)
	oauthMgr.EXPECT().ExchangeCodeForToken(mock.Anything, "auth-code").Return(fakeToken, nil)
	oauthMgr.EXPECT().VerifyIDToken(mock.Anything, fakeToken).Return(fakeClaims, nil)
}

// mockSuccessfulUserAndFamily wires GetOrCreateUser/DeleteStaleFamiliesForUser/CreateFamily to all succeed.
func mockSuccessfulUserAndFamily(userRepo *genmocks.MockUserRepository, refreshTokenRepo *genmocks.MockRefreshTokenRepository) {
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

// decodeJSONBodyAny handles responses with non-string fields (e.g. retryAfterSeconds), where decodeJSONBody's map[string]string would fail to unmarshal.
func decodeJSONBodyAny(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
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
		setup       func(oauthMgr *genmocks.MockOAuthManager)
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
			setup: func(oauthMgr *genmocks.MockOAuthManager) {
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
		setup     func(oauthMgr *genmocks.MockOAuthManager, userRepo *genmocks.MockUserRepository, refreshTokenRepo *genmocks.MockRefreshTokenRepository)
		wantError string
	}{
		{
			name: "exchange fails",
			setup: func(oauthMgr *genmocks.MockOAuthManager, _ *genmocks.MockUserRepository, _ *genmocks.MockRefreshTokenRepository) {
				oauthMgr.EXPECT().ValidateState("valid-state").Return(true)
				oauthMgr.EXPECT().ExchangeCodeForToken(mock.Anything, "auth-code").Return(nil, errors.New("exchange failed"))
			},
			wantError: "exchange_failed",
		},
		{
			name: "verify fails",
			setup: func(oauthMgr *genmocks.MockOAuthManager, _ *genmocks.MockUserRepository, _ *genmocks.MockRefreshTokenRepository) {
				oauthMgr.EXPECT().ValidateState("valid-state").Return(true)
				oauthMgr.EXPECT().ExchangeCodeForToken(mock.Anything, "auth-code").Return(fakeToken, nil)
				oauthMgr.EXPECT().VerifyIDToken(mock.Anything, fakeToken).Return(nil, errors.New("verify failed"))
			},
			wantError: "verify_failed",
		},
		{
			name: "email not verified",
			setup: func(oauthMgr *genmocks.MockOAuthManager, _ *genmocks.MockUserRepository, _ *genmocks.MockRefreshTokenRepository) {
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
			setup: func(oauthMgr *genmocks.MockOAuthManager, userRepo *genmocks.MockUserRepository, _ *genmocks.MockRefreshTokenRepository) {
				mockSuccessfulExchange(oauthMgr)
				userRepo.EXPECT().GetOrCreateUser(mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).
					Return(nil, models.ErrEmailRegisteredWithPassword)
			},
			wantError: "email_registered_with_password",
		},
		{
			name: "GetOrCreateUser generic error",
			setup: func(oauthMgr *genmocks.MockOAuthManager, userRepo *genmocks.MockUserRepository, _ *genmocks.MockRefreshTokenRepository) {
				mockSuccessfulExchange(oauthMgr)
				userRepo.EXPECT().GetOrCreateUser(mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).
					Return(nil, errors.New("db exploded"))
			},
			wantError: "server_error",
		},
		{
			name: "DeleteStaleFamiliesForUser fails",
			setup: func(oauthMgr *genmocks.MockOAuthManager, userRepo *genmocks.MockUserRepository, refreshTokenRepo *genmocks.MockRefreshTokenRepository) {
				mockSuccessfulExchange(oauthMgr)
				userRepo.EXPECT().GetOrCreateUser(mock.Anything, fakeClaims.Email, fakeClaims.GoogleID, fakeClaims.DisplayName, fakeClaims.AvatarURL).Return(fakeUser, nil)
				refreshTokenRepo.EXPECT().DeleteStaleFamiliesForUser(mock.Anything, fakeUser.ID.String()).Return(errors.New("delete failed"))
			},
			wantError: "server_error",
		},
		{
			name: "CreateFamily fails",
			setup: func(oauthMgr *genmocks.MockOAuthManager, userRepo *genmocks.MockUserRepository, refreshTokenRepo *genmocks.MockRefreshTokenRepository) {
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
	m.emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, newUser.ID.String()).Return(nil, models.ErrNoActiveEmailVerificationToken)
	m.emailTokenRepo.EXPECT().DeleteActiveForUser(mock.Anything, newUser.ID.String()).Return(nil)
	m.emailTokenRepo.EXPECT().Create(mock.Anything, newUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(&models.EmailVerificationToken{}, nil)
	m.emailSender.EXPECT().SendConfirmationCode(mock.Anything, "new@example.com", mock.AnythingOfType("string")).Return(nil)

	w := doRegister(m.router, map[string]string{"email": "new@example.com", "password": "correcthorse", "name": "New User"})

	assert.Equal(t, http.StatusCreated, w.Code)
}

// A failed send must not leave a token behind that blocks the next attempt behind the resend cooldown.
func TestRegister_EmailSendFailureMarksTokenUsedInstead(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	newUser := &models.User{ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Email: "new@example.com"}
	createdToken := &models.EmailVerificationToken{ID: uuid.MustParse("88888888-8888-8888-8888-888888888888")}

	m.userRepo.EXPECT().CreateUnconfirmedUser(mock.Anything, "new@example.com", mock.AnythingOfType("string"), "New User").Return(newUser, nil)
	m.emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, newUser.ID.String()).Return(nil, models.ErrNoActiveEmailVerificationToken)
	m.emailTokenRepo.EXPECT().DeleteActiveForUser(mock.Anything, newUser.ID.String()).Return(nil)
	m.emailTokenRepo.EXPECT().Create(mock.Anything, newUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(createdToken, nil)
	m.emailSender.EXPECT().SendConfirmationCode(mock.Anything, "new@example.com", mock.AnythingOfType("string")).Return(errors.New("resend unreachable"))
	m.emailTokenRepo.EXPECT().MarkUsed(mock.Anything, createdToken.ID.String()).Return(nil)

	w := doRegister(m.router, map[string]string{"email": "new@example.com", "password": "correcthorse", "name": "New User"})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// No password-mutating mock is set up here on purpose — case (c) must never touch the existing password.
func TestRegister_UnconfirmedRetry_Succeeds(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	existingUser := &models.User{ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), Email: "retry@example.com", PasswordHash: ptr("original-hash-untouched")}

	m.userRepo.EXPECT().CreateUnconfirmedUser(mock.Anything, "retry@example.com", mock.AnythingOfType("string"), "New Name").Return(nil, models.ErrEmailUnconfirmed)
	m.userRepo.EXPECT().FindByEmail(mock.Anything, "retry@example.com").Return(existingUser, nil)
	m.emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, existingUser.ID.String()).Return(nil, models.ErrNoActiveEmailVerificationToken)
	m.emailTokenRepo.EXPECT().DeleteActiveForUser(mock.Anything, existingUser.ID.String()).Return(nil)
	m.emailTokenRepo.EXPECT().Create(mock.Anything, existingUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(&models.EmailVerificationToken{}, nil)
	m.emailSender.EXPECT().SendConfirmationCode(mock.Anything, "retry@example.com", mock.AnythingOfType("string")).Return(nil)

	w := doRegister(m.router, map[string]string{"email": "retry@example.com", "password": "attacker-chosen-password", "name": "New Name"})

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestRegister_UnconfirmedRetry_WithinCooldown(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	existingUser := &models.User{ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), Email: "retry@example.com"}

	m.userRepo.EXPECT().CreateUnconfirmedUser(mock.Anything, "retry@example.com", mock.AnythingOfType("string"), "New Name").Return(nil, models.ErrEmailUnconfirmed)
	m.userRepo.EXPECT().FindByEmail(mock.Anything, "retry@example.com").Return(existingUser, nil)
	m.emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, existingUser.ID.String()).Return(&models.EmailVerificationToken{
		ID: uuid.New(), UserID: existingUser.ID, ExpiresAt: time.Now().Add(5 * time.Minute),
		CreatedAt: time.Now().Add(-10 * time.Second),
	}, nil)

	w := doRegister(m.router, map[string]string{"email": "retry@example.com", "password": "correcthorse", "name": "New Name"})

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "resend_too_soon", decodeJSONBodyAny(t, w)["error"])
}

// --- Register: fails ---

func TestRegister_Fails(t *testing.T) {
	cases := []struct {
		name      string
		body      map[string]string
		setup     func(userRepo *genmocks.MockUserRepository)
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
			setup: func(userRepo *genmocks.MockUserRepository) {
				userRepo.EXPECT().CreateUnconfirmedUser(mock.Anything, "taken@example.com", mock.AnythingOfType("string"), "New User").
					Return(nil, models.ErrEmailRegisteredWithGoogle)
			},
			wantCode:  http.StatusConflict,
			wantError: "email_registered_with_google",
		},
		{
			name: "email already registered with password",
			body: map[string]string{"email": "taken@example.com", "password": "correcthorse", "name": "New User"},
			setup: func(userRepo *genmocks.MockUserRepository) {
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

// --- ConfirmEmail ---

func doConfirm(r *gin.Engine, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/confirm", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestConfirmEmail_Success(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	confirmUser := &models.User{ID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), Email: "confirm@example.com"}
	token := &models.EmailVerificationToken{
		ID:        uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		UserID:    confirmUser.ID,
		TokenHash: auth.HashToken("482913"),
		Attempts:  0,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	m.userRepo.EXPECT().FindByEmail(mock.Anything, "confirm@example.com").Return(confirmUser, nil)
	m.emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, confirmUser.ID.String()).Return(token, nil)
	m.userRepo.EXPECT().MarkEmailConfirmed(mock.Anything, confirmUser.ID.String()).Return(confirmUser, nil)
	m.emailTokenRepo.EXPECT().MarkUsed(mock.Anything, token.ID.String()).Return(nil)

	w := doConfirm(m.router, map[string]string{"email": "confirm@example.com", "code": "482913"})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestConfirmEmail_Fails(t *testing.T) {
	confirmUser := &models.User{ID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), Email: "confirm@example.com"}

	cases := []struct {
		name      string
		setup     func(userRepo *genmocks.MockUserRepository, emailTokenRepo *genmocks.MockEmailVerificationTokenRepository)
		wantCode  int
		wantError string
	}{
		{
			name: "unknown email",
			setup: func(userRepo *genmocks.MockUserRepository, _ *genmocks.MockEmailVerificationTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "confirm@example.com").Return(nil, models.ErrUserNotFound)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "code_invalid",
		},
		{
			name: "FindByEmail generic error",
			setup: func(userRepo *genmocks.MockUserRepository, _ *genmocks.MockEmailVerificationTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "confirm@example.com").Return(nil, errors.New("db exploded"))
			},
			wantCode:  http.StatusInternalServerError,
			wantError: "server_error",
		},
		{
			name: "no active token",
			setup: func(userRepo *genmocks.MockUserRepository, emailTokenRepo *genmocks.MockEmailVerificationTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "confirm@example.com").Return(confirmUser, nil)
				emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, confirmUser.ID.String()).Return(nil, models.ErrNoActiveEmailVerificationToken)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "code_invalid",
		},
		{
			name: "expired token",
			setup: func(userRepo *genmocks.MockUserRepository, emailTokenRepo *genmocks.MockEmailVerificationTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "confirm@example.com").Return(confirmUser, nil)
				emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, confirmUser.ID.String()).Return(&models.EmailVerificationToken{
					ID: uuid.New(), UserID: confirmUser.ID, TokenHash: auth.HashToken("482913"),
					Attempts: 0, ExpiresAt: time.Now().Add(-time.Minute),
				}, nil)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "code_expired",
		},
		{
			name: "wrong code increments attempts",
			setup: func(userRepo *genmocks.MockUserRepository, emailTokenRepo *genmocks.MockEmailVerificationTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "confirm@example.com").Return(confirmUser, nil)
				tokenID := uuid.New()
				emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, confirmUser.ID.String()).Return(&models.EmailVerificationToken{
					ID: tokenID, UserID: confirmUser.ID, TokenHash: auth.HashToken("482913"),
					Attempts: 0, ExpiresAt: time.Now().Add(5 * time.Minute),
				}, nil)
				emailTokenRepo.EXPECT().IncrementAttempts(mock.Anything, tokenID.String()).Return(&models.EmailVerificationToken{ID: tokenID, Attempts: 1}, nil)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "code_invalid",
		},
		{
			name: "already locked out",
			setup: func(userRepo *genmocks.MockUserRepository, emailTokenRepo *genmocks.MockEmailVerificationTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "confirm@example.com").Return(confirmUser, nil)
				emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, confirmUser.ID.String()).Return(&models.EmailVerificationToken{
					ID: uuid.New(), UserID: confirmUser.ID, TokenHash: auth.HashToken("482913"),
					Attempts: 5, ExpiresAt: time.Now().Add(5 * time.Minute),
				}, nil)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "too_many_attempts",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t, config.EnvDevelopment)
			tc.setup(m.userRepo, m.emailTokenRepo)

			w := doConfirm(m.router, map[string]string{"email": "confirm@example.com", "code": "000000"})

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantError, decodeJSONBody(t, w)["error"])
		})
	}
}

// --- ResendConfirmation ---

func doResend(r *gin.Engine, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/resend-confirmation", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestResendConfirmation_SuccessNoExistingToken(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	resendUser := &models.User{ID: uuid.MustParse("66666666-6666-6666-6666-666666666666"), Email: "resend@example.com"}

	m.userRepo.EXPECT().FindByEmail(mock.Anything, "resend@example.com").Return(resendUser, nil)
	m.emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, resendUser.ID.String()).Return(nil, models.ErrNoActiveEmailVerificationToken)
	m.emailTokenRepo.EXPECT().DeleteActiveForUser(mock.Anything, resendUser.ID.String()).Return(nil)
	m.emailTokenRepo.EXPECT().Create(mock.Anything, resendUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(&models.EmailVerificationToken{}, nil)
	m.emailSender.EXPECT().SendConfirmationCode(mock.Anything, "resend@example.com", mock.AnythingOfType("string")).Return(nil)

	w := doResend(m.router, map[string]string{"email": "resend@example.com"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "confirmation code resent", decodeJSONBody(t, w)["message"])
}

func TestResendConfirmation_SuccessPastCooldown(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	resendUser := &models.User{ID: uuid.MustParse("66666666-6666-6666-6666-666666666666"), Email: "resend@example.com"}
	oldToken := &models.EmailVerificationToken{
		ID: uuid.New(), UserID: resendUser.ID, ExpiresAt: time.Now().Add(5 * time.Minute),
		CreatedAt: time.Now().Add(-90 * time.Second),
	}

	m.userRepo.EXPECT().FindByEmail(mock.Anything, "resend@example.com").Return(resendUser, nil)
	m.emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, resendUser.ID.String()).Return(oldToken, nil)
	m.emailTokenRepo.EXPECT().DeleteActiveForUser(mock.Anything, resendUser.ID.String()).Return(nil)
	m.emailTokenRepo.EXPECT().Create(mock.Anything, resendUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(&models.EmailVerificationToken{}, nil)
	m.emailSender.EXPECT().SendConfirmationCode(mock.Anything, "resend@example.com", mock.AnythingOfType("string")).Return(nil)

	w := doResend(m.router, map[string]string{"email": "resend@example.com"})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestResendConfirmation_Fails(t *testing.T) {
	resendUser := &models.User{ID: uuid.MustParse("66666666-6666-6666-6666-666666666666"), Email: "resend@example.com"}
	confirmedUser := &models.User{ID: uuid.MustParse("77777777-7777-7777-7777-777777777777"), Email: "resend@example.com", EmailVerifiedAt: ptr(time.Now())}

	cases := []struct {
		name      string
		setup     func(userRepo *genmocks.MockUserRepository, emailTokenRepo *genmocks.MockEmailVerificationTokenRepository)
		wantCode  int
		wantError string
	}{
		{
			name: "unknown email",
			setup: func(userRepo *genmocks.MockUserRepository, _ *genmocks.MockEmailVerificationTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "resend@example.com").Return(nil, models.ErrUserNotFound)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "email_not_found",
		},
		{
			name: "FindByEmail generic error",
			setup: func(userRepo *genmocks.MockUserRepository, _ *genmocks.MockEmailVerificationTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "resend@example.com").Return(nil, errors.New("db exploded"))
			},
			wantCode:  http.StatusInternalServerError,
			wantError: "server_error",
		},
		{
			name: "already confirmed",
			setup: func(userRepo *genmocks.MockUserRepository, _ *genmocks.MockEmailVerificationTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "resend@example.com").Return(confirmedUser, nil)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "already_confirmed",
		},
		{
			name: "within cooldown",
			setup: func(userRepo *genmocks.MockUserRepository, emailTokenRepo *genmocks.MockEmailVerificationTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "resend@example.com").Return(resendUser, nil)
				emailTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, resendUser.ID.String()).Return(&models.EmailVerificationToken{
					ID: uuid.New(), UserID: resendUser.ID, ExpiresAt: time.Now().Add(5 * time.Minute),
					CreatedAt: time.Now().Add(-10 * time.Second),
				}, nil)
			},
			wantCode:  http.StatusTooManyRequests,
			wantError: "resend_too_soon",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t, config.EnvDevelopment)
			tc.setup(m.userRepo, m.emailTokenRepo)

			w := doResend(m.router, map[string]string{"email": "resend@example.com"})

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantError, decodeJSONBodyAny(t, w)["error"])
		})
	}
}

// --- Login ---

func doLogin(r *gin.Engine, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// success: FindByEmail -> password match -> confirmed -> UpdateLastLogin -> issue tokens.
func TestLogin_Success(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	loginUser := &models.User{
		ID:              uuid.MustParse("99999999-9999-9999-9999-999999999999"),
		Email:           "login@example.com",
		PasswordHash:    ptr(loginUserPasswordHash),
		EmailVerifiedAt: ptr(time.Now()),
	}

	m.userRepo.EXPECT().FindByEmail(mock.Anything, "login@example.com").Return(loginUser, nil)
	m.userRepo.EXPECT().UpdateLastLogin(mock.Anything, loginUser.ID.String()).Return(loginUser, nil)
	m.refreshTokenRepo.EXPECT().DeleteStaleFamiliesForUser(mock.Anything, loginUser.ID.String()).Return(nil)
	m.refreshTokenRepo.EXPECT().CreateFamily(mock.Anything, mock.AnythingOfType("string"), loginUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).
		Return(&models.RefreshTokenFamily{ID: uuid.New(), UserID: loginUser.ID}, nil)

	w := doLogin(m.router, map[string]string{"email": "login@example.com", "password": loginUserPassword})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, decodeJSONBody(t, w)["accessToken"])
	require.NotNil(t, refreshCookieFrom(w), "expected refreshToken cookie to be set")
}

func TestLogin_Fails(t *testing.T) {
	pwUser := &models.User{
		ID: uuid.MustParse("99999999-9999-9999-9999-999999999999"), Email: "login@example.com",
		PasswordHash: ptr(loginUserPasswordHash), EmailVerifiedAt: ptr(time.Now()),
	}
	unconfirmedUser := &models.User{
		ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), Email: "unconfirmed@example.com",
		PasswordHash: ptr(loginUserPasswordHash),
	}
	googleOnlyUser := &models.User{
		ID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), Email: "google@example.com",
		GoogleID: ptr("google-123"),
	}

	cases := []struct {
		name      string
		body      map[string]string
		setup     func(userRepo *genmocks.MockUserRepository)
		wantCode  int
		wantError string
	}{
		{
			name:      "malformed body",
			body:      map[string]string{"email": "not-an-email", "password": "whatever"},
			wantCode:  http.StatusBadRequest,
			wantError: "invalid_request",
		},
		{
			name: "unknown email",
			body: map[string]string{"email": "ghost@example.com", "password": loginUserPassword},
			setup: func(userRepo *genmocks.MockUserRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "ghost@example.com").Return(nil, models.ErrUserNotFound)
			},
			wantCode:  http.StatusUnauthorized,
			wantError: "invalid_credentials",
		},
		{
			name: "FindByEmail generic error",
			body: map[string]string{"email": "ghost@example.com", "password": loginUserPassword},
			setup: func(userRepo *genmocks.MockUserRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "ghost@example.com").Return(nil, errors.New("db exploded"))
			},
			wantCode:  http.StatusInternalServerError,
			wantError: "server_error",
		},
		{
			name: "google-only account has no password to check against",
			body: map[string]string{"email": "google@example.com", "password": "whatever"},
			setup: func(userRepo *genmocks.MockUserRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "google@example.com").Return(googleOnlyUser, nil)
			},
			wantCode:  http.StatusUnauthorized,
			wantError: "google_account_no_password",
		},
		{
			name: "wrong password",
			body: map[string]string{"email": "login@example.com", "password": "wrong-password"},
			setup: func(userRepo *genmocks.MockUserRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "login@example.com").Return(pwUser, nil)
			},
			wantCode:  http.StatusUnauthorized,
			wantError: "invalid_credentials",
		},
		{
			// Password matches but the account is unconfirmed — checked after password so an unauthenticated request can't probe account state for free.
			name: "unconfirmed account, correct password",
			body: map[string]string{"email": "unconfirmed@example.com", "password": loginUserPassword},
			setup: func(userRepo *genmocks.MockUserRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "unconfirmed@example.com").Return(unconfirmedUser, nil)
			},
			wantCode:  http.StatusForbidden,
			wantError: "email_not_confirmed",
		},
		{
			// UpdateLastLogin runs before issueRefreshSession precisely so a failure here — asserted below — never leaves a live session behind.
			name: "UpdateLastLogin fails",
			body: map[string]string{"email": "login@example.com", "password": loginUserPassword},
			setup: func(userRepo *genmocks.MockUserRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "login@example.com").Return(pwUser, nil)
				userRepo.EXPECT().UpdateLastLogin(mock.Anything, pwUser.ID.String()).Return(nil, errors.New("db exploded"))
			},
			wantCode:  http.StatusInternalServerError,
			wantError: "server_error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t, config.EnvDevelopment)
			if tc.setup != nil {
				tc.setup(m.userRepo)
			}

			w := doLogin(m.router, tc.body)

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantError, decodeJSONBody(t, w)["error"])
			assert.Nil(t, refreshCookieFrom(w), "no refresh cookie must be set on a failed login")
		})
	}
}

// --- ForgotPassword ---

func doForgotPassword(r *gin.Engine, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestForgotPassword_SuccessNoExistingToken(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	forgotUser := &models.User{ID: uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"), Email: "forgot@example.com", PasswordHash: ptr(loginUserPasswordHash)}

	m.userRepo.EXPECT().FindByEmail(mock.Anything, "forgot@example.com").Return(forgotUser, nil)
	m.resetTokenRepo.EXPECT().AcquireUserLock(mock.Anything, forgotUser.ID.String()).Return(nil)
	m.resetTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, forgotUser.ID.String()).Return(nil, models.ErrNoActivePasswordResetToken)
	m.resetTokenRepo.EXPECT().DeleteActiveForUser(mock.Anything, forgotUser.ID.String()).Return(nil)
	m.resetTokenRepo.EXPECT().Create(mock.Anything, forgotUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(&models.PasswordResetToken{}, nil)
	m.emailSender.EXPECT().SendPasswordResetLink(mock.Anything, "forgot@example.com", mock.AnythingOfType("string")).Return(nil)

	w := doForgotPassword(m.router, map[string]string{"email": "forgot@example.com"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, decodeJSONBody(t, w)["message"])
}

func TestForgotPassword_Fails(t *testing.T) {
	forgotUser := &models.User{ID: uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"), Email: "forgot@example.com", PasswordHash: ptr(loginUserPasswordHash)}
	googleOnlyUser := &models.User{ID: uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"), Email: "google@example.com", GoogleID: ptr("google-123")}

	cases := []struct {
		name      string
		email     string
		setup     func(userRepo *genmocks.MockUserRepository, resetTokenRepo *genmocks.MockPasswordResetTokenRepository)
		wantCode  int
		wantError string
	}{
		{
			name:      "malformed body",
			email:     "not-an-email",
			wantCode:  http.StatusBadRequest,
			wantError: "invalid_request",
		},
		{
			name:  "unknown email",
			email: "ghost@example.com",
			setup: func(userRepo *genmocks.MockUserRepository, _ *genmocks.MockPasswordResetTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "ghost@example.com").Return(nil, models.ErrUserNotFound)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "email_not_found",
		},
		{
			name:  "FindByEmail generic error",
			email: "ghost@example.com",
			setup: func(userRepo *genmocks.MockUserRepository, _ *genmocks.MockPasswordResetTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "ghost@example.com").Return(nil, errors.New("db exploded"))
			},
			wantCode:  http.StatusInternalServerError,
			wantError: "server_error",
		},
		{
			name:  "google-only account has no password to reset",
			email: "google@example.com",
			setup: func(userRepo *genmocks.MockUserRepository, _ *genmocks.MockPasswordResetTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "google@example.com").Return(googleOnlyUser, nil)
			},
			wantCode:  http.StatusUnauthorized,
			wantError: "google_account_no_password",
		},
		{
			name:  "within cooldown",
			email: "forgot@example.com",
			setup: func(userRepo *genmocks.MockUserRepository, resetTokenRepo *genmocks.MockPasswordResetTokenRepository) {
				userRepo.EXPECT().FindByEmail(mock.Anything, "forgot@example.com").Return(forgotUser, nil)
				resetTokenRepo.EXPECT().AcquireUserLock(mock.Anything, forgotUser.ID.String()).Return(nil)
				resetTokenRepo.EXPECT().FindActiveByUserID(mock.Anything, forgotUser.ID.String()).Return(&models.PasswordResetToken{
					ID: uuid.New(), UserID: forgotUser.ID, ExpiresAt: time.Now().Add(50 * time.Minute),
					CreatedAt: time.Now().Add(-10 * time.Second),
				}, nil)
			},
			wantCode:  http.StatusTooManyRequests,
			wantError: "resend_too_soon",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t, config.EnvDevelopment)
			if tc.setup != nil {
				tc.setup(m.userRepo, m.resetTokenRepo)
			}

			w := doForgotPassword(m.router, map[string]string{"email": tc.email})

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantError, decodeJSONBodyAny(t, w)["error"])
		})
	}
}

// --- ResetPassword ---

func doResetPassword(r *gin.Engine, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const resetPasswordPlaintext = "new-correct-horse"

func TestResetPassword_Success(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	resetUser := &models.User{
		ID: uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"), Email: "reset@example.com",
		PasswordHash: ptr(loginUserPasswordHash), Role: "FREE",
	}
	resetToken := &models.PasswordResetToken{
		ID: uuid.New(), UserID: resetUser.ID, ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	var order []string

	m.resetTokenRepo.EXPECT().FindActiveByTokenHash(mock.Anything, auth.HashToken("some-opaque-token")).Return(resetToken, nil)
	m.resetTokenRepo.EXPECT().MarkUsed(mock.Anything, resetToken.ID.String()).
		Run(func(context.Context, string) { order = append(order, "MarkUsed") }).
		Return(true, nil)
	m.userRepo.EXPECT().UpdatePassword(mock.Anything, resetUser.ID.String(), mock.AnythingOfType("string")).
		Run(func(context.Context, string, string) { order = append(order, "UpdatePassword") }).
		Return(resetUser, nil)
	m.userRepo.EXPECT().MarkEmailConfirmed(mock.Anything, resetUser.ID.String()).
		Run(func(context.Context, string) { order = append(order, "MarkEmailConfirmed") }).
		Return(resetUser, nil)
	m.refreshTokenRepo.EXPECT().RevokeAllFamiliesForUser(mock.Anything, resetUser.ID.String()).
		Run(func(context.Context, string) { order = append(order, "RevokeAllFamiliesForUser") }).
		Return(nil)
	m.refreshTokenRepo.EXPECT().DeleteStaleFamiliesForUser(mock.Anything, resetUser.ID.String()).
		Run(func(context.Context, string) { order = append(order, "DeleteStaleFamiliesForUser") }).
		Return(nil)
	m.refreshTokenRepo.EXPECT().CreateFamily(mock.Anything, mock.AnythingOfType("string"), resetUser.ID.String(), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).
		Run(func(context.Context, string, string, string, time.Time) { order = append(order, "CreateFamily") }).
		Return(&models.RefreshTokenFamily{ID: uuid.New(), UserID: resetUser.ID}, nil)

	w := doResetPassword(m.router, map[string]string{"token": "some-opaque-token", "newPassword": resetPasswordPlaintext})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, decodeJSONBody(t, w)["accessToken"])
	require.NotNil(t, refreshCookieFrom(w), "expected refreshToken cookie to be set")
	assert.Equal(t, []string{"MarkUsed", "UpdatePassword", "MarkEmailConfirmed", "RevokeAllFamiliesForUser", "DeleteStaleFamiliesForUser", "CreateFamily"}, order)
}

func TestResetPassword_Fails(t *testing.T) {
	resetUser := &models.User{
		ID: uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"), Email: "reset@example.com",
		PasswordHash: ptr(loginUserPasswordHash), Role: "FREE",
	}

	cases := []struct {
		name      string
		body      map[string]string
		setup     func(resetTokenRepo *genmocks.MockPasswordResetTokenRepository)
		wantCode  int
		wantError string
	}{
		{
			name:      "malformed body",
			body:      map[string]string{"newPassword": resetPasswordPlaintext},
			wantCode:  http.StatusBadRequest,
			wantError: "invalid_request",
		},
		{
			name: "invalid token",
			body: map[string]string{"token": "bogus-token", "newPassword": resetPasswordPlaintext},
			setup: func(resetTokenRepo *genmocks.MockPasswordResetTokenRepository) {
				resetTokenRepo.EXPECT().FindActiveByTokenHash(mock.Anything, mock.AnythingOfType("string")).Return(nil, models.ErrNoActivePasswordResetToken)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "token_invalid",
		},
		{
			name: "expired token",
			body: map[string]string{"token": "expired-token", "newPassword": resetPasswordPlaintext},
			setup: func(resetTokenRepo *genmocks.MockPasswordResetTokenRepository) {
				resetTokenRepo.EXPECT().FindActiveByTokenHash(mock.Anything, mock.AnythingOfType("string")).Return(&models.PasswordResetToken{
					ID: uuid.New(), UserID: resetUser.ID, ExpiresAt: time.Now().Add(-time.Minute),
				}, nil)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "token_expired",
		},
		{
			name: "password too short",
			body: map[string]string{"token": "valid-token", "newPassword": "short"},
			setup: func(resetTokenRepo *genmocks.MockPasswordResetTokenRepository) {
				resetTokenRepo.EXPECT().FindActiveByTokenHash(mock.Anything, mock.AnythingOfType("string")).Return(&models.PasswordResetToken{
					ID: uuid.New(), UserID: resetUser.ID, ExpiresAt: time.Now().Add(30 * time.Minute),
				}, nil)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "password_too_short",
		},
		{
			name: "password too long",
			body: map[string]string{"token": "valid-token", "newPassword": strings.Repeat("a", 73)},
			setup: func(resetTokenRepo *genmocks.MockPasswordResetTokenRepository) {
				resetTokenRepo.EXPECT().FindActiveByTokenHash(mock.Anything, mock.AnythingOfType("string")).Return(&models.PasswordResetToken{
					ID: uuid.New(), UserID: resetUser.ID, ExpiresAt: time.Now().Add(30 * time.Minute),
				}, nil)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "password_too_long",
		},
		{
			// Simulates a concurrent request winning the race to claim the same token first —
			// MarkUsed's atomic conditional UPDATE reports 0 rows affected.
			name: "token claimed by a concurrent request",
			body: map[string]string{"token": "valid-token", "newPassword": resetPasswordPlaintext},
			setup: func(resetTokenRepo *genmocks.MockPasswordResetTokenRepository) {
				token := &models.PasswordResetToken{
					ID: uuid.New(), UserID: resetUser.ID, ExpiresAt: time.Now().Add(30 * time.Minute),
				}
				resetTokenRepo.EXPECT().FindActiveByTokenHash(mock.Anything, mock.AnythingOfType("string")).Return(token, nil)
				resetTokenRepo.EXPECT().MarkUsed(mock.Anything, token.ID.String()).Return(false, nil)
			},
			wantCode:  http.StatusBadRequest,
			wantError: "token_invalid",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t, config.EnvDevelopment)
			if tc.setup != nil {
				tc.setup(m.resetTokenRepo)
			}

			w := doResetPassword(m.router, tc.body)

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantError, decodeJSONBodyAny(t, w)["error"])
			assert.Nil(t, refreshCookieFrom(w), "no refresh cookie must be set on a failed reset")
		})
	}
}

// --- Refresh / Logout ---

var (
	refreshTestUserID   = uuid.MustParse("44444444-4444-4444-4444-444444444444")
	refreshTestFamilyID = "55555555-5555-5555-5555-555555555555"
	refreshTestUser     = &models.User{ID: refreshTestUserID, Email: "refresh@example.com", Role: "FREE"}
)

// mustRefreshToken generates a real, validly-signed refresh JWT for refreshTestUserID/refreshTestFamilyID — the handler decodes it for real, only the repository lookups are mocked.
func mustRefreshToken(t *testing.T) string {
	t.Helper()
	token, err := auth.GenerateRefreshToken(refreshTestUserID.String(), refreshTestFamilyID, testutil.TestRefreshSecret)
	require.NoError(t, err)
	return token
}

func doRefresh(r *gin.Engine, cookieValue *string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	if cookieValue != nil {
		req.AddCookie(&http.Cookie{Name: "refreshToken", Value: *cookieValue})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doLogout(r *gin.Engine, cookieValue *string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	if cookieValue != nil {
		req.AddCookie(&http.Cookie{Name: "refreshToken", Value: *cookieValue})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Current-hash vs. previous-hash-within-grace-window is now decided inside RotateFamily's atomic
// UPDATE (see refresh_token_test.go) — the handler just acts on its bool result.
func TestRefreshToken_RotatesWhenAtomicRotateSucceeds(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	token := mustRefreshToken(t)
	family := &models.RefreshTokenFamily{ID: uuid.MustParse(refreshTestFamilyID), UserID: refreshTestUserID}

	m.refreshTokenRepo.EXPECT().FindFamilyByID(mock.Anything, refreshTestFamilyID, refreshTestUserID.String()).Return(family, nil)
	m.userRepo.EXPECT().FindByID(mock.Anything, refreshTestUserID.String()).Return(refreshTestUser, nil)
	m.refreshTokenRepo.EXPECT().RotateFamily(mock.Anything, refreshTestFamilyID, auth.HashToken(token), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
		Return(true, nil)

	w := doRefresh(m.router, &token)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, decodeJSONBody(t, w)["accessToken"])
	newCookie := refreshCookieFrom(w)
	require.NotNil(t, newCookie, "expected a rotated refreshToken cookie to be set")
	assert.NotEqual(t, token, newCookie.Value, "expected a rotated refresh token, not the one presented")
}

// Both "matched previous_token_hash outside the grace window" and "matched neither hash at all"
// surface identically here: RotateFamily reports no match, so nothing was mutated and it's a reuse.
func TestRefreshToken_RevokesWhenAtomicRotateReportsNoMatch(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	token := mustRefreshToken(t)
	family := &models.RefreshTokenFamily{ID: uuid.MustParse(refreshTestFamilyID), UserID: refreshTestUserID}

	m.refreshTokenRepo.EXPECT().FindFamilyByID(mock.Anything, refreshTestFamilyID, refreshTestUserID.String()).Return(family, nil)
	m.userRepo.EXPECT().FindByID(mock.Anything, refreshTestUserID.String()).Return(refreshTestUser, nil)
	m.refreshTokenRepo.EXPECT().RotateFamily(mock.Anything, refreshTestFamilyID, auth.HashToken(token), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
		Return(false, nil)
	m.refreshTokenRepo.EXPECT().RevokeFamily(mock.Anything, refreshTestFamilyID).Return(nil)

	w := doRefresh(m.router, &token)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "invalid_refresh_token", decodeJSONBody(t, w)["error"])
	assert.Nil(t, refreshCookieFrom(w))
}

// A genuine RotateFamily error (DB failure) is not the same as an atomic no-match — it must not be
// treated as reuse, so RevokeFamily is deliberately not expected here.
func TestRefreshToken_FailsWhenAtomicRotateErrors(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	token := mustRefreshToken(t)
	family := &models.RefreshTokenFamily{ID: uuid.MustParse(refreshTestFamilyID), UserID: refreshTestUserID}

	m.refreshTokenRepo.EXPECT().FindFamilyByID(mock.Anything, refreshTestFamilyID, refreshTestUserID.String()).Return(family, nil)
	m.userRepo.EXPECT().FindByID(mock.Anything, refreshTestUserID.String()).Return(refreshTestUser, nil)
	m.refreshTokenRepo.EXPECT().RotateFamily(mock.Anything, refreshTestFamilyID, auth.HashToken(token), mock.AnythingOfType("string"), mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
		Return(false, errors.New("db exploded"))

	w := doRefresh(m.router, &token)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", decodeJSONBody(t, w)["error"])
}

func TestRefreshToken_RejectsAlreadyRevokedFamily(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	token := mustRefreshToken(t)
	revokedAt := time.Now().Add(-time.Minute)
	family := &models.RefreshTokenFamily{
		ID:        uuid.MustParse(refreshTestFamilyID),
		UserID:    refreshTestUserID,
		TokenHash: auth.HashToken(token),
		RevokedAt: &revokedAt,
	}

	// No RevokeFamily expectation — asserts an already-revoked family isn't re-revoked.
	m.refreshTokenRepo.EXPECT().FindFamilyByID(mock.Anything, refreshTestFamilyID, refreshTestUserID.String()).Return(family, nil)

	w := doRefresh(m.router, &token)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "invalid_refresh_token", decodeJSONBody(t, w)["error"])
}

// A failed user lookup must never leave a rotated-but-unusable session behind.
func TestRefreshToken_FailsBeforeRotatingWhenUserLookupFails(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	token := mustRefreshToken(t)
	family := &models.RefreshTokenFamily{
		ID:        uuid.MustParse(refreshTestFamilyID),
		UserID:    refreshTestUserID,
		TokenHash: auth.HashToken(token),
	}

	m.refreshTokenRepo.EXPECT().FindFamilyByID(mock.Anything, refreshTestFamilyID, refreshTestUserID.String()).Return(family, nil)
	m.userRepo.EXPECT().FindByID(mock.Anything, refreshTestUserID.String()).Return(nil, errors.New("db exploded"))
	// RotateFamily deliberately has no expectation.

	w := doRefresh(m.router, &token)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", decodeJSONBody(t, w)["error"])
	assert.Nil(t, refreshCookieFrom(w), "no rotated cookie should be set when the user lookup fails")
}

func TestRefreshToken_ReturnsUnauthorizedWhenUserNotFound(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	token := mustRefreshToken(t)
	family := &models.RefreshTokenFamily{
		ID:        uuid.MustParse(refreshTestFamilyID),
		UserID:    refreshTestUserID,
		TokenHash: auth.HashToken(token),
	}

	m.refreshTokenRepo.EXPECT().FindFamilyByID(mock.Anything, refreshTestFamilyID, refreshTestUserID.String()).Return(family, nil)
	m.userRepo.EXPECT().FindByID(mock.Anything, refreshTestUserID.String()).Return(nil, models.ErrUserNotFound)
	// RotateFamily deliberately has no expectation.

	w := doRefresh(m.router, &token)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "unauthorized", decodeJSONBody(t, w)["error"])
	assert.Nil(t, refreshCookieFrom(w), "no rotated cookie should be set when the user is gone")
}

func TestRefreshToken_Fails(t *testing.T) {
	validToken := mustRefreshToken(t)
	malformed := "not-a-jwt"

	cases := []struct {
		name      string
		cookie    *string
		setup     func(refreshTokenRepo *genmocks.MockRefreshTokenRepository)
		wantCode  int
		wantError string
	}{
		{
			name:      "missing cookie",
			wantCode:  http.StatusUnauthorized,
			wantError: "invalid_refresh_token",
		},
		{
			name:      "malformed token",
			cookie:    &malformed,
			wantCode:  http.StatusUnauthorized,
			wantError: "invalid_refresh_token",
		},
		{
			name:   "family not found",
			cookie: &validToken,
			setup: func(refreshTokenRepo *genmocks.MockRefreshTokenRepository) {
				refreshTokenRepo.EXPECT().FindFamilyByID(mock.Anything, refreshTestFamilyID, refreshTestUserID.String()).
					Return(nil, models.ErrRefreshTokenFamilyNotFound)
			},
			wantCode:  http.StatusUnauthorized,
			wantError: "invalid_refresh_token",
		},
		{
			name:   "FindFamilyByID generic error",
			cookie: &validToken,
			setup: func(refreshTokenRepo *genmocks.MockRefreshTokenRepository) {
				refreshTokenRepo.EXPECT().FindFamilyByID(mock.Anything, refreshTestFamilyID, refreshTestUserID.String()).
					Return(nil, errors.New("db exploded"))
			},
			wantCode:  http.StatusInternalServerError,
			wantError: "server_error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t, config.EnvDevelopment)
			if tc.setup != nil {
				tc.setup(m.refreshTokenRepo)
			}

			w := doRefresh(m.router, tc.cookie)

			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.wantError, decodeJSONBody(t, w)["error"])
		})
	}
}

func TestLogout_RevokesMatchingFamily(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	token := mustRefreshToken(t)

	m.refreshTokenRepo.EXPECT().RevokeFamily(mock.Anything, refreshTestFamilyID).Return(nil)

	w := doLogout(m.router, &token)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, decodeJSONBody(t, w)["message"])
	cookie := refreshCookieFrom(w)
	require.NotNil(t, cookie, "expected the refreshToken cookie to be cleared")
	assert.Empty(t, cookie.Value)
	assert.True(t, cookie.MaxAge < 0)
}

// A presented, validly-signed token whose revoke actually fails must not report success — the family
// stays live server-side, so a 200 here would tell the client it's safely logged out when it isn't.
func TestLogout_ReturnsErrorWhenRevocationFails(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	token := mustRefreshToken(t)

	m.refreshTokenRepo.EXPECT().RevokeFamily(mock.Anything, refreshTestFamilyID).Return(errors.New("db exploded"))

	w := doLogout(m.router, &token)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", decodeJSONBody(t, w)["error"])
	cookie := refreshCookieFrom(w)
	require.NotNil(t, cookie, "expected the refreshToken cookie to still be cleared on a revoke failure")
	assert.Empty(t, cookie.Value)
	assert.True(t, cookie.MaxAge < 0)
}

func TestLogout_ClearsCookieEvenWithoutValidRefreshToken(t *testing.T) {
	malformed := "not-a-jwt"
	cases := []struct {
		name   string
		cookie *string
	}{
		{name: "no cookie", cookie: nil},
		{name: "malformed cookie", cookie: &malformed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t, config.EnvDevelopment)
			// No RevokeFamily expectation — asserts it's never called for a missing/invalid cookie.

			w := doLogout(m.router, tc.cookie)

			assert.Equal(t, http.StatusOK, w.Code)
			cookie := refreshCookieFrom(w)
			require.NotNil(t, cookie, "expected the refreshToken cookie to be cleared")
			assert.Empty(t, cookie.Value)
			assert.True(t, cookie.MaxAge < 0)
		})
	}
}

// --- Me ---

var (
	meTestUserID = uuid.MustParse("55555555-5555-5555-5555-555555555555")
	meTestName   = "Me Test User"
	meTestImage  = "https://example.com/avatar.png"
	meTestUser   = &models.User{
		ID:    meTestUserID,
		Email: "me@example.com",
		Name:  &meTestName,
		Image: &meTestImage,
		Role:  "FREE",
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
	m := newMocks(t, config.EnvDevelopment)
	m.userRepo.EXPECT().FindByID(mock.Anything, meTestUserID.String()).Return(meTestUser, nil)

	w := doMe(m.router, testutil.AuthHeader(t, meTestUser.Email, meTestUserID.String(), meTestUser.Role))

	assert.Equal(t, http.StatusOK, w.Code)
	body := decodeJSONBodyAny(t, w)
	assert.Equal(t, meTestUserID.String(), body["id"])
	assert.Equal(t, meTestUser.Email, body["email"])
	assert.Equal(t, meTestName, body["name"])
	assert.Equal(t, meTestImage, body["image"])
	assert.Equal(t, meTestUser.Role, body["role"])
}

func TestMe_ReturnsUnauthorizedWhenUserNotFound(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	m.userRepo.EXPECT().FindByID(mock.Anything, meTestUserID.String()).Return(nil, models.ErrUserNotFound)

	w := doMe(m.router, testutil.AuthHeader(t, meTestUser.Email, meTestUserID.String(), meTestUser.Role))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "unauthorized", decodeJSONBody(t, w)["error"])
}

func TestMe_ReturnsServerErrorOnOtherRepositoryError(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	m.userRepo.EXPECT().FindByID(mock.Anything, meTestUserID.String()).Return(nil, errors.New("db exploded"))

	w := doMe(m.router, testutil.AuthHeader(t, meTestUser.Email, meTestUserID.String(), meTestUser.Role))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "server_error", decodeJSONBody(t, w)["error"])
}

// Defensive branch: unreachable through the wired router (AuthMiddleware always sets userID
// before Me runs), exercised by calling the handler directly against a bare context instead.
func TestMe_ReturnsUnauthorizedWhenUserIDMissingFromContext(t *testing.T) {
	m := newMocks(t, config.EnvDevelopment)
	h := handler.NewAuthHandler(m.userRepo, m.oauthMgr, m.refreshTokenRepo, m.emailTokenRepo, m.resetTokenRepo, m.emailSender, m.transactor, &config.Config{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/me", nil)

	h.Me(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "unauthorized", decodeJSONBody(t, w)["error"])
}
