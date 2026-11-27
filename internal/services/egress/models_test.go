package egress

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func modelsServer(t *testing.T, fail *bool) *quotaStore {
	t.Helper()
	bodies := map[string]string{
		"/load": `{}`,
		"/agy": `{"models":{
			"b-high":{"displayName":"B (High)"},"a":{"displayName":"A"},
			"old-disabled":{"displayName":"Old","disabled":true},
			"tab_internal":{},"retired":{"displayName":"Retired"}},
			"agentModelSorts":[{"groups":[{"modelIds":["b-high","a","old-disabled","gone","a"]}]}]}`,
		"/claude": `{"data":[{"id":"claude-x","display_name":"Claude X"}]}`,
		"/codex":  `{"models":[{"slug":"gpt-x","display_name":"GPT X","visibility":"list"},{"slug":"review","visibility":"hide"}]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agy" {
			raw, err := io.ReadAll(r.Body)
			if err != nil || !strings.Contains(string(raw), antigravityConsumerProject) {
				t.Errorf("agy models body = %q (%v), want the consumer project", raw, err)
			}
		}
		if *fail && r.URL.Path != "/load" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if _, err := io.WriteString(w, bodies[r.URL.Path]); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return newQuotaStore(nil, nil, QuotaEndpoints{
		AntigravityLoad: srv.URL + "/load", AntigravityModels: srv.URL + "/agy",
		ClaudeModels: srv.URL + "/claude", CodexModels: srv.URL + "/codex",
	})
}

// A raw catalog names internal, retired, and disabled models; an account's
// list must be only what its CLI offers.
func TestModelsAreWhatTheCLIOffers(t *testing.T) {
	fail := false
	qs := modelsServer(t, &fail)
	for _, c := range []struct {
		binding string
		want    []Model
	}{
		{bindingAgy, []Model{{ID: "b-high", Name: "B (High)"}, {ID: "a", Name: "A"}}},
		{bindingClaude, []Model{{ID: "claude-x", Name: "Claude X"}}},
		{bindingCodex, []Model{{ID: "gpt-x", Name: "GPT X"}}},
	} {
		got, err := qs.fetchModels(t.Context(), c.binding, resolvedAccount{value: "tok"})
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s models = %+v (%v), want %+v", c.binding, got, err, c.want)
		}
	}
}

func TestAFailedListingKeepsTheLastModels(t *testing.T) {
	qs := newQuotaStore(nil, nil, QuotaEndpoints{})
	rec := AccountQuota{Binding: bindingAgy, Name: "a", Models: []Model{{ID: "m"}}}
	qs.putFetched(rec)
	for _, quotaErr := range []string{"", "HTTP 500"} {
		qs.putFetched(AccountQuota{Binding: bindingAgy, Name: "a", Error: quotaErr, ModelsError: "HTTP 500"})
		got := qs.snapshot(nil)
		if len(got) != 1 || len(got[0].Models) != 1 || got[0].ModelsError == "" {
			t.Errorf("quota error %q: record = %+v, want the old models and the error", quotaErr, got)
		}
	}
}
