package service

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Ashby answers {"jobs":[...]}: the discovery service must read it through the ats adapter, not as a flat array.
func TestDiscoverCompanyJobsReadsAshbyThroughTheAdapter(t *testing.T) {
	body, err := os.ReadFile("../sources/ats/testdata/ashby.json")
	require.NoError(t, err)
	var asked string
	s := &JobDiscoveryService{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		asked = r.URL.String()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
	})}}
	got, err := s.DiscoverCompanyJobs(context.Background(), domain.SourceAshby, "Inkeep")
	require.NoError(t, err)
	require.Contains(t, asked, "api.ashbyhq.com/posting-api/job-board/inkeep")
	require.NotEmpty(t, got, "Ashby's jobs object must yield postings")
	require.NotEmpty(t, got[0].Title)
	require.Equal(t, domain.SourceAshby, got[0].Source)
}

func TestDiscoverCompanyJobsRejectsUnknownSourceAndFailedBoard(t *testing.T) {
	s := &JobDiscoveryService{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
	})}}
	_, err := s.DiscoverCompanyJobs(context.Background(), domain.SourceLinkedIn, "x")
	require.Error(t, err)
	_, err = s.DiscoverCompanyJobs(context.Background(), domain.SourceLever, "nope")
	require.Error(t, err, "a board that could not be read is an error, not zero jobs")
}
