package names

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"my-app":      "my-app",
		"my_app":      "my-app",
		"My App":      "my-app",
		"--x--":       "x",
		"app.v2":      "app-v2",
		"a__b..c":     "a-b-c",
		"Café":        "caf",
		"Projects123": "projects123",
	}
	for in, want := range cases {
		got, err := Normalize(in)
		if err != nil {
			t.Errorf("Normalize(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeEmpty(t *testing.T) {
	for _, in := range []string{"", "___", "...", "é"} {
		if _, err := Normalize(in); err != ErrEmpty {
			t.Errorf("Normalize(%q) err = %v, want ErrEmpty", in, err)
		}
	}
}

func TestNormalizeMaxLength(t *testing.T) {
	got, err := Normalize(strings.Repeat("a", 62) + "_b" + strings.Repeat("c", 10))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 63 || strings.HasSuffix(got, "-") {
		t.Errorf("got %q (len %d), want <= 63 chars without trailing hyphen", got, len(got))
	}
}

func TestDomains(t *testing.T) {
	if got := Domain("my-app"); got != "my-app.test" {
		t.Errorf("Domain = %q", got)
	}
	if got := ViteDomain("my-app"); got != "vite.my-app.test" {
		t.Errorf("ViteDomain = %q", got)
	}
}
