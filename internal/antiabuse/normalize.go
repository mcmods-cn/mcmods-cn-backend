package antiabuse

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"html"
	"math/bits"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	markupPattern = regexp.MustCompile(`(?s)<[^>]*>|\[[^\]]*\]\([^)]*\)|[*_~` + "`" + `>#]+`)
	urlPattern    = regexp.MustCompile(`https?://[^\s]+`)
	spacePattern  = regexp.MustCompile(`\s+`)
)

func NormalizeContent(value string) string {
	value = norm.NFKC.String(value)
	value = strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			return -1
		}
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, html.UnescapeString(value))
	value = urlPattern.ReplaceAllStringFunc(value, normalizeURL)
	value = markupPattern.ReplaceAllString(value, " ")
	return strings.TrimSpace(spacePattern.ReplaceAllString(value, " "))
}

func normalizeURL(raw string) string {
	parsed, err := url.Parse(strings.TrimRight(raw, ".,;:!?)]}"))
	if err != nil || parsed.Host == "" {
		return raw
	}
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func ContentHash(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func SimHash(normalized string) uint64 {
	runes := []rune(normalized)
	if len(runes) == 0 {
		return 0
	}
	features := make([]string, 0, len(runes))
	for index := range runes {
		end := min(index+3, len(runes))
		feature := strings.TrimSpace(string(runes[index:end]))
		if feature != "" {
			features = append(features, feature)
		}
	}
	sort.Strings(features)
	weights := [64]int{}
	for _, feature := range features {
		sum := sha256.Sum256([]byte(feature))
		value := binary.LittleEndian.Uint64(sum[:8])
		for bit := 0; bit < 64; bit++ {
			if value&(uint64(1)<<bit) != 0 {
				weights[bit]++
			} else {
				weights[bit]--
			}
		}
	}
	var result uint64
	for bit, weight := range weights {
		if weight >= 0 {
			result |= uint64(1) << bit
		}
	}
	return result
}

func Similarity(left, right uint64) int {
	return 1000 - bits.OnesCount64(left^right)*1000/64
}
