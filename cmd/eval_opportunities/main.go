// Command eval_opportunities runs the opportunity benchmark: the pipeline's deterministic stages, offline, over the
// dataset in internal/eval/testdata/dataset.json, and writes the metrics as markdown and JSON. It never touches the
// network and never calls a model.
//
//	go run ./cmd/eval_opportunities -md /tmp/bench.md -json /tmp/bench.json
//	go run ./cmd/eval_opportunities review            # confirm or edit proposed labels, one record at a time
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/griffin/core/internal/eval"
)

const defaultDataset = "internal/eval/testdata/dataset.json"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "review" {
		if err := review(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "review:", err)
			os.Exit(1)
		}
		return
	}
	fs := flag.NewFlagSet("eval_opportunities", flag.ExitOnError)
	dataset := fs.String("dataset", "", "dataset JSON to read (default: the one embedded in internal/eval)")
	md := fs.String("md", "", "write the markdown report to this file (default: standard output)")
	js := fs.String("json", "", "write the JSON report to this file")
	model := fs.Bool("model", false, "include the model stage (not available yet: the benchmark runs the deterministic stages only)")
	_ = fs.Parse(os.Args[1:])
	if *model {
		fmt.Fprintln(os.Stderr, "eval_opportunities: the model stage is not wired into the benchmark yet; it runs offline with the deterministic stages only")
		os.Exit(2)
	}

	recs, err := load(*dataset)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval_opportunities:", err)
		os.Exit(1)
	}
	rep := eval.Build(recs)
	text := rep.Markdown()
	if *md == "" {
		fmt.Print(text)
	} else if err := os.WriteFile(*md, []byte(text), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "eval_opportunities:", err)
		os.Exit(1)
	}
	if *js != "" {
		b, err := eval.MarshalReport(rep)
		if err == nil {
			err = os.WriteFile(*js, b, 0o644)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "eval_opportunities:", err)
			os.Exit(1)
		}
	}
}

// load reads the dataset at path, or the embedded one when path is empty.
func load(path string) ([]eval.Record, error) {
	if path == "" {
		return eval.Embedded()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return eval.Parse(b)
}
