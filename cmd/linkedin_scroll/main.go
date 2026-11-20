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
)

func main() {
	minutes := flag.Float64("minutes", 10, "how long to scroll")
	home, _ := os.UserHomeDir()
	out := flag.String("out", filepath.Join(home, "Documents", "Griffin", "internships", "linkedin_posts.json"), "where to save the posts")
	flag.Parse()

	posts, err := linkedin.Scroller{ADB: whatsapp.ExecADB{Serial: os.Getenv("ANDROID_SERIAL")}}.
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
	fmt.Printf("%d job-like posts saved to %s\n", len(posts), *out)
	if err != nil {
		os.Exit(1)
	}
}
