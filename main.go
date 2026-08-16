package main

import (
	"flag"
	"fmt"
	"os"
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
		scanDir = getDefaultDirectory()
	}

	if *interactive {
		if err := runTUI(scanDir); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	subdirs, err := getSubdirectories(scanDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	results := processSubdirectoriesParallel(subdirs)

	filterGiven := *filterExpr != ""
	if filterGiven {
		filter, err := ParseFilter(*filterExpr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error parsing filter: %v\n", err)
			os.Exit(2)
		}

		var filtered []*RepoStatus
		for _, status := range results {
			if filter.Match(status) {
				filtered = append(filtered, status)
			}
		}
		results = filtered
	}

	sortKeys, err := ParseSort(*sortExpr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing sort expression: %v\n", err)
		os.Exit(2)
	}
	SortResults(results, sortKeys)

	visible, hiddenBare := prepareDisplay(results, *showAll)
	printReport(os.Stdout, visible, hiddenBare, useColor(os.Stdout))
	os.Exit(filterExitCode(filterGiven, len(results)))
}
