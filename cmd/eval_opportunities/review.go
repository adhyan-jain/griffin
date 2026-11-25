package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/griffin/core/internal/eval"
)

// review walks the unreviewed records one at a time. For each it prints the posting and the proposed labels and reads
// one command; the file is saved after every accepted or edited record, so quitting loses nothing.
//
//	Enter or a   accept the proposed labels as they are
//	i y|n        is it an internship
//	e c|l|u|i    eligibility: confirmed, likely, unknown, ineligible
//	q e|s|g|x|l  quality: exceptional, strong, good, exploratory, low
//	d ID|-       duplicate of that record id, or - for none
//	r TEXT       replace the reason
//	s            skip for now
//	x            quit
//
// Several edits can be chained before accepting; the record is marked reviewed when it is accepted.
func review(args []string) error {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	path := fs.String("dataset", defaultDataset, "dataset JSON to read and write")
	all := fs.Bool("all", false, "also show records that are already reviewed")
	_ = fs.Parse(args)

	raw, err := os.ReadFile(*path)
	if err != nil {
		return fmt.Errorf("%w (run this from the repository root, or pass -dataset)", err)
	}
	recs, err := eval.Parse(raw)
	if err != nil {
		return err
	}
	pending := 0
	for _, r := range recs {
		if !r.Labels.Reviewed {
			pending++
		}
	}
	fmt.Printf("%d records, %d to review. Commands: Enter=accept, i y|n, e c|l|u|i, q e|s|g|x|l, d ID|-, r TEXT, s=skip, x=quit\n", len(recs), pending)

	in := bufio.NewScanner(os.Stdin)
	for i := range recs {
		r := &recs[i]
		if r.Labels.Reviewed && !*all {
			continue
		}
		show(i+1, len(recs), r)
	prompt:
		for {
			fmt.Print("> ")
			if !in.Scan() {
				return save(*path, recs)
			}
			cmd := strings.TrimSpace(in.Text())
			word, rest, _ := strings.Cut(cmd, " ")
			rest = strings.TrimSpace(rest)
			switch strings.ToLower(word) {
			case "", "a":
				r.Labels.Reviewed = true
				if err := save(*path, recs); err != nil {
					return err
				}
				break prompt
			case "s":
				break prompt
			case "x":
				return save(*path, recs)
			case "i":
				r.Labels.IsInternship = strings.HasPrefix(strings.ToLower(rest), "y")
			case "e":
				if v := pick(rest, map[string]string{"c": eval.Confirmed, "l": eval.Likely, "u": eval.Unknown, "i": eval.Ineligible}); v != "" {
					r.Labels.Eligible = v
				}
			case "q":
				if v := pick(rest, map[string]string{"e": eval.Exceptional, "s": eval.Strong, "g": eval.Good, "x": eval.Exploratory, "l": eval.Low}); v != "" {
					r.Labels.Quality = v
				}
			case "d":
				if rest == "-" || rest == "" {
					r.Labels.DuplicateOf = ""
				} else {
					r.Labels.DuplicateOf = rest
				}
			case "r":
				r.Labels.Reasons = []string{rest}
			default:
				fmt.Println("unknown command")
				continue
			}
			if err := eval.Validate(recs); err != nil {
				fmt.Println("not valid:", err)
				continue
			}
			fmt.Printf("  now: internship=%v eligible=%s quality=%s duplicate_of=%q\n", r.Labels.IsInternship, r.Labels.Eligible, r.Labels.Quality, r.Labels.DuplicateOf)
		}
	}
	return save(*path, recs)
}

func pick(s string, choices map[string]string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if v, ok := choices[s]; ok {
		return v
	}
	for _, v := range choices {
		if v == s {
			return v
		}
	}
	fmt.Println("not one of the choices")
	return ""
}

func save(path string, recs []eval.Record) error {
	b, err := eval.Marshal(recs)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func show(n, total int, r *eval.Record) {
	j := r.Raw
	fmt.Printf("\n=== %d/%d  %s  [%s]  tags: %s\n", n, total, r.ID, j.Source, strings.Join(r.Tags, ", "))
	fmt.Printf("%s | %s\n", j.EmployerName, j.Title)
	fmt.Printf("Location: %s   Type: %s   Department: %s   Pay: %s\n", strings.Join(j.Locations, "; "), j.EmploymentType, j.Department, j.Compensation)
	fmt.Printf("Link: %s   (read from %s on %s)\n", j.URL, r.SourceURL, r.FetchedDate)
	if d := strings.Join(strings.Fields(j.Description), " "); d != "" {
		if len(d) > 700 {
			d = d[:700] + " ..."
		}
		fmt.Println("Text:", d)
	} else {
		fmt.Println("Text: (the source gave no description)")
	}
	l := r.Labels
	fmt.Printf("PROPOSED: internship=%v  eligible=%s  quality=%s  duplicate_of=%q\n  because: %s\n", l.IsInternship, l.Eligible, l.Quality, l.DuplicateOf, strings.Join(l.Reasons, " "))
}
