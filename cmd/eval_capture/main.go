// Command eval_capture reads a small, fixed set of public employer boards and the aggregator pages with the real
// adapters and writes every posting it saw, untouched, with the URL it was read from and the day it was fetched.
// It is how the benchmark's postings were collected; the benchmark itself (cmd/eval_opportunities) never touches
// the network. Public endpoints only, one request per second per host.
//
//	go run ./cmd/eval_capture -out /tmp/capture.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/sources"
	"github.com/griffin/core/internal/sources/ats"
)

// board is one employer board the benchmark reads (a subset of the verified seeds).
type board struct{ Provider, Token string }

var boards = []board{
	{"greenhouse", "razorpaysoftwareprivatelimited"}, {"greenhouse", "databricks"}, {"greenhouse", "stripe"},
	{"greenhouse", "cloudflare"}, {"greenhouse", "datadog"}, {"greenhouse", "airbnb"}, {"greenhouse", "coinbase"},
	{"greenhouse", "figma"}, {"greenhouse", "anthropic"}, {"greenhouse", "scaleai"}, {"greenhouse", "mongodb"},
	{"greenhouse", "rubrik"}, {"greenhouse", "gitlab"}, {"greenhouse", "reddit"},
	{"lever", "meesho"}, {"lever", "cred"}, {"lever", "paytm"}, {"lever", "palantir"}, {"lever", "spotify"},
	{"ashby", "openai"}, {"ashby", "notion"}, {"ashby", "ramp"}, {"ashby", "snowflake"}, {"ashby", "sarvam"}, {"ashby", "supabase"},
}

// Captured is one posting with where and when it was read.
type Captured struct {
	SourceURL string      `json:"source_url"`
	Provider  string      `json:"provider"`
	Target    string      `json:"target"`
	Fetched   string      `json:"fetched_date"`
	Job       jobs.RawJob `json:"job"`
}

func sourceURL(provider, target string) string {
	switch provider {
	case "greenhouse":
		return "https://boards-api.greenhouse.io/v1/boards/" + url.PathEscape(target) + "/jobs?content=true"
	case "lever":
		return "https://api.lever.co/v0/postings/" + url.PathEscape(target) + "?mode=json"
	case "ashby":
		return "https://api.ashbyhq.com/posting-api/job-board/" + url.PathEscape(target)
	case "internshala":
		return "https://internshala.com" + target
	case "unstop":
		return "https://unstop.com/api/public/opportunity/search-result?opportunity=internships&per_page=50&page=1&searchTerm=" + url.QueryEscape(target)
	case "interndock":
		return "https://www.interndock.com" + target
	}
	return ""
}

func main() {
	out := flag.String("out", "capture.json", "where to write the capture")
	flag.Parse()
	ctx := context.Background()
	ad := ats.Adapters(ats.NewFetcher())

	type target struct{ provider, token string }
	var targets []target
	for _, b := range boards {
		targets = append(targets, target{b.Provider, b.Token})
	}
	for _, p := range sources.InternshalaPages {
		targets = append(targets, target{"internshala", p})
	}
	for _, q := range sources.UnstopSearches {
		targets = append(targets, target{"unstop", q})
	}
	for _, p := range []string{"/internships/india", "/internships/fields/software-engineering", "/internships/fields/data-science-ml", "/internships/remote"} {
		targets = append(targets, target{"interndock", p})
	}

	var all []Captured
	for _, t := range targets {
		sc := ad[t.provider].Read(ctx, t.token)
		fmt.Fprintf(os.Stderr, "%-12s %-40s %-8s %4d jobs %s\n", t.provider, t.token, sc.Status, len(sc.Jobs), sc.Error)
		if sc.Status != jobs.ScanOK {
			continue // a partial or failed read is not a complete board; the benchmark only keeps complete reads
		}
		day := time.Now().UTC().Format("2006-01-02")
		for _, j := range sc.Jobs {
			all = append(all, Captured{SourceURL: sourceURL(t.provider, t.token), Provider: t.provider, Target: t.token, Fetched: day, Job: j})
		}
	}
	b, err := json.MarshalIndent(all, "", " ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %d postings to %s\n", len(all), *out)
}
