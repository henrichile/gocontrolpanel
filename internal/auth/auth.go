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

// Purpose distingue un access token normal (vacío, o "access") de un ticket
// intermedio de 2FA ("totp") — así un ticket pendiente de verificar el
// código nunca puede colarse como sesión válida en requireAuth.
type Claims struct {
	UserID   uuid.UUID   `json:"uid"`
	Username string      `json:"usr"`
	Role     models.Role `json:"rol"`
	Purpose  string      `json:"pur,omitempty"`
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
	claims, err := t.parseClaims(tokenStr)
	if err != nil {
		return nil, err
	}
	// Un ticket de 2FA pendiente ("totp") nunca debe servir como token de
	// acceso normal, aunque esté firmado correctamente y no haya expirado.
	if claims.Purpose != "" && claims.Purpose != "access" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// totpTicketTTL es deliberadamente corto: el ticket solo vive lo que tarda
// el usuario en escribir el código de su app de autenticación.
const totpTicketTTL = 5 * time.Minute

// IssueTOTPTicket emite un token intermedio tras validar usuario+contraseña,
// pendiente de que el cliente confirme el código TOTP.
func (t *TokenIssuer) IssueTOTPTicket(u *models.User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: u.ID, Username: u.Username, Role: u.Role, Purpose: "totp",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID.String(),
			Issuer:    t.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(totpTicketTTL)),
			ID:        uuid.NewString(),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}

// ParseTOTPTicket valida un ticket emitido por IssueTOTPTicket; rechaza
// cualquier otro tipo de token, incluido un access token normal.
func (t *TokenIssuer) ParseTOTPTicket(tokenStr string) (*Claims, error) {
	claims, err := t.parseClaims(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.Purpose != "totp" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func (t *TokenIssuer) parseClaims(tokenStr string) (*Claims, error) {
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
