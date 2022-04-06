package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	refTime = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	base36  = strings.Split("0123456789abcdefghijklmnopqrstuvwxyz", "")
)

var (
	ErrTokenExpired = errors.New("Token expired")
	ErrTokenInvalid = errors.New("Invalid token")
)

func intToBase36(i int64) (string, error) {
	if i < 0 {
		return "", fmt.Errorf("Negative base36 conversion input.")
	}
	if i < 36 {
		return base36[i], nil
	}
	b36 := ""
	var n int64
	for i != 0 {
		n = i % 36
		i = i / 36
		// i, n = divmod(i, 36)
		// b36 = string(char_set[n]) + b36
		b36 = base36[n] + b36
	}
	return b36, nil
}

func base36Toint(i string) (int64, error) {
	return strconv.ParseInt(i, 36, 64)
}

type TokenGenerator struct {
	key        string
	salt       string
	expiration time.Duration
}

func (t *TokenGenerator) tokenWithTimestamp(claims string, timestamp int64) (string, error) {
	h := hmac.New(sha256.New, []byte(t.key))
	h.Write([]byte(t.salt))
	data := fmt.Sprintf("%s%d", claims, timestamp)
	h.Write([]byte(data))
	encodedTimestamp, err := intToBase36(timestamp)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%x", encodedTimestamp, h.Sum(nil)), nil
	// return hex.EncodeToString(h.Sum(nil))
	// return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (t *TokenGenerator) GenerateToken(claims string) (string, error) {
	timestamp := time.Now().UTC().Unix() - refTime
	return t.tokenWithTimestamp(claims, timestamp)
}

func (t *TokenGenerator) CheckToken(token, claims string) error {
	parts := strings.Split(token, "-")
	encodedTimestamp := parts[0]
	timestamp, err := base36Toint(encodedTimestamp)
	if err != nil {
		return err
	}
	currentTimestamp := time.Now().UTC().Unix() - refTime
	if currentTimestamp-timestamp > int64(t.expiration) {
		return ErrTokenExpired
	}
	genToken, err := t.tokenWithTimestamp(claims, timestamp)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(token), []byte(genToken)) {
		return ErrTokenInvalid
	}
	return nil
}

func NewTokenGenerator(key, salt string, expiration time.Duration) *TokenGenerator {
	return &TokenGenerator{key, salt, expiration}
}
