package signedlink

import (
	"strings"
	"unicode"
)

// SanitizeName cleans the link's name segment for Content-Disposition. The
// segment is outside the signature, so it is untrusted: control
// characters, path separators and quotes are removed. An empty result
// falls back to the target's stored name, cleaned the same way and cut to
// its last path element (ADR 0010 rule 13).
func SanitizeName(name, storedName string) string {
	if cleaned := clean(name); cleaned != "" {
		return cleaned
	}
	if i := strings.LastIndexAny(storedName, `/\`); i >= 0 {
		storedName = storedName[i+1:]
	}
	if cleaned := clean(storedName); cleaned != "" {
		return cleaned
	}
	return "download"
}

func clean(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\"'`, r) {
			return -1
		}
		return r
	}, name)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "." || cleaned == ".." {
		return ""
	}
	return cleaned
}
