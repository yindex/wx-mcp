package wx

import (
	"crypto/aes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// EncryptAESECB encrypts plaintext with AES-128-ECB + PKCS7 padding.
func EncryptAESECB(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes new cipher: %w", err)
	}
	bs := block.BlockSize()

	// PKCS7 padding.
	pad := bs - len(plaintext)%bs
	padded := make([]byte, len(plaintext)+pad)
	copy(padded, plaintext)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(pad)
	}

	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += bs {
		block.Encrypt(out[i:i+bs], padded[i:i+bs])
	}
	return out, nil
}

// DecryptAESECB decrypts AES-128-ECB ciphertext with PKCS7 padding.
func DecryptAESECB(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes new cipher: %w", err)
	}
	bs := block.BlockSize()
	if len(ciphertext)%bs != 0 {
		return nil, fmt.Errorf("ciphertext len %d not multiple of block size %d", len(ciphertext), bs)
	}

	out := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += bs {
		block.Decrypt(out[i:i+bs], ciphertext[i:i+bs])
	}

	// Strip PKCS7 padding.
	if len(out) == 0 {
		return out, nil
	}
	pad := int(out[len(out)-1])
	if pad == 0 || pad > bs {
		return nil, fmt.Errorf("invalid PKCS7 padding value: %d", pad)
	}
	return out[:len(out)-pad], nil
}

// AESECBPaddedSize returns the ciphertext byte count for a given plaintext size.
func AESECBPaddedSize(n int) int {
	return ((n / 16) + 1) * 16
}

// NewAESKey generates a cryptographically random 16-byte AES key.
func NewAESKey() ([]byte, error) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// ParseAESKey converts a CDNMedia.aes_key (base64-encoded) to a raw 16-byte key.
//
// Two encodings exist in the wild:
//   - base64(raw 16 bytes)            — images
//   - base64(32-char hex of 16 bytes) — file / voice / video
func ParseAESKey(aesKeyBase64 string) ([]byte, error) {
	decoded, err := decodeBase64Any(aesKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("base64 decode aes_key: %w", err)
	}
	if len(decoded) == 16 {
		return decoded, nil
	}
	// Try hex interpretation.
	if len(decoded) == 32 {
		raw, err := hex.DecodeString(string(decoded))
		if err == nil && len(raw) == 16 {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("aes_key must be 16 raw bytes or 32-char hex, got %d bytes", len(decoded))
}

func decodeBase64Any(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}
