// Package auth implementa hashing de contraseñas, emisión/validación de JWT
// y el contexto de identidad que viaja por los handlers.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/etasoft/gocontrolpanel/internal/models"
)

var (
	ErrInvalidToken   = errors.New("token inválido o expirado")
	ErrInvalidLogin   = errors.New("usuario o contraseña incorrectos")
	ErrAccountLocked  = errors.New("la cuenta está desactivada")
	ErrForbidden      = errors.New("no tienes permisos para esta operación")
	ErrPasswordPolicy = errors.New("la contraseña debe tener al menos 10 caracteres")
)

// --- Contraseñas -----------------------------------------------------------

func HashPassword(plain string, cost int) (string, error) {
	if len(plain) < 10 {
		return "", ErrPasswordPolicy
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// RandomPassword genera una contraseña legible y suficientemente entrópica,
// útil para credenciales de BD y FTP creadas por el panel.
func RandomPassword(n int) (string, error) {
	if n < 16 {
		n = 16
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf)[:n], nil
}

// --- Tokens ----------------------------------------------------------------

type Claims struct {
	UserID   uuid.UUID   `json:"uid"`
	Username string      `json:"usr"`
	Role     models.Role `json:"rol"`
	jwt.RegisteredClaims
}

type TokenIssuer struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	issuer     string
}

func NewTokenIssuer(secret string, accessTTL, refreshTTL time.Duration) *TokenIssuer {
	return &TokenIssuer{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		issuer:     "gocontrolpanel",
	}
}

func (t *TokenIssuer) AccessTTL() time.Duration  { return t.accessTTL }
func (t *TokenIssuer) RefreshTTL() time.Duration { return t.refreshTTL }

func (t *TokenIssuer) IssueAccess(u *models.User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   u.ID,
		Username: u.Username,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID.String(),
			Issuer:    t.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(now.Add(t.accessTTL)),
			ID:        uuid.NewString(),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}

func (t *TokenIssuer) Parse(tokenStr string) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(tokenStr, &Claims{},
		func(tk *jwt.Token) (any, error) {
			if _, ok := tk.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("algoritmo de firma inesperado: %v", tk.Header["alg"])
			}
			return t.secret, nil
		},
		jwt.WithIssuer(t.issuer),
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err != nil || !tok.Valid {
		return nil, ErrInvalidToken
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// NewRefreshToken devuelve el token en claro (para el cliente) y su hash
// SHA-256 (lo único que se guarda en la base de datos).
func NewRefreshToken() (plain, hashed string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	plain = base64.RawURLEncoding.EncodeToString(buf)
	return plain, HashRefresh(plain), nil
}

func HashRefresh(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// --- Identidad en el contexto ----------------------------------------------

type ctxKey struct{}

// Identity es el usuario autenticado de la petición en curso.
type Identity struct {
	UserID   uuid.UUID
	Username string
	Role     models.Role
}

func (i Identity) IsAdmin() bool    { return i.Role == models.RoleAdmin }
func (i Identity) IsReseller() bool { return i.Role == models.RoleReseller }

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// MustIdentity devuelve la identidad asumiendo que el middleware de
// autenticación ya se ejecutó.
func MustIdentity(ctx context.Context) Identity {
	id, ok := FromContext(ctx)
	if !ok {
		panic("auth: no hay identidad en el contexto; ¿falta el middleware RequireAuth?")
	}
	return id
}
