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
	fs.StringVar(&opts.filter, "filter", "", "Filter expression (e.g., 'dirty', 'git & ahead', 'jj | bare')")
	fs.StringVar(&opts.filter, "f", "", "Filter expression (short form)")
	fs.StringVar(&opts.sort, "sort", "", "Sort expression (e.g., 'name', '!dirty,name', 'vcs,ahead')")
	fs.StringVar(&opts.sort, "s", "", "Sort expression (short form)")
	fs.BoolVar(&opts.interactive, "interactive", false, "Launch interactive TUI")
	fs.BoolVar(&opts.interactive, "i", false, "Launch interactive TUI (short form)")
	fs.BoolVar(&opts.showAll, "all", false, "Include non-repository (bare) directories in the table")
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

	subdirs, err := scan.GetSubdirectories(scanDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	results := scan.ProcessSubdirectoriesParallel(subdirs)

	filterGiven := opts.filter != ""
	if filterGiven {
		filter, err := query.ParseFilter(opts.filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error parsing filter: %v\n", err)
			os.Exit(2)
		}

		var filtered []*vcs.RepoStatus
		for _, status := range results {
			if filter.Match(status) {
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
	os.Exit(report.FilterExitCode(filterGiven, len(results)))
}
