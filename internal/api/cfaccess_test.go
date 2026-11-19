package api

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	testIssuer   = "https://team.cloudflareaccess.com"
	testAudience = "4714c1358e65fe4b408ad6d432a5f878f08194bdb4752441fd56faefa9b2b6f2"
)

// RSA keys take a while to generate: two for the whole package. testKey
// plays the team's key, otherKey anyone else's.
var (
	testKey  = sync.OnceValue(newRSAKey)
	otherKey = sync.OnceValue(newRSAKey)
)

func newRSAKey() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
}

// testVerifier checks tokens as relocd does, but against testKey instead of
// the keys it would fetch from the team domain.
func testVerifier() *oidc.IDTokenVerifier {
	keys := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&testKey().PublicKey}}
	return oidc.NewVerifier(testIssuer, keys, &oidc.Config{ClientID: testAudience})
}

// accessClaims is the part of an Access JWT payload that the check reads.
type accessClaims struct {
	Issuer   string   `json:"iss"`
	Audience []string `json:"aud"`
	IssuedAt int64    `json:"iat"`
	Expiry   int64    `json:"exp"`
	Email    string   `json:"email"`
}

// validClaims are what Access puts into a token for this application.
func validClaims() accessClaims {
	now := time.Now()
	return accessClaims{
		Issuer:   testIssuer,
		Audience: []string{testAudience},
		IssuedAt: now.Unix(),
		Expiry:   now.Add(time.Hour).Unix(),
		Email:    "me@example.com",
	}
}

// signJWT makes a token the way Access does: header.payload.signature, each
// base64url, the signature RS256 over "header.payload". A nil key leaves
// the signature empty, as "alg": "none" does.
func signJWT(t *testing.T, key *rsa.PrivateKey, alg string, c accessClaims) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": alg, "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding
	signed := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	if key == nil {
		return signed + "."
	}
	sum := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signed + "." + enc.EncodeToString(sig)
}

// passed answers for whatever is behind the check, with a status nothing
// else uses.
var passed = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusTeapot)
})

func serveWithJWT(h http.Handler, path, jwt string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if jwt != "" {
		r.Header.Set(cfAccessHeader, jwt)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCFAccessValid(t *testing.T) {
	h := requireCFAccess(testVerifier(), passed)
	w := serveWithJWT(h, "/", signJWT(t, testKey(), "RS256", validClaims()))
	if w.Code != http.StatusTeapot {
		t.Errorf("status = %d, want the request to pass; body: %s", w.Code, w.Body)
	}
}

func TestCFAccessOff(t *testing.T) {
	h := requireCFAccess(nil, passed)
	if w := serveWithJWT(h, "/", ""); w.Code != http.StatusTeapot {
		t.Errorf("status = %d, want the request to pass", w.Code)
	}
}

func TestCFAccessRefused(t *testing.T) {
	claims := func(change func(*accessClaims)) accessClaims {
		c := validClaims()
		change(&c)
		return c
	}
	// the payload of one token with the signature of another: the signature
	// covers the payload, so no field can be changed after signing
	tampered := func() string {
		good := strings.Split(signJWT(t, testKey(), "RS256", validClaims()), ".")
		evil := strings.Split(signJWT(t, otherKey(), "RS256", claims(func(c *accessClaims) {
			c.Email = "evil@example.com"
		})), ".")
		return good[0] + "." + evil[1] + "." + good[2]
	}

	tests := []struct {
		name string
		jwt  string
	}{
		{"no header", ""},
		{"not a jwt", "not-a-jwt"},
		{"signed by another key", signJWT(t, otherKey(), "RS256", validClaims())},
		{"not signed", signJWT(t, nil, "none", validClaims())},
		{"tampered payload", tampered()},
		{"another application", signJWT(t, testKey(), "RS256", claims(func(c *accessClaims) {
			c.Audience = []string{"another-application"}
		}))},
		{"another team", signJWT(t, testKey(), "RS256", claims(func(c *accessClaims) {
			c.Issuer = "https://another.cloudflareaccess.com"
		}))},
		{"expired", signJWT(t, testKey(), "RS256", claims(func(c *accessClaims) {
			c.IssuedAt = time.Now().Add(-2 * time.Hour).Unix()
			c.Expiry = time.Now().Add(-time.Hour).Unix()
		}))},
	}
	h := requireCFAccess(testVerifier(), passed)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, serveWithJWT(h, "/", tt.jwt), http.StatusUnauthorized, "unauthorized")
		})
	}
}

// The reason of a refusal is logged, the token never: it is a credential.
func TestCFAccessLog(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	jwt := signJWT(t, otherKey(), "RS256", validClaims())
	serveWithJWT(requireCFAccess(testVerifier(), passed), "/", jwt)

	log := buf.String()
	if !strings.Contains(log, "cf access: token rejected") {
		t.Errorf("log lacks the refusal: %s", log)
	}
	for _, part := range strings.Split(jwt, ".") {
		if strings.Contains(log, part) {
			t.Errorf("log contains a part of the token: %s", log)
		}
	}
}

// What New puts behind Access: the API and the UI, not the probes.
func TestCFAccessRoutes(t *testing.T) {
	h := New(nil, testToken, testVerifier(), fakeUI)
	jwt := signJWT(t, testKey(), "RS256", validClaims())

	t.Run("probe without jwt", func(t *testing.T) {
		if w := serveWithJWT(h, "/livez", ""); w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200: the kubelet has no jwt", w.Code)
		}
	})
	t.Run("ui without jwt", func(t *testing.T) {
		wantError(t, serveWithJWT(h, "/", ""), http.StatusUnauthorized, "unauthorized")
	})
	t.Run("ui with jwt", func(t *testing.T) {
		if w := serveWithJWT(h, "/", jwt); w.Code != http.StatusTeapot {
			t.Errorf("status = %d, want the UI", w.Code)
		}
	})
	// the token alone is not enough...
	t.Run("api with token, without jwt", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, clientsPath(1), nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		wantError(t, w, http.StatusUnauthorized, "unauthorized")
		// refused by the Access check, which, unlike the token check,
		// names no scheme
		if got := w.Header().Get("WWW-Authenticate"); got != "" {
			t.Errorf("WWW-Authenticate = %q, want none", got)
		}
	})
	// ...and neither is the jwt alone
	t.Run("api with jwt, without token", func(t *testing.T) {
		w := serveWithJWT(h, clientsPath(1), jwt)
		wantError(t, w, http.StatusUnauthorized, "unauthorized")
		if got := w.Header().Get("WWW-Authenticate"); got != "Bearer" {
			t.Errorf("WWW-Authenticate = %q, want Bearer", got)
		}
	})
}
