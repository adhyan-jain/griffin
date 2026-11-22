package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/sources/ats"
)

// OpportunityScanTitle is the routine that reads the employers' own boards for internships, in code.
const OpportunityScanTitle = domain.OpportunityScanTitle

// IsOpportunityScan reports whether a task is that routine.
func IsOpportunityScan(title string) bool {
	return strings.Contains(strings.ToUpper(title), OpportunityScanTitle)
}

// OpportunityStore is what the scan needs from the database: everything ingest needs, plus the owner's boards,
// companies and scan history.
type OpportunityStore interface {
	IngestStore
	ListBoards(ctx context.Context, userID uuid.UUID) ([]*domain.ATSBoard, error)
	ListCompanies(ctx context.Context, userID uuid.UUID, statuses []string) ([]*domain.Company, error)
	SourceHealth(ctx context.Context, userID uuid.UUID) ([]domain.SourceHealth, error)
}

// Defaults of one scan. A full pass over ~100 boards does not fit one request, so a run reads boards oldest-first
// until its time budget is spent and the next run carries on.
const (
	DefaultScanBudget  = 20 * time.Second // a host claim runs on Lambda behind a ~29s gateway: reads end by 20s, nothing new starts in the last 6s
	scanReserve        = 6 * time.Second  // no new board, careers page or seed is started when less than this remains of the budget
	defaultScanWorkers = 4
	defaultMaxProbes   = 4                  // companies whose careers pages are tried per run
	brokenRetryAfter   = 7 * 24 * time.Hour // a broken board is tried again this often, so one that recovers is found
	probeSource        = "careers_probe"
	listNames          = 12 // names shown in a summary before "and N more"
)

// OpportunityScan is the routine that keeps the owner's internship leads true to the employers' own boards. It reads
// every active ATS board with the adapters, keeps the postings that could be internships or early-career roles
// (ats.IsEarlyCareer, stage 1 of matching), ingests each board's scan, and tries the careers pages of companies that
// only an aggregator has named. No model is involved. It is idempotent: running it twice changes nothing the second
// time beyond when boards were last read.
type OpportunityScan struct {
	Store OpportunityStore
	// Adapters read the boards; nil means the real adapters over one shared, per-host throttled fetcher.
	Adapters map[string]ats.Adapter
	// Fetcher reads careers pages; nil means a new one. Use the one the adapters share so the host throttle holds.
	Fetcher *ats.Fetcher
	// Seeds are the verified employers inserted on the first run; nil means the embedded seed list.
	Seeds []ats.Seed
	// Budget is the hard time limit of a run's network reads (DefaultScanBudget when zero): every read carries a
	// deadline at that point, and no new board, seed or careers page is started in the last scanReserve of it.
	Budget    time.Duration
	Workers   int // boards read at once (4 when zero)
	MaxProbes int // careers pages tried per run for aggregator-led companies (4 when zero, negative for none)
	Now       func() time.Time
}

// OpportunityScanResult counts what one run did.
type OpportunityScanResult struct {
	Seeded        int // boards inserted from the seed list on this run
	SeedsLeft     int // seed boards not yet inserted because the time budget ran out; the next run adds them
	Boards        int // active boards due for a read at the start
	Read          int // boards read (ok, partial or failed)
	OK            int
	Partial       int
	Failed        int
	Skipped       int      // boards whose read was cut off by the budget: nothing was recorded, they stay due
	Remaining     int      // boards not reached before the budget ended
	NowBroken     []string // boards that reached the failure limit on this run
	Postings      int      // postings the boards listed
	EarlyCareer   int      // of those, how many could be internships or early-career roles
	New           []string // "Role at Company" for each newly seen employer-verified posting
	Changed       int
	Closed        int
	Reopened      int
	Merged        int
	FailedBoards  []string
	PartialBoards []string
	Probed        int      // careers pages tried for aggregator-led companies
	Expanded      []string // companies that gained a verified board from a careers page
	Problems      []string
}

func (s *OpportunityScan) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *OpportunityScan) adapters() map[string]ats.Adapter {
	if s.Adapters == nil {
		s.Adapters = ats.Adapters(s.fetcher())
	}
	return s.Adapters
}

func (s *OpportunityScan) fetcher() *ats.Fetcher {
	if s.Fetcher == nil {
		s.Fetcher = ats.NewFetcher()
	}
	return s.Fetcher
}

func (s *OpportunityScan) seeds() []ats.Seed {
	if s.Seeds != nil {
		return s.Seeds
	}
	return ats.Seeds().Seeds
}

// boardKey identifies a board the way the table does: provider and token (tokens compare case-insensitively here, so
// a seed never duplicates a board that differs only in case).
func boardKey(provider, token string) string { return provider + "|" + strings.ToLower(token) }

// ---- seeding ----------------------------------------------------------------------------------------------------

// seed inserts the verified seed employers and boards the owner does not have yet. A company the owner ignored is not
// brought back, and a board that is already there (active, broken or retired) is left as it is.
func (s *OpportunityScan) seed(ctx context.Context, userID uuid.UUID, boards []*domain.ATSBoard, companies []*domain.Company, stop func() bool) ([]*domain.ATSBoard, []*domain.Company, int, int, []string) {
	have := map[string]bool{}
	for _, b := range boards {
		have[boardKey(b.Provider, b.TokenOrURL)] = true
	}
	byName := map[string]*domain.Company{}
	for _, c := range companies {
		byName[c.NormalizedName] = c
	}
	var added, left int
	var problems []string
	for _, sd := range s.seeds() {
		if have[boardKey(sd.Provider, sd.Token)] {
			continue
		}
		key := domain.NormalizeEmployer(sd.Name)
		c := byName[key]
		if c != nil && c.Status == domain.CompanyIgnored {
			continue
		}
		if stop() {
			left++ // the time budget is spent: the rest are seeded by the next run
			continue
		}
		if c == nil {
			c = &domain.Company{UserID: userID, Name: sd.Name, NormalizedName: key, Kind: sd.Kind, DiscoveredVia: "seed", Status: domain.CompanyActive}
			if d := strings.ToLower(strings.TrimSpace(sd.Domain)); d != "" {
				c.Domain = &d
			}
			if u := (ats.BoardRef{Provider: sd.Provider, Token: sd.Token}).PageURL(); u != "" {
				c.EvidenceURL = &u
			}
		} else {
			// a company already known (an aggregator named it) gains an employer board: it is the employer's own, so it is active
			c = &domain.Company{UserID: userID, Name: c.Name, NormalizedName: c.NormalizedName, Domain: c.Domain, Kind: c.Kind, DiscoveredVia: c.DiscoveredVia, EvidenceURL: c.EvidenceURL, Status: domain.CompanyActive}
			if c.Domain == nil && strings.TrimSpace(sd.Domain) != "" {
				d := strings.ToLower(strings.TrimSpace(sd.Domain))
				c.Domain = &d
			}
			if u := (ats.BoardRef{Provider: sd.Provider, Token: sd.Token}).PageURL(); u != "" && c.EvidenceURL == nil {
				c.EvidenceURL = &u
			}
		}
		if err := s.Store.UpsertCompany(ctx, c); err != nil {
			problems = append(problems, "could not save "+sd.Name+": "+err.Error())
			continue
		}
		b := &domain.ATSBoard{UserID: userID, CompanyID: &c.ID, Provider: sd.Provider, TokenOrURL: sd.Token}
		if err := s.Store.UpsertBoard(ctx, b); err != nil {
			problems = append(problems, "could not save the "+sd.Name+" board: "+err.Error())
			continue
		}
		byName[key] = c
		have[boardKey(sd.Provider, sd.Token)] = true
		boards = append(boards, b)
		companies = appendCompany(companies, c)
		added++
	}
	return boards, companies, added, left, problems
}

func appendCompany(list []*domain.Company, c *domain.Company) []*domain.Company {
	for i, x := range list {
		if x.ID == c.ID {
			list[i] = c
			return list
		}
	}
	return append(list, c)
}

// ---- the run ----------------------------------------------------------------------------------------------------

// due returns the boards to read, oldest read first. Boards never read come first; a retired board is never read; a
// broken one only once a week.
func due(boards []*domain.ATSBoard, now time.Time) []*domain.ATSBoard {
	var out []*domain.ATSBoard
	for _, b := range boards {
		switch b.Status {
		case domain.BoardActive:
			out = append(out, b)
		case domain.BoardBroken:
			if b.LastScanAt == nil || now.Sub(*b.LastScanAt) >= brokenRetryAfter {
				out = append(out, b)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.LastScanAt == nil) != (b.LastScanAt == nil) {
			return a.LastScanAt == nil
		}
		if a.LastScanAt != nil && !a.LastScanAt.Equal(*b.LastScanAt) {
			return a.LastScanAt.Before(*b.LastScanAt)
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.TokenOrURL < b.TokenOrURL
	})
	return out
}

// Run reads the owner's boards for one budget.
func (s *OpportunityScan) Run(ctx context.Context, userID uuid.UUID) (OpportunityScanResult, error) {
	var res OpportunityScanResult
	if s == nil || s.Store == nil {
		return res, fmt.Errorf("opportunity scan: no store")
	}
	started := s.now()
	budget := s.Budget
	if budget <= 0 {
		budget = DefaultScanBudget
	}
	boards, err := s.Store.ListBoards(ctx, userID)
	if err != nil {
		return res, err
	}
	companies, err := s.Store.ListCompanies(ctx, userID, nil)
	if err != nil {
		return res, err
	}
	// Every read ends by the budget, whatever the board does: the host claim that runs this has a hard limit.
	runCtx, cancelRun := context.WithTimeout(ctx, budget)
	defer cancelRun()
	stopStarting := func() bool { return runCtx.Err() != nil || s.now().Sub(started) >= budget-scanReserve }

	var problems []string
	boards, companies, res.Seeded, res.SeedsLeft, problems = s.seed(ctx, userID, boards, companies, stopStarting)
	res.Problems = append(res.Problems, problems...)

	byID := map[uuid.UUID]*domain.Company{}
	for _, c := range companies {
		byID[c.ID] = c
	}
	queue := due(boards, started)
	res.Boards = len(queue)

	ing := &Ingester{Store: s.Store, Now: s.Now}
	var (
		mu    sync.Mutex // guards res, next and the ingest (stores are not assumed to be safe for concurrent writes)
		next  int
		reads sync.WaitGroup
	)
	workers := s.Workers
	if workers <= 0 {
		workers = defaultScanWorkers
	}
	adapters := s.adapters()
	for w := 0; w < workers; w++ {
		reads.Add(1)
		go func() {
			defer reads.Done()
			for {
				mu.Lock()
				if next >= len(queue) || stopStarting() {
					mu.Unlock()
					return
				}
				b := queue[next]
				next++
				mu.Unlock()

				ad := adapters[b.Provider]
				var scan jobs.Scan
				if ad == nil {
					scan = jobs.Scan{Source: b.Provider, Target: b.TokenOrURL, Status: jobs.ScanFailed, Error: "no adapter for " + b.Provider}
				} else {
					scan = ad.Read(runCtx, b.TokenOrURL)
				}
				cutOff := runCtx.Err() != nil && scan.Status != jobs.ScanOK

				mu.Lock()
				if cutOff {
					// our own deadline stopped the read, not the board: it is not a failure and is not recorded
					res.Skipped++
					mu.Unlock()
					continue
				}
				s.ingest(ctx, ing, userID, b, byID, scan, &res)
				mu.Unlock()
			}
		}()
	}
	reads.Wait()
	res.Remaining = len(queue) - next + res.Skipped
	if res.Remaining < 0 {
		res.Remaining = 0
	}

	if s.MaxProbes >= 0 && !stopStarting() {
		s.probe(ctx, runCtx, userID, stopStarting, &res)
	}
	return res, nil
}

// ingest filters one board's scan to the postings that could be internships or early-career roles and ingests it. The
// scan's status is kept as it is, so a partial or failed read closes nothing.
func (s *OpportunityScan) ingest(ctx context.Context, ing *Ingester, userID uuid.UUID, b *domain.ATSBoard, byID map[uuid.UUID]*domain.Company, scan jobs.Scan, res *OpportunityScanResult) {
	var co *domain.Company
	if b.CompanyID != nil {
		co = byID[*b.CompanyID]
	}
	label := b.Provider + " " + b.TokenOrURL
	if co != nil {
		label = co.Name + " (" + b.Provider + " " + b.TokenOrURL + ")"
	}
	res.Read++
	switch scan.Status {
	case jobs.ScanOK:
		res.OK++
	case jobs.ScanPartial:
		res.Partial++
		res.PartialBoards = append(res.PartialBoards, label+": "+scan.Error)
	default:
		res.Failed++
		res.FailedBoards = append(res.FailedBoards, label+": "+scan.Error)
		if b.ConsecutiveFailures+1 >= domain.BoardBrokenAfter {
			res.NowBroken = append(res.NowBroken, label)
		}
	}
	res.Postings += len(scan.Jobs)
	kept := ats.FilterEarlyCareer(scan.Jobs)
	res.EarlyCareer += len(kept)
	// A board is one employer's: the company it was stored under names every posting on it, so the same employer is
	// not split by how a provider spells it (Lever and Ashby give no name at all, and an unnamed posting cannot be ingested).
	for i := range kept {
		if co != nil {
			kept[i].EmployerName = co.Name
			if co.Domain != nil && kept[i].EmployerDomain == "" {
				kept[i].EmployerDomain = *co.Domain
			}
		}
	}
	scan.Jobs = kept
	out, err := ing.Ingest(ctx, userID, scan)
	if err != nil {
		res.Problems = append(res.Problems, label+": "+err.Error())
		return
	}
	res.Problems = append(res.Problems, out.Problems...)
	for _, l := range out.Created {
		if l.Verification == domain.VerificationEmployer {
			res.New = append(res.New, l.Role+" at "+l.Company)
		}
	}
	res.Changed += out.Changed
	res.Closed += out.Closed
	res.Reopened += out.Reopened
	res.Merged += out.Merged
}

// ---- expansion --------------------------------------------------------------------------------------------------

// probe tries the careers pages of companies an aggregator named and that have a known domain: https://<domain>/careers
// and, when that yields no board, /jobs. Each page is tried once, ever: the try is recorded in the scan history and a
// company with a recorded try is not asked again. The pages are read through the shared throttled fetcher.
func (s *OpportunityScan) probe(ctx, runCtx context.Context, userID uuid.UUID, stop func() bool, res *OpportunityScanResult) {
	max := s.MaxProbes
	if max == 0 {
		max = defaultMaxProbes
	}
	health, err := s.Store.SourceHealth(ctx, userID)
	if err != nil {
		res.Problems = append(res.Problems, "could not read the scan history: "+err.Error())
		return
	}
	tried := map[string]bool{}
	for _, h := range health {
		if h.Source == probeSource {
			tried[h.Target] = true
		}
	}
	candidates, err := s.Store.ListCompanies(ctx, userID, []string{domain.CompanyCandidate})
	if err != nil {
		res.Problems = append(res.Problems, "could not list the candidate companies: "+err.Error())
		return
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
	for _, c := range candidates {
		if res.Probed >= max || stop() {
			return
		}
		if c.Domain == nil {
			continue
		}
		urls := ats.CareersURLs(*c.Domain)
		var fresh []string
		for _, u := range urls {
			if !tried[u] {
				fresh = append(fresh, u)
			}
		}
		if len(fresh) == 0 {
			continue
		}
		res.Probed++
		for _, u := range fresh {
			d := ats.Discover(runCtx, s.fetcher(), s.adapters(), u)
			if runCtx.Err() != nil && len(d.Verified) == 0 {
				return // the budget stopped the read, not the company's site: it is not recorded as tried
			}
			tried[u] = true
			s.recordProbe(ctx, userID, d)
			if len(d.Verified) == 0 {
				continue
			}
			names, err := s.adopt(ctx, userID, c.Name, derefStr(c.Domain), "careers_page", d)
			if err != nil {
				res.Problems = append(res.Problems, c.Name+": "+err.Error())
				break
			}
			res.Expanded = append(res.Expanded, c.Name+" ("+strings.Join(names, ", ")+")")
			break // /careers gave the boards: /jobs is not needed
		}
	}
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// recordProbe stores one careers-page try in the scan history. A page that was read is an ok scan whether or not it
// named a board; one that could not be read is a failed scan with its error.
func (s *OpportunityScan) recordProbe(ctx context.Context, userID uuid.UUID, d ats.Discovery) {
	at := s.now().UTC()
	sc := domain.SourceScan{UserID: userID, Source: probeSource, Target: d.PageURL, Status: string(jobs.ScanOK), JobsSeen: len(d.Verified), Pages: 1, StartedAt: at, FinishedAt: at}
	if !d.Fetched {
		sc.Status, sc.Pages, sc.Error = string(jobs.ScanFailed), 0, d.PageErr
	}
	_ = s.Store.RecordScan(ctx, &sc)
}

// ExpandResult is what adopting one careers page did.
type ExpandResult struct {
	Discovery ats.Discovery
	Boards    []string // "provider token" of each board stored
	Created   []*domain.Listing
}

// ExpandFromCareersPage reads a company's careers page, finds its ATS boards, verifies each with its adapter and stores
// the company and the boards that read ok (discovered via "careers_page", the page as evidence). Boards that could not
// be read are reported in the result and never stored. The postings read while verifying are ingested at once.
func (s *OpportunityScan) ExpandFromCareersPage(ctx context.Context, userID uuid.UUID, name, domainName, pageURL string) (ExpandResult, error) {
	var r ExpandResult
	if s == nil || s.Store == nil {
		return r, fmt.Errorf("opportunity scan: no store")
	}
	if strings.TrimSpace(name) == "" {
		return r, fmt.Errorf("a company name is needed: a board belongs to one employer")
	}
	r.Discovery = ats.Discover(ctx, s.fetcher(), s.adapters(), pageURL)
	s.recordProbe(ctx, userID, r.Discovery)
	if len(r.Discovery.Verified) == 0 {
		return r, nil
	}
	names, err := s.adopt(ctx, userID, name, domainName, "careers_page", r.Discovery)
	r.Boards = names
	return r, err
}

// adopt stores a discovery's verified boards under the company and ingests the scans that verified them. The company
// is active when a board yielded at least one posting, otherwise it stays a candidate until one does.
func (s *OpportunityScan) adopt(ctx context.Context, userID uuid.UUID, name, domainName, via string, d ats.Discovery) ([]string, error) {
	postings := 0
	for _, v := range d.Verified {
		postings += len(v.Scan.Jobs)
	}
	c := &domain.Company{UserID: userID, Name: strings.TrimSpace(name), NormalizedName: domain.NormalizeEmployer(name), Kind: "unknown", DiscoveredVia: via, EvidenceURL: &d.PageURL, Status: domain.CompanyCandidate}
	if postings > 0 {
		c.Status = domain.CompanyActive
	}
	if dn := strings.ToLower(strings.TrimSpace(domainName)); dn != "" {
		c.Domain = &dn
	}
	if err := s.Store.UpsertCompany(ctx, c); err != nil {
		return nil, err
	}
	ing := &Ingester{Store: s.Store, Now: s.Now}
	var names []string
	for _, v := range d.Verified {
		b := &domain.ATSBoard{UserID: userID, CompanyID: &c.ID, Provider: v.Ref.Provider, TokenOrURL: v.Ref.Token}
		if err := s.Store.UpsertBoard(ctx, b); err != nil {
			return names, err
		}
		names = append(names, v.Ref.Provider+" "+v.Ref.Token)
		scan := v.Scan
		kept := ats.FilterEarlyCareer(scan.Jobs)
		for i := range kept {
			kept[i].EmployerName = c.Name
			if c.Domain != nil && kept[i].EmployerDomain == "" {
				kept[i].EmployerDomain = *c.Domain
			}
		}
		scan.Jobs = kept
		if _, err := ing.Ingest(ctx, userID, scan); err != nil {
			return names, err
		}
	}
	return names, nil
}

// ---- summary ----------------------------------------------------------------------------------------------------

func capList(items []string) string {
	if len(items) <= listNames {
		return strings.Join(items, "; ")
	}
	return strings.Join(items[:listNames], "; ") + fmt.Sprintf("; and %d more", len(items)-listNames)
}

// Summary is the owner-facing text for a run.
func (r OpportunityScanResult) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Opportunity scan: read %d of %d employer boards (%d read fully, %d partly, %d could not be read).", r.Read, r.Boards, r.OK, r.Partial, r.Failed)
	if r.Seeded > 0 {
		fmt.Fprintf(&b, " Added %d verified employer boards to watch on this first pass.", r.Seeded)
	}
	if r.SeedsLeft > 0 {
		fmt.Fprintf(&b, " %d more verified employers are added on the next run.", r.SeedsLeft)
	}
	if r.Remaining > 0 {
		fmt.Fprintf(&b, " %d boards are left for the next run; the oldest-read are always taken first.", r.Remaining)
	} else if r.Boards > 0 {
		b.WriteString(" Every due board was read.")
	}
	fmt.Fprintf(&b, "\n\nThe boards listed %d postings; %d could be internships or early-career roles. %d are new employer-verified internships, %d changed, %d closed because the employer took them down", r.Postings, r.EarlyCareer, len(r.New), r.Changed, r.Closed)
	if r.Reopened > 0 {
		fmt.Fprintf(&b, ", %d reopened", r.Reopened)
	}
	b.WriteString(".")
	if len(r.New) > 0 {
		b.WriteString("\n\nNew: " + capList(r.New) + ".")
	} else if r.OK+r.Partial > 0 {
		b.WriteString("\n\nNothing new on the boards read this time. Every posting here comes from the employer's own board, so an empty run means they listed nothing new that fits.")
	}
	if len(r.Expanded) > 0 {
		b.WriteString("\n\nFound employer boards on careers pages: " + capList(r.Expanded) + ".")
	}
	if r.Probed > 0 {
		fmt.Fprintf(&b, " Tried the careers pages of %d companies that only an aggregator had named.", r.Probed)
	}
	if len(r.NowBroken) > 0 {
		b.WriteString("\n\nMarked broken after repeated failures: " + capList(r.NowBroken) + ".")
	}
	if len(r.FailedBoards) > 0 {
		b.WriteString("\n\nCould not read: " + capList(r.FailedBoards) + ". A board that cannot be read never closes anything.")
	}
	if len(r.PartialBoards) > 0 {
		b.WriteString("\n\nRead only partly, so nothing was closed: " + capList(r.PartialBoards) + ".")
	}
	if len(r.Problems) > 0 {
		b.WriteString("\n\nProblems: " + capList(r.Problems) + ".")
	}
	return b.String()
}

// ScanOpportunities runs the routine for its task, in code. handled is false for any other task. A run that cannot
// start is answered with the reason, never handed to a model: the routine comes round again on its cadence.
func (w *TrackerLists) ScanOpportunities(ctx context.Context, task *domain.Task) (string, bool) {
	if w == nil || w.Opportunities == nil || task == nil || !IsOpportunityScan(task.Title) {
		return "", false
	}
	res, err := w.Opportunities.Run(ctx, task.UserID)
	if err != nil {
		return "Opportunity scan could not run: " + err.Error(), true
	}
	return res.Summary(), true
}
