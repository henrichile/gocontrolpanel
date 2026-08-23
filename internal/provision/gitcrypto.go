package provision

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// generateDeployKey crea un par de claves ed25519 nuevo para que un sitio se
// autentique contra su repositorio Git — el panel nunca reutiliza la misma
// clave entre sitios ni le pide al usuario que suba una.
func generateDeployKey() (privatePEM []byte, publicAuthorizedKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("generando clave ed25519: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, "", fmt.Errorf("serializando clave privada: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, "", fmt.Errorf("derivando clave pública: %w", err)
	}
	return pem.EncodeToMemory(block), string(ssh.MarshalAuthorizedKey(sshPub)), nil
}

// deriveEncryptionKey obtiene la clave AES-256 usada para cifrar las claves
// privadas de deploy en la base de datos, a partir del secreto de firma de
// JWT ya configurado (config.go exige ≥32 caracteres) — así no hace falta
// una variable de entorno nueva solo para esto.
func deriveEncryptionKey(jwtSecret string) [32]byte {
	return sha256.Sum256([]byte("gocp-git-deploy-key:" + jwtSecret))
}

func encryptPrivateKey(jwtSecret string, plaintext []byte) ([]byte, error) {
	key := deriveEncryptionKey(jwtSecret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decryptPrivateKey(jwtSecret string, ciphertext []byte) ([]byte, error) {
	key := deriveEncryptionKey(jwtSecret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("clave cifrada inválida")
	}
	nonce, data := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, data, nil)
}

// randomWebhookSecret genera el secreto compartido para verificar la firma
// (GitHub) o el token (GitLab) del webhook de push.
func randomWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}
