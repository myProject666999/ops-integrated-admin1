package utils

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func ToBool(v interface{}) bool {
	switch b := v.(type) {
	case bool:
		return b
	case float64:
		return b != 0
	case int:
		return b != 0
	case string:
		t := strings.ToLower(strings.TrimSpace(b))
		return t == "1" || t == "true" || t == "yes"
	default:
		return false
	}
}

func ToBoolDefault(v interface{}, def bool) bool {
	if v == nil {
		return def
	}
	return ToBool(v)
}

func NormalizeGarbledText(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return raw
	}
	if !LooksLikeMojibake(text) {
		return raw
	}
	gbkBytes, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(text))
	if err != nil || !utf8.Valid(gbkBytes) {
		return raw
	}
	fixed := string(gbkBytes)
	if strings.TrimSpace(fixed) == "" {
		return raw
	}
	if strings.ContainsRune(fixed, utf8.RuneError) {
		return raw
	}
	if MojibakeScore(fixed) >= MojibakeScore(text) {
		return raw
	}
	return fixed
}

func LooksLikeMojibake(text string) bool {
	return strings.ContainsRune(text, utf8.RuneError)
}

func MojibakeScore(text string) int {
	return strings.Count(text, string(utf8.RuneError))
}
