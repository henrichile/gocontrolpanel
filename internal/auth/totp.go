package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const totpStep = 30 * time.Second

// GenerateTOTPSecret crea un secreto nuevo de 20 bytes (160 bits, el tamaño
// recomendado por RFC 4226 para HMAC-SHA1) codificado en base32 sin padding,
// el formato que esperan las apps de autenticación.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// totpStepAt calcula el contador de paso (RFC 6238) para un instante dado.
func totpStepAt(t time.Time) int64 {
	return t.Unix() / int64(totpStep.Seconds())
}

// totpCodeAtStep implementa HOTP (RFC 4226) sobre el contador de paso dado.
func totpCodeAtStep(secret string, step int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(
		strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("secreto TOTP inválido: %w", err)
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(step))

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", code%1_000_000), nil
}

// ValidateTOTP comprueba un código de 6 dígitos contra el paso actual y el
// paso inmediatamente anterior (tolerancia de reloj de ±30s). Devuelve el
// número de paso que validó el código, para que el llamador lo guarde y
// rechace reintentos del mismo código (protección contra repetición).
func ValidateTOTP(secret, code string, now time.Time, lastUsedStep int64) (step int64, ok bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	current := totpStepAt(now)
	for _, s := range []int64{current, current - 1} {
		if s <= lastUsedStep {
			continue // ya se usó este paso (o uno posterior): no se puede repetir
		}
		expected, err := totpCodeAtStep(secret, s)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
			return s, true
		}
	}
	return 0, false
}

// ProvisioningURI arma la URL otpauth:// que las apps de autenticación leen
// (directamente o vía QR) para dar de alta la cuenta.
func ProvisioningURI(secret, issuer, account string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{
		"secret":    {secret},
		"issuer":    {issuer},
		"algorithm": {"SHA1"},
		"digits":    {"6"},
		"period":    {strconv.Itoa(int(totpStep.Seconds()))},
	}
	return "otpauth://totp/" + label + "?" + q.Encode()
}
