package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/osteele/gitsync/internal/query"
	"github.com/osteele/gitsync/internal/report"
	"github.com/osteele/gitsync/internal/scan"
	"github.com/osteele/gitsync/internal/tui"
	"github.com/osteele/gitsync/internal/vcs"
)

func main() {
	flag.Bool("no-unicode", false, "Use text instead of Unicode symbols (deprecated; output is now plain text)")
	filterExpr := flag.String("filter", "", "Filter expression (e.g., 'dirty', 'git & ahead', 'jj | bare')")
	flag.StringVar(filterExpr, "f", "", "Filter expression (short form)")
	sortExpr := flag.String("sort", "", "Sort expression (e.g., 'name', '!dirty,name', 'vcs,ahead')")
	flag.StringVar(sortExpr, "s", "", "Sort expression (short form)")
	interactive := flag.Bool("interactive", false, "Launch interactive TUI")
	flag.BoolVar(interactive, "i", false, "Launch interactive TUI (short form)")
	showAll := flag.Bool("all", false, "Include non-repository (bare) directories in the table")
	flag.Parse()

	// Determine which directory to scan
	var scanDir string
	args := flag.Args()
	if len(args) > 0 {
		scanDir = args[0]
	} else {
		scanDir = vcs.GetDefaultDirectory()
	}

	if *interactive {
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

	filterGiven := *filterExpr != ""
	if filterGiven {
		filter, err := query.ParseFilter(*filterExpr)
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

	sortKeys, err := query.ParseSort(*sortExpr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing sort expression: %v\n", err)
		os.Exit(2)
	}
	query.SortResults(results, sortKeys)

	visible, hiddenBare := report.PrepareDisplay(results, *showAll)
	report.PrintReport(os.Stdout, visible, hiddenBare, report.UseColor(os.Stdout))
	os.Exit(report.FilterExitCode(filterGiven, len(results)))
}
