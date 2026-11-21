package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckPosting(t *testing.T) {
	long := strings.Repeat("We build databases and love our customers. ", 60)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone":
			w.WriteHeader(http.StatusNotFound)
		case "/open":
			_, _ = w.Write([]byte("<html><body><h1>Software Engineering Intern, Summer 2027</h1>" + long + "</body></html>"))
		case "/noroles":
			_, _ = w.Write([]byte("<html><body><script>var intern=1</script>" + long + "</body></html>"))
		default: // a script-built page: almost no text without a browser
			_, _ = w.Write([]byte("<html><body><div id=root></div></body></html>"))
		}
	}))
	defer srv.Close()
	for path, want := range map[string]PostingState{"/gone": PostingMissing, "/open": PostingOpen, "/noroles": PostingMissing, "/app": PostingUnknown} {
		if got := CheckPosting(context.Background(), srv.Client(), srv.URL+path); got != want {
			t.Errorf("%s: got %s, want %s", path, got, want)
		}
	}
	if CheckPosting(context.Background(), nil, "not a link") != PostingUnknown {
		t.Error("no link is unknown")
	}
}
