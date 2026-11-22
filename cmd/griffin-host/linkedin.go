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

// newLinkedInService returns the LinkedIn hand. The Alumni people search always works (it needs only the signed-in
// laptop browser); scans need a model key, since nothing could be verified without one.
//
//	LINKEDIN_FROM_CHROME  the owner's Chrome profile whose LinkedIn login Griffin's browser borrows (default "Default";
//	                      "off" uses only the login in Griffin's own browser profile)
//	LINKEDIN_VISIBLE      "on" shows the browser window; the default is headless, nothing on screen
func newLinkedInService(logger *slog.Logger) *hands.LinkedInService {
	from := envOr("LINKEDIN_FROM_CHROME", "Default")
	if from == "off" {
		from = ""
	}
	cfg := linkedin.LaptopConfig{BrowserPath: os.Getenv("WHATSAPP_BROWSER"), ProfileDir: browserProfileDir(), FromChrome: from, Visible: os.Getenv("LINKEDIN_VISIBLE") == "on"}
	laptop := func(ctx context.Context, workspace int) (linkedin.Page, func(), error) {
		return linkedin.OpenLaptop(ctx, cfg, workspace)
	}
	svc := &hands.LinkedInService{People: linkedin.PeopleSearch{Open: laptop}}
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		logger.Warn("LinkedIn scans are unavailable: set GEMINI_API_KEY so posts can be verified (the Alumni people search still works)")
		return svc
	}
	svc.Scan = linkedin.Scan{
		Laptop:     laptop,
		Phone:      whatsapp.ExecADB{Serial: os.Getenv("ANDROID_SERIAL")},
		Classifier: linkedin.Gemini{Client: &gemini.Client{APIKey: key, MaxOutputTokens: 10}},
		Extractor:  linkedin.GeminiExtractor{Client: &gemini.Client{APIKey: key, MaxOutputTokens: 400}},
	}
	return svc
}
