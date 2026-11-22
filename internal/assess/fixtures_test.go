package assess

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/sources"
	"github.com/griffin/core/internal/sources/ats"
)

type fixtureTransport struct{ body []byte }

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(f.body)), Request: r}, nil
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "sources", "ats", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// atsFetcher serves one real fixture to every request.
func atsFetcher(t *testing.T, fixture string) *ats.Fetcher {
	return &ats.Fetcher{Client: &http.Client{Transport: fixtureTransport{readFixture(t, fixture)}}}
}

// atsJobs reads a real ATS fixture through the real adapter.
func atsJobs(t *testing.T, adapter ats.Adapter, token string) []jobs.RawJob {
	t.Helper()
	s := adapter.Read(context.Background(), token)
	if s.Status != jobs.ScanOK {
		t.Fatalf("fixture scan %s: %s", s.Status, s.Error)
	}
	return s.Jobs
}

// internshalaJobs reads the real Internshala page fixture through the real parser.
func internshalaJobs(t *testing.T) []jobs.RawJob {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "sources", "testdata", "internshala.html"))
	if err != nil {
		t.Fatal(err)
	}
	var out []jobs.RawJob
	for _, p := range sources.ParseInternshala(string(b)) {
		out = append(out, sources.RawJobFromPosting(p, p.Posted))
	}
	return out
}

// inputOf is what the routine builds from a stored listing.
func inputOf(j jobs.RawJob) Input {
	kind, ver := "aggregator", "lead"
	if j.SourceKind != jobs.Aggregator {
		kind, ver = string(j.SourceKind), "employer_verified"
	}
	return Input{Title: j.Title, Company: j.EmployerName, Location: strings.Join(j.Locations, "; "), Pay: j.Compensation, Description: j.Description,
		SourceKind: kind, Verification: ver, Key: "k:" + j.ExternalID + j.Title}
}
