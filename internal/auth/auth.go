package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"townsquare/internal/store"
)

const (
	SessionCookie = "ts_session"
	CSRFCookie    = "ts_csrf"
)

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]{3,24}$`)

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

var dummyPasswordHash = func() string {
	hash, err := bcrypt.GenerateFromPassword([]byte("townsquare-dummy-password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(hash)
}()

func DummyPasswordCheck(pw string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(pw))
}

func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NewRecoveryCode() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return enc[0:4] + "-" + enc[4:8] + "-" + enc[8:12] + "-" + enc[12:16]
}

func NormalizeRecoveryCode(v string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(v) {
		if r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func GenerateRecoveryCodes(count int) (codes, hashes []string) {
	codes = make([]string, 0, count)
	hashes = make([]string, 0, count)
	for i := 0; i < count; i++ {
		code := NewRecoveryCode()
		codes = append(codes, code)
		hashes = append(hashes, HashToken(NormalizeRecoveryCode(code)))
	}
	return codes, hashes
}

func NewSession(userID string, ttl time.Duration) (*store.Session, error) {
	now := time.Now().UTC()
	sess := &store.Session{
		ID:        NewToken(),
		UserID:    userID,
		CSRFToken: NewToken(),
		CreatedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(ttl).Format(time.RFC3339),
	}
	return sess, nil
}

func SessionCookieValue(id string, ttl time.Duration, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookie,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	}
}

func ClearSessionCookie(secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

func CSRFCookieValue(token string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     CSRFCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func ValidateUsername(v string) string {
	v = strings.TrimSpace(v)
	if !usernameRE.MatchString(v) {
		return "Username must be 3-24 characters, using letters, numbers, or underscores."
	}
	return ""
}

func ValidateEmail(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	addr, err := mail.ParseAddress(v)
	if err != nil || !strings.EqualFold(addr.Address, v) {
		return "Please enter a valid email address."
	}
	return ""
}

func ValidatePassword(v string) string {
	if len(v) < 8 {
		return "Password must be at least 8 characters."
	}
	if len(v) > 72 {
		return "Password must be 72 characters or fewer."
	}
	return ""
}

func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) < 7 || !strings.EqualFold(h[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}
