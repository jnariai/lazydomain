package reconcile

import (
	"errors"
	"strings"
	"testing"

	"github.com/jnariai/lazydomain/internal/state"
)

func TestCheckReportsDrift(t *testing.T) {
	site := state.Site{Label: "gone", Dir: t.TempDir(), AppPort: 1, VitePort: 2}

	healthy := Observed{
		ProxyHosts: map[string]bool{"gone.test": true, "vite.gone.test": true},
		HostsFile:  map[string]bool{"gone.test": true, "vite.gone.test": true},
	}
	st := Check(site, healthy)
	if !st.InProxy || !st.InHosts {
		t.Errorf("expected proxy and hosts present: %+v", st)
	}
	if len(st.Problems) != 1 || !strings.Contains(st.Problems[0], "folder") {
		t.Errorf("problems = %v", st.Problems)
	}

	broken := Observed{ProxyErr: errors.New("down"), HostsFile: map[string]bool{}}
	st = Check(site, broken)
	joined := strings.Join(st.Problems, "; ")
	for _, want := range []string{"proxy unreachable", "hosts entry missing"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems %q missing %q", joined, want)
		}
	}
}
