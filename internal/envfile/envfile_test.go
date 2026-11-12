package envfile

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	in := `# comment
APP_NAME="My App"
APP_URL=https://my-app.test
APP_PORT=8080 # host port
export VITE_PORT=5174
SINGLE='quoted # not a comment'
EMPTY=
=novalue
garbage line
`
	env, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"APP_NAME":  "My App",
		"APP_URL":   "https://my-app.test",
		"APP_PORT":  "8080",
		"VITE_PORT": "5174",
		"SINGLE":    "quoted # not a comment",
		"EMPTY":     "",
	}
	if len(env) != len(want) {
		t.Errorf("got %d keys %v, want %d", len(env), env, len(want))
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
}
