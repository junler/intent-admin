package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Session expiry lives in Redis. Omitting a JWT exp claim is deliberate: the
// documented refresh operation extends the online session without replacing
// its signed token. A token is never accepted without its live Redis session.
type tokenClaims struct {
	UUID     string `json:"uuid"`
	Issuer   string `json:"iss"`
	IssuedAt int64  `json:"iat"`
}

var errToken = errors.New("invalid session token")

func signToken(secret, issuer, id string, now time.Time) (string, error) {
	payload, err := json.Marshal(tokenClaims{UUID: id, Issuer: issuer, IssuedAt: now.Unix()})
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	data := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(data))
	return data + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func parseToken(secret, issuer, token string, now time.Time) (string, error) {
	if len(token) > 4096 {
		return "", errToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", errToken
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", errToken
	}
	rawHeader, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errToken
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if json.Unmarshal(rawHeader, &header) != nil || header.Alg != "HS256" || header.Typ != "JWT" {
		return "", errToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errToken
	}
	var claims tokenClaims
	if json.Unmarshal(payload, &claims) != nil || claims.Issuer != issuer || claims.IssuedAt <= 0 || claims.IssuedAt > now.Add(time.Minute).Unix() {
		return "", errToken
	}
	if _, err := uuid.Parse(claims.UUID); err != nil {
		return "", errToken
	}
	return claims.UUID, nil
}
