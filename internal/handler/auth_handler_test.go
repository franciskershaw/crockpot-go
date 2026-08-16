package handler_test

import (
	"bytes"
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
	emailSender      *genmocks.MockEmailSender
	router           *gin.Engine
}

func newMocks(t *testing.T, env config.Environment) *mocks {
	m := &mocks{
		userRepo:         genmocks.NewMockUserRepository(t),
		oauthMgr:         genmocks.NewMockOAuthManager(t),
		refreshTokenRepo: genmocks.NewMockRefreshTokenRepository(t),
		emailTokenRepo:   genmocks.NewMockEmailVerificationTokenRepository(t),
		emailSender:      genmocks.NewMockEmailSender(t),
	}
	h := handler.NewAuthHandler(m.userRepo, m.oauthMgr, m.refreshTokenRepo, m.emailTokenRepo, m.emailSender, &config.Config{
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
		})
	}
}
