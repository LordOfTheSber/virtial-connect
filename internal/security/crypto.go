package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/user"
)

func deriveKey() ([]byte, error) {
	h, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(h + ":" + u.Uid + ":virtial-connect"))
	return sum[:], nil
}

func EncryptString(plain string) (string, error) {
	key, err := deriveKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	cipherText := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(cipherText), nil
}

func DecryptString(cipherText string) (string, error) {
	if cipherText == "" {
		return "", nil
	}
	key, err := deriveKey()
	if err != nil {
		return "", err
	}
	blob, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(blob) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid encrypted payload")
	}
	nonce, cipherPart := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, cipherPart, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
