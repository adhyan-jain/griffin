package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/griffin/core/internal/hands"
	"github.com/griffin/core/internal/hands/linkedin"
	"github.com/griffin/core/internal/hands/whatsapp"
	"github.com/griffin/core/internal/provider/gemini"
)

// newLinkedInService returns the LinkedIn scan hand, or nil without a model key (nothing could be verified).
//
//	LINKEDIN_FROM_CHROME  the owner's Chrome profile whose LinkedIn login Griffin's browser borrows (default "Default";
//	                      "off" uses only the login in Griffin's own browser profile)
//	LINKEDIN_VISIBLE      "on" shows the browser window; the default is headless, nothing on screen
func newLinkedInService(logger *slog.Logger) *hands.LinkedInService {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		logger.Warn("LinkedIn scans are unavailable: set GEMINI_API_KEY so posts can be verified")
		return nil
	}
	from := envOr("LINKEDIN_FROM_CHROME", "Default")
	if from == "off" {
		from = ""
	}
	cfg := linkedin.LaptopConfig{BrowserPath: os.Getenv("WHATSAPP_BROWSER"), ProfileDir: browserProfileDir(), FromChrome: from, Visible: os.Getenv("LINKEDIN_VISIBLE") == "on"}
	return &hands.LinkedInService{Scan: linkedin.Scan{
		Laptop: func(ctx context.Context, workspace int) (linkedin.Page, func(), error) {
			return linkedin.OpenLaptop(ctx, cfg, workspace)
		},
		Phone:      whatsapp.ExecADB{Serial: os.Getenv("ANDROID_SERIAL")},
		Classifier: linkedin.Gemini{Client: &gemini.Client{APIKey: key, MaxOutputTokens: 10}},
		Extractor:  linkedin.GeminiExtractor{Client: &gemini.Client{APIKey: key, MaxOutputTokens: 400}},
	}}
}
