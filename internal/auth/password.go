package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

const (
	// hashIterations — число итераций PBKDF2-SHA256. Подобрано так, чтобы вход на слабом роутере
	// занимал доли секунды; перебор дополнительно ограничен лимитом попыток.
	hashIterations = 200_000

	hashSaltSize = 16
	hashKeySize  = 32

	// hashScheme — префикс формата хэша: pbkdf2-sha256$<итерации>$<соль>$<ключ>.
	hashScheme = "pbkdf2-sha256"
)

// HashPassword возвращает хэш пароля со случайной солью.
func HashPassword(password string) (string, error) {
	salt := make([]byte, hashSaltSize)

	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("cannot generate salt: %w", err)
	}

	key, err := pbkdf2.Key(sha256.New, password, salt, hashIterations, hashKeySize)
	if err != nil {
		return "", fmt.Errorf("cannot hash password: %w", err)
	}

	encoding := base64.RawStdEncoding

	return strings.Join([]string{hashScheme, strconv.Itoa(hashIterations), encoding.EncodeToString(salt), encoding.EncodeToString(key)}, "$"), nil
}

// VerifyPassword проверяет пароль по хэшу из HashPassword.
func VerifyPassword(password, hash string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return false
	}

	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}

	encoding := base64.RawStdEncoding

	salt, err := encoding.DecodeString(parts[2])
	if err != nil {
		return false
	}

	want, err := encoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}

	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}

	return subtle.ConstantTimeCompare(got, want) == 1
}
