package names

import (
	"errors"
	"strings"
)

const TLD = "test"

const maxLabel = 63

var ErrEmpty = errors.New("name is empty after normalization")

func Normalize(raw string) (string, error) {
	var b strings.Builder
	lastHyphen := true
	for _, r := range strings.ToLower(raw) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if len(s) > maxLabel {
		s = strings.TrimRight(s[:maxLabel], "-")
	}
	if s == "" {
		return "", ErrEmpty
	}
	return s, nil
}

func Domain(label string) string { return label + "." + TLD }

func ViteDomain(label string) string { return "vite." + Domain(label) }
