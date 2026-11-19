package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
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

func (s *JobDiscoveryService) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// DiscoverCompanyJobs queries a target company board on an authorized ATS platform.
func (s *JobDiscoveryService) DiscoverCompanyJobs(ctx context.Context, source domain.DiscoverySource, company string) ([]ATSJobPosting, error) {
	if s.CustomFetcher != nil {
		return s.CustomFetcher(ctx, source, company)
	}

	var fetchURL string
	switch source {
	case domain.SourceGreenhouse:
		fetchURL = fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs", strings.ToLower(company))
	case domain.SourceLever:
		fetchURL = fmt.Sprintf("https://api.lever.co/v0/postings/%s?mode=json", strings.ToLower(company))
	case domain.SourceAshby:
		fetchURL = fmt.Sprintf("https://api.ashbyhq.com/posting-api/job-board/%s", strings.ToLower(company))
	default:
		return nil, fmt.Errorf("unsupported discovery source: %s", source)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create discovery request: %w", err)
	}

	resp, err := s.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s postings for %s: %w", source, company, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream discovery returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read discovery response: %w", err)
	}

	return s.parsePostings(source, company, body)
}

func (s *JobDiscoveryService) parsePostings(source domain.DiscoverySource, company string, data []byte) ([]ATSJobPosting, error) {
	var out []ATSJobPosting

	switch source {
	case domain.SourceGreenhouse:
		var gh struct {
			Jobs []struct {
				ID          int    `json:"id"`
				Title       string `json:"title"`
				AbsoluteURL string `json:"absolute_url"`
				Location    struct {
					Name string `json:"name"`
				} `json:"location"`
			} `json:"jobs"`
		}
		if err := json.Unmarshal(data, &gh); err == nil {
			for _, j := range gh.Jobs {
				out = append(out, ATSJobPosting{
					ID:       fmt.Sprintf("%d", j.ID),
					Title:    j.Title,
					Company:  company,
					URL:      j.AbsoluteURL,
					Location: j.Location.Name,
					Source:   source,
				})
			}
		}

	case domain.SourceLever:
		var lev []struct {
			ID          string `json:"id"`
			Text        string `json:"text"`
			HostedURL   string `json:"hostedUrl"`
			Description string `json:"description"`
			Categories  struct {
				Location string `json:"location"`
			} `json:"categories"`
		}
		if err := json.Unmarshal(data, &lev); err == nil {
			for _, j := range lev {
				out = append(out, ATSJobPosting{
					ID:          j.ID,
					Title:       j.Text,
					Company:     company,
					URL:         j.HostedURL,
					Description: j.Description,
					Location:    j.Categories.Location,
					Source:      source,
				})
			}
		}

	default:
		// Generic or mock array
		_ = json.Unmarshal(data, &out)
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
