// Package security provides password hashing and JWT issuing/verification.
package security

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// dummyHash is a fixed bcrypt hash used to equalise response time on the
// "user not found" branch (defeats a timing oracle).
var dummyHash []byte

func init() {
	dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-equalizer"), bcrypt.DefaultCost)
}

// bcrypt accepts at most 72 bytes; trim the rest like the former Python code.
func toBytes(password string) []byte {
	b := []byte(password)
	if len(b) > 72 {
		b = b[:72]
	}
	return b
}

// HashPassword returns a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword(toBytes(password), bcrypt.DefaultCost)
	return string(h), err
}

// VerifyPassword reports whether password matches the stored hash.
func VerifyPassword(password, hashed string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashed), toBytes(password)) == nil
}

// DummyVerify performs a throwaway bcrypt comparison to equalise timing.
func DummyVerify() {
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte("x"))
}

// Auth issues and validates access tokens.
type Auth struct {
	secret []byte
	ttl    time.Duration
}

// New creates an Auth with the given HS256 secret and token lifetime (minutes).
func New(secret string, expireMinutes int) *Auth {
	return &Auth{secret: []byte(secret), ttl: time.Duration(expireMinutes) * time.Minute}
}

// CreateToken issues a signed JWT for subject with the given token version.
func (a *Auth) CreateToken(subject string, tokenVersion int) (string, error) {
	claims := jwt.MapClaims{
		"sub": subject,
		"ver": tokenVersion,
		"exp": time.Now().UTC().Add(a.ttl).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(a.secret)
}

// Claims carries the validated token subject and version.
type Claims struct {
	Subject string
	Version int
}

// DecodeToken validates a token and returns its claims, or ok=false.
func (a *Auth) DecodeToken(tokenStr string) (Claims, bool) {
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return a.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !tok.Valid {
		return Claims{}, false
	}
	mc, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, false
	}
	sub, _ := mc["sub"].(string)
	if sub == "" {
		return Claims{}, false
	}
	ver := 0
	if v, ok := mc["ver"].(float64); ok {
		ver = int(v)
	}
	return Claims{Subject: sub, Version: ver}, true
}
