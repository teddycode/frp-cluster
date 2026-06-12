package control

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func NewTOTPSecret() (string, error) {
	var raw [20]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:]), "="), nil
}

func TOTPURI(issuer, account, secret string) string {
	if issuer == "" {
		issuer = "frp-cluster"
	}
	if account == "" {
		account = "admin"
	}
	label := url.PathEscape(issuer + ":" + account)
	values := url.Values{}
	values.Set("secret", normalizeTOTPSecret(secret))
	values.Set("issuer", issuer)
	values.Set("algorithm", "SHA1")
	values.Set("digits", "6")
	values.Set("period", "30")
	return "otpauth://totp/" + label + "?" + values.Encode()
}

func VerifyTOTP(secret, code string, now time.Time) bool {
	secret = normalizeTOTPSecret(secret)
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	counter := now.Unix() / 30
	for offset := int64(-1); offset <= 1; offset++ {
		if generateTOTP(secret, counter+offset) == code {
			return true
		}
	}
	return false
}

func GenerateTOTPForCLI(secret string, now time.Time) string {
	return generateTOTP(secret, now.Unix()/30)
}

func generateTOTP(secret string, counter int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalizeTOTPSecret(secret))
	if err != nil {
		return ""
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}

func normalizeTOTPSecret(secret string) string {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	secret = strings.ReplaceAll(secret, " ", "")
	secret = strings.TrimRight(secret, "=")
	return secret
}

func ParseTOTPCode(value string) (int, bool) {
	code, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || code < 0 || code > 999999 {
		return 0, false
	}
	return code, true
}
