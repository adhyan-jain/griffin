package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/sources/ats"
)

var (
	reInternMatch = regexp.MustCompile(`(?i)\b(intern|internship|fellow|co-op|new grad|campus|junior)\b`)
	reDomainMatch = regexp.MustCompile(`(?i)\b(software|systems|backend|infrastructure|distributed|ai|machine learning|ml|compiler|cloud|go|rust|python)\b`)
)

// ATSJobPosting models an ATS job item from Greenhouse, Lever, or Ashby.
type ATSJobPosting struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Company     string                 `json:"company"`
	URL         string                 `json:"url"`
	Description string                 `json:"description"`
	Location    string                 `json:"location"`
	Source      domain.DiscoverySource `json:"source"`
}

// JobDiscoveryService fetches live openings from authorized ATS endpoints and invokes the opportunity pipeline.
type JobDiscoveryService struct {
	DiscoveryRepo *repository.DiscoveryRepository
	Pipeline      *OpportunityPipeline
	Client        *http.Client
	Logger        *slog.Logger
	Now           func() time.Time

	// Fetcher can be overridden for hermetic unit testing
	CustomFetcher func(ctx context.Context, source domain.DiscoverySource, company string) ([]ATSJobPosting, error)
}

func (s *JobDiscoveryService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *JobDiscoveryService) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// adapter returns the ats adapter for a discovery source. The adapters own the parsing (Ashby's {"jobs":[...]}
// object, Greenhouse pagination and secondary locations, Lever's array), so none of it is repeated here.
func (s *JobDiscoveryService) adapter(source domain.DiscoverySource) (ats.Adapter, error) {
	switch source {
	case domain.SourceGreenhouse, domain.SourceLever, domain.SourceAshby:
	default:
		return nil, fmt.Errorf("unsupported discovery source: %s", source)
	}
	f := ats.NewFetcher()
	if s.Client != nil {
		f.Client = s.Client
	}
	a, ok := ats.Adapters(f)[string(source)]
	if !ok {
		return nil, fmt.Errorf("unsupported discovery source: %s", source)
	}
	return a, nil
}

// DiscoverCompanyJobs queries a target company board on an authorized ATS platform. A board that could not be read
// at all is an error; a partly read one returns what was read.
func (s *JobDiscoveryService) DiscoverCompanyJobs(ctx context.Context, source domain.DiscoverySource, company string) ([]ATSJobPosting, error) {
	if s.CustomFetcher != nil {
		return s.CustomFetcher(ctx, source, company)
	}
	a, err := s.adapter(source)
	if err != nil {
		return nil, err
	}
	scan := a.Read(ctx, strings.ToLower(company))
	if scan.Status == jobs.ScanFailed {
		return nil, fmt.Errorf("fetch %s postings for %s: %s", source, company, scan.Error)
	}
	out := make([]ATSJobPosting, 0, len(scan.Jobs))
	for _, j := range scan.Jobs {
		out = append(out, ATSJobPosting{
			ID:          j.ExternalID,
			Title:       j.Title,
			Company:     company,
			URL:         j.URL,
			Description: j.Description,
			Location:    strings.Join(j.Locations, "; "),
			Source:      source,
		})
	}
	return out, nil
}

// SweepAndStage runs discovery across target companies, verifies against profile, deduplicates, and stages matching opportunities.
func (s *JobDiscoveryService) SweepAndStage(ctx context.Context, userID uuid.UUID, targets []struct {
	Source  domain.DiscoverySource
	Company string
}) ([]*domain.DiscoveredJob, error) {
	var staged []*domain.DiscoveredJob

	for _, t := range targets {
		postings, err := s.DiscoverCompanyJobs(ctx, t.Source, t.Company)
		if err != nil {
			s.log().Warn("discovery sweep failed for target", "company", t.Company, "error", err)
			continue
		}

		for _, p := range postings {
			combined := p.Title + " " + p.Description
			// 1. Profile relevance filter (Internship / Systems / AI match)
			if !reInternMatch.MatchString(combined) || !reDomainMatch.MatchString(combined) {
				continue
			}

			// 2. Duplicate detection
			if s.DiscoveryRepo != nil {
				dup, err := s.DiscoveryRepo.IsJobDuplicate(ctx, userID, p.Company, p.Title)
				if err != nil {
					s.log().Warn("duplicate check error", "error", err)
				}
				if dup {
					continue
				}
			}

			// 3. Score opportunity & stage via OpportunityPipeline
			tierLabel := "Standard"
			var taskID *uuid.UUID

			if s.Pipeline != nil {
				opInput := OpportunityInput{
					Company:     p.Company,
					Role:        p.Title,
					URL:         p.URL,
					Description: p.Description,
				}
				isTierA, label := s.Pipeline.ScoreOpportunity(opInput)
				tierLabel = label

				if isTierA || label == "Tier A" {
					task, _, err := s.Pipeline.StageOpportunity(ctx, userID, opInput)
					if err == nil && task != nil {
						taskID = &task.ID
					} else if err != nil {
						s.log().Warn("failed to stage opportunity via pipeline", "error", err)
					}
				}
			}

			discJob := &domain.DiscoveredJob{
				UserID:      userID,
				Source:      t.Source,
				ExternalID:  p.ID,
				Company:     p.Company,
				Role:        p.Title,
				URL:         p.URL,
				Description: p.Description,
				Location:    p.Location,
				Tier:        tierLabel,
				Status:      "staged",
				TaskID:      taskID,
				CreatedAt:   s.now(),
			}

			if s.DiscoveryRepo != nil {
				if err := s.DiscoveryRepo.SaveDiscoveredJob(ctx, discJob); err != nil {
					s.log().Warn("failed to persist discovered job", "error", err)
				}
			}

			staged = append(staged, discJob)
		}
	}

	return staged, nil
}
