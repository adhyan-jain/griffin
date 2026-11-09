// Package vault holds what the owner's documents need before they reach the cloud: encryption on the
// device, so S3 only ever stores ciphertext for sensitive files (PAN, Aadhaar, passport and the like), and
// the rules for what may be uploaded. The web app implements the same format with the browser's WebCrypto
// (web/src/lib/vault-crypto.ts); a shared known-answer test keeps the two in step.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// Format: "GVLT1" | iterations (uint32, big endian) | salt (16) | nonce (12) | AES-256-GCM ciphertext+tag.
// The key is PBKDF2-HMAC-SHA256 of the owner's passphrase. The header is authenticated, so changing the
// iteration count or salt makes decryption fail instead of silently weakening it. PBKDF2 is used because
// browsers have it built in; scrypt and Argon2 are not available there.
const (
	Magic             = "GVLT1"
	saltLen           = 16
	nonceLen          = 12
	headerLen         = len(Magic) + 4 + saltLen + nonceLen
	DefaultIterations = 600_000 // OWASP's 2023 guidance for PBKDF2-HMAC-SHA256
	minIterations     = 1_000
	maxIterations     = 10_000_000
)

var (
	ErrNotEncrypted = errors.New("vault: this is not an encrypted vault file")
	// ErrDecrypt covers a wrong passphrase and a damaged file alike; telling them apart would help a guesser.
	ErrDecrypt = errors.New("vault: could not decrypt (wrong passphrase, or the file is damaged)")
)

// IsEncrypted reports whether data starts with the vault header.
func IsEncrypted(data []byte) bool {
	return len(data) >= headerLen+16 && string(data[:len(Magic)]) == Magic
}

// Encrypt seals plaintext with a key derived from passphrase, using fresh random salt and nonce.
func Encrypt(passphrase string, plaintext []byte) ([]byte, error) {
	salt := make([]byte, saltLen)
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return EncryptWith(passphrase, plaintext, salt, nonce, DefaultIterations)
}

// EncryptWith is Encrypt with a caller-chosen salt, nonce and iteration count. It exists so the format can
// be checked against fixed test vectors; real files must always use Encrypt, because reusing a nonce with
// one key breaks AES-GCM.
func EncryptWith(passphrase string, plaintext, salt, nonce []byte, iterations int) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("vault: a passphrase is required")
	}
	if len(salt) != saltLen || len(nonce) != nonceLen {
		return nil, errors.New("vault: bad salt or nonce length")
	}
	if iterations < minIterations || iterations > maxIterations {
		return nil, fmt.Errorf("vault: iterations must be between %d and %d", minIterations, maxIterations)
	}
	aead, err := newAEAD(passphrase, salt, iterations)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 0, headerLen)
	header = append(header, Magic...)
	header = binary.BigEndian.AppendUint32(header, uint32(iterations))
	header = append(header, salt...)
	header = append(header, nonce...)
	// The header up to the nonce is the associated data; the nonce itself is authenticated by being the nonce.
	return aead.Seal(header, nonce, plaintext, header[:len(header)-nonceLen]), nil
}

// Decrypt opens a file made by Encrypt.
func Decrypt(passphrase string, data []byte) ([]byte, error) {
	if !IsEncrypted(data) {
		return nil, ErrNotEncrypted
	}
	iterations := int(binary.BigEndian.Uint32(data[len(Magic) : len(Magic)+4]))
	if iterations < minIterations || iterations > maxIterations {
		return nil, ErrDecrypt // refuse a header that asks for a trivial or absurd cost
	}
	salt := data[len(Magic)+4 : len(Magic)+4+saltLen]
	nonce := data[len(Magic)+4+saltLen : headerLen]
	aead, err := newAEAD(passphrase, salt, iterations)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nonce, data[headerLen:], data[:headerLen-nonceLen])
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}

func newAEAD(passphrase string, salt []byte, iterations int) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, passphrase, salt, iterations, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
