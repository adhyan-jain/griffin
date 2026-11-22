// Command verify_seeds reads every candidate company's boards live, with the same adapters the scan uses, and writes
// the seed file with only the boards that read ok and whose postings name the company. Run it by hand when the
// candidate list changes; it only reads public ATS endpoints and writes nothing to any database.
//
//	go run ./cmd/verify_seeds -in cmd/verify_seeds/candidates.txt -out internal/sources/ats/seeds.json -log /tmp/verify.txt
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/griffin/core/internal/sources/ats"
)

func parse(path string) ([]ats.Candidate, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []ats.Candidate
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 5 && len(parts) != 6 {
			return nil, fmt.Errorf("%s:%d: want name | domain | kind | region | provider:token, ... [| needs=word;word]", path, n)
		}
		c := ats.Candidate{Name: strings.TrimSpace(parts[0]), Domain: strings.TrimSpace(parts[1]), Kind: strings.TrimSpace(parts[2]), Region: strings.TrimSpace(parts[3])}
		if len(parts) == 6 {
			needs, ok := strings.CutPrefix(strings.TrimSpace(parts[5]), "needs=")
			if !ok {
				return nil, fmt.Errorf("%s:%d: the sixth field is needs=word;word, not %q", path, n, parts[5])
			}
			for _, w := range strings.Split(needs, ";") {
				if w = strings.TrimSpace(w); w != "" {
					c.Needs = append(c.Needs, w)
				}
			}
		}
		for _, t := range strings.Split(parts[4], ",") {
			p, tok, ok := strings.Cut(strings.TrimSpace(t), ":")
			if !ok || p == "" || tok == "" {
				return nil, fmt.Errorf("%s:%d: %q is not provider:token", path, n, t)
			}
			c.Tries = append(c.Tries, ats.Try{Provider: p, Token: tok})
		}
		out = append(out, c)
	}
	return out, sc.Err()
}

func main() {
	in := flag.String("in", "cmd/verify_seeds/candidates.txt", "candidate list")
	out := flag.String("out", "internal/sources/ats/seeds.json", "seed file to write")
	logPath := flag.String("log", "", "write the per-try record here")
	workers := flag.Int("workers", 8, "companies read at once (each host is still throttled)")
	only := flag.String("only", "", "verify only candidates whose name contains this")
	flag.Parse()

	cands, err := parse(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *only != "" {
		var keep []ats.Candidate
		for _, c := range cands {
			if strings.Contains(strings.ToLower(c.Name), strings.ToLower(*only)) {
				keep = append(keep, c)
			}
		}
		cands = keep
	}
	day := time.Now().UTC().Format("2006-01-02")
	// A big board is several MB and a slow link needs minutes for it: this tool waits longer than the scan does.
	f := ats.NewFetcher()
	f.Client.Timeout = 5 * time.Minute
	f.MaxRetries = 1
	adapters := ats.Adapters(f)

	type result struct {
		c    ats.Candidate
		seed ats.Seed
		ok   bool
		outs []ats.Outcome
	}
	results := make([]result, len(cands))
	var wg sync.WaitGroup
	var done atomic.Int32
	sem := make(chan struct{}, *workers)
	for i, c := range cands {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c ats.Candidate) {
			defer wg.Done()
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
			defer cancel()
			s, ok, outs := ats.VerifyCandidate(ctx, adapters, c, day)
			results[i] = result{c, s, ok, outs}
			verdict := "DROPPED"
			if ok {
				verdict = "kept " + s.Provider + ":" + s.Token
			}
			fmt.Fprintf(os.Stderr, "[%d/%d] %s: %s\n", int(done.Add(1)), len(cands), c.Name, verdict)
		}(i, c)
	}
	wg.Wait()

	var seeds []ats.Seed
	var rec strings.Builder
	var dropped []string
	for _, r := range results {
		fmt.Fprintf(&rec, "%s\n", r.c.Name)
		for _, o := range r.outs {
			fmt.Fprintf(&rec, "    %s:%s -> %s, %d postings, identity %q %s\n", o.Provider, o.Token, o.Status, o.Jobs, o.Identity, o.Note)
		}
		if r.ok {
			seeds = append(seeds, r.seed)
			fmt.Fprintf(&rec, "    KEPT %s:%s (%s)\n", r.seed.Provider, r.seed.Token, r.seed.Identity)
		} else {
			dropped = append(dropped, r.c.Name)
			fmt.Fprintf(&rec, "    DROPPED\n")
		}
	}
	region := map[string]int{"india": 0, "remote": 1, "global": 2}
	sort.SliceStable(seeds, func(i, j int) bool {
		if region[seeds[i].Region] != region[seeds[j].Region] {
			return region[seeds[i].Region] < region[seeds[j].Region]
		}
		return strings.ToLower(seeds[i].Name) < strings.ToLower(seeds[j].Name)
	})
	file := ats.SeedFile{Version: 1, VerifiedOn: day, Seeds: seeds}
	data, err := ats.MarshalSeeds(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := ats.ParseSeeds(data); err != nil {
		fmt.Fprintln(os.Stderr, "the result is not a valid seed file:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *logPath != "" {
		_ = os.WriteFile(*logPath, []byte(rec.String()), 0o644)
	}
	fmt.Printf("verified %d of %d candidates on %s\ndropped (%d): %s\n", len(seeds), len(cands), day, len(dropped), strings.Join(dropped, ", "))
}
