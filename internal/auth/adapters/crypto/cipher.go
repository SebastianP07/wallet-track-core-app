package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	aesKeyBytes   = 32
	keyIDHexChars = 16
	keyIDLabel    = "wallet-track/totp-key-id\x00"
)

// Keyring encrypts with the current AES-256-GCM key and also decrypts with the previous one
// during a rotation. Each key's ID is derived from it, so no extra setting can drift out of step.
type Keyring struct {
	currentID string
	keys      map[string]cipher.AEAD
}

// NewKeyring takes the current key and, while rotating, the previous one (nil otherwise).
func NewKeyring(current, previous []byte) (*Keyring, error) {
	ring := &Keyring{keys: make(map[string]cipher.AEAD, 2)}
	id, err := ring.add(current)
	if err != nil {
		return nil, fmt.Errorf("current key: %w", err)
	}
	ring.currentID = id
	if previous != nil {
		if _, err := ring.add(previous); err != nil {
			return nil, fmt.Errorf("previous key: %w", err)
		}
	}
	return ring, nil
}

func (k *Keyring) add(key []byte) (string, error) {
	if len(key) != aesKeyBytes {
		return "", fmt.Errorf("want %d bytes, got %d", aesKeyBytes, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	sum := sha256.Sum256(append([]byte(keyIDLabel), key...))
	id := hex.EncodeToString(sum[:])[:keyIDHexChars]
	k.keys[id] = aead
	return id, nil
}

func (k *Keyring) CurrentKeyID() string {
	return k.currentID
}

// Encrypt returns nonce || ciphertext, authenticated against context and the key ID.
func (k *Keyring) Encrypt(plaintext []byte, context string) ([]byte, string, error) {
	aead := k.keys[k.currentID]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", fmt.Errorf("generate nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plaintext, additionalData(context, k.currentID)), k.currentID, nil
}

func (k *Keyring) Decrypt(ciphertext []byte, keyID, context string) ([]byte, error) {
	aead, ok := k.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("no key loaded with ID %s", keyID)
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, sealed := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, sealed, additionalData(context, keyID))
	if err != nil {
		return nil, fmt.Errorf("open ciphertext: %w", err)
	}
	return plaintext, nil
}

func additionalData(context, keyID string) []byte {
	return []byte(context + "\x00" + keyID)
}
