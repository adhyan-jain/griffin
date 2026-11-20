// linkedin_scroll scrolls the LinkedIn app on the attached phone and saves the job-like posts it saw.
// It only reads. Usage: go run ./cmd/linkedin_scroll [-minutes 10] [-out file.json]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/griffin/core/internal/hands/linkedin"
	"github.com/griffin/core/internal/hands/whatsapp"
	"github.com/griffin/core/internal/provider/gemini"
)

func main() {
	minutes := flag.Float64("minutes", 10, "how long to scroll")
	home, _ := os.UserHomeDir()
	out := flag.String("out", filepath.Join(home, "Documents", "Griffin", "internships", "linkedin_posts.json"), "where to save the posts")
	flag.Parse()

	model := &gemini.Client{APIKey: os.Getenv("GEMINI_API_KEY"), MaxOutputTokens: 10}
	if !model.Enabled() {
		fmt.Fprintln(os.Stderr, "set GEMINI_API_KEY: without it no post can be verified, so none would be saved")
		os.Exit(1)
	}
	posts, st, err := linkedin.Scroller{ADB: whatsapp.ExecADB{Serial: os.Getenv("ANDROID_SERIAL")}, Classifier: linkedin.Gemini{Client: model}}.
		Run(context.Background(), time.Duration(*minutes*float64(time.Minute)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "scroll stopped: %v (saving the %d posts found)\n", err, len(posts))
	}
	raw, _ := json.MarshalIndent(posts, "", "  ")
	_ = os.MkdirAll(filepath.Dir(*out), 0o755)
	if werr := os.WriteFile(*out, raw, 0o644); werr != nil {
		fmt.Fprintln(os.Stderr, werr)
		os.Exit(1)
	}
	fmt.Printf("%d real job posts saved to %s\n%+v\n", len(posts), *out, st)
	if err != nil {
		os.Exit(1)
	}
}
