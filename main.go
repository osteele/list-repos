package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/osteele/gitsync/internal/query"
	"github.com/osteele/gitsync/internal/report"
	"github.com/osteele/gitsync/internal/scan"
	"github.com/osteele/gitsync/internal/tui"
	"github.com/osteele/gitsync/internal/vcs"
)

// options holds the parsed command line.
type options struct {
	filter      string
	sort        string
	interactive bool
	showAll     bool
	recursive   bool
	depth       int
	scanDir     string
}

// parseArgs parses argv (excluding the program name) into options. The flag
// package stops at the first non-flag argument, so parsing loops: lift one
// positional per pass and re-parse the remainder, which accepts flags before
// or after the directory while leaving flag values to the flag package.
// Errors and usage are written to stderr; the caller turns them into exit 2.
func parseArgs(argv []string, stderr io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet("gitsync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Bool("no-unicode", false, "Use text instead of Unicode symbols (deprecated; output is now plain text)")
	fs.StringVar(&opts.filter, "filter", "", "Filter expression (e.g., 'dirty', 'git & ahead', 'jj | dir')")
	fs.StringVar(&opts.filter, "f", "", "Filter expression (short form)")
	fs.StringVar(&opts.sort, "sort", "", "Sort expression (e.g., 'name', '!dirty,name', 'vcs,ahead')")
	fs.StringVar(&opts.sort, "s", "", "Sort expression (short form)")
	fs.BoolVar(&opts.interactive, "interactive", false, "Launch interactive TUI")
	fs.BoolVar(&opts.interactive, "i", false, "Launch interactive TUI (short form)")
	fs.BoolVar(&opts.showAll, "all", false, "Include non-repository (dir) directories in the table")
	fs.BoolVar(&opts.recursive, "recursive", false, "Descend into non-repository directories to find nested repositories")
	fs.BoolVar(&opts.recursive, "r", false, "Descend into non-repository directories (short form)")
	fs.IntVar(&opts.depth, "depth", 4, "Cap recursive descent at this many levels (implies -r)")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: gitsync [flags] [DIR]\n\n")
		_, _ = fmt.Fprintf(fs.Output(), "DIR defaults to the repository root or current directory.\n\nFlags:\n")
		fs.PrintDefaults()
	}

	var positionals []string
	args := argv
	for {
		if err := fs.Parse(args); err != nil {
			return options{}, err
		}
		if fs.NArg() == 0 {
			break
		}
		positionals = append(positionals, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(positionals) > 1 {
		err := fmt.Errorf("expected at most one directory; unexpected arguments: %s", strings.Join(positionals[1:], " "))
		_, _ = fmt.Fprintf(fs.Output(), "gitsync: %v\n", err)
		fs.Usage()
		return options{}, err
	}
	if len(positionals) == 1 {
		opts.scanDir = positionals[0]
	}
	// --depth implies --recursive.
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "depth" {
			opts.recursive = true
		}
	})
	if opts.depth < 1 {
		err := fmt.Errorf("depth must be at least 1, got %d", opts.depth)
		_, _ = fmt.Fprintf(fs.Output(), "gitsync: %v\n", err)
		return options{}, err
	}
	return opts, nil
}

func main() {
	opts, err := parseArgs(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		os.Exit(2)
	}

	// Determine which directory to scan
	scanDir := opts.scanDir
	if scanDir == "" {
		scanDir = vcs.GetDefaultDirectory()
	}

	if opts.interactive {
		if err := tui.RunTUI(scanDir); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	results, err := scan.Scan(scanDir, scan.Options{Recursive: opts.recursive, MaxDepth: opts.depth})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// A directory that contains repositories is worth showing even though
	// it is not one — as context, so the summary does not tally it. Under
	// a filter, containers instead show only when a match lives inside.
	filterGiven := opts.filter != ""
	if !filterGiven {
		for _, status := range results {
			if status.Type == vcs.Dir && status.NestedRepos > 0 {
				status.ContextOnly = true
			}
		}
	}

	matches := 0
	if filterGiven {
		filter, err := query.ParseFilter(opts.filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error parsing filter: %v\n", err)
			os.Exit(2)
		}

		// Keep matching rows; an ancestor of a match is kept too, as
		// context — it is the only way to see where the match lives.
		// Results are ordered so ancestors precede descendants, so a row's
		// ancestors are the nearest preceding rows at each shallower depth.
		keep := make([]bool, len(results))
		for i, status := range results {
			if !filter.Match(status) {
				continue
			}
			matches++
			keep[i] = true
			for depth, j := status.Depth, i-1; depth > 1 && j >= 0; j-- {
				if results[j].Depth == depth-1 {
					keep[j] = true
					depth--
				}
			}
		}
		var filtered []*vcs.RepoStatus
		for i, status := range results {
			if keep[i] {
				filtered = append(filtered, status)
			}
		}
		results = filtered
	}

	sortKeys, err := query.ParseSort(opts.sort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing sort expression: %v\n", err)
		os.Exit(2)
	}
	query.SortResults(results, sortKeys)

	visible, hiddenBare := report.PrepareDisplay(results, opts.showAll)
	report.PrintReport(os.Stdout, visible, hiddenBare, report.UseColor(os.Stdout))
	os.Exit(report.FilterExitCode(filterGiven, matches))
}
