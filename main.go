package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	noUnicode := flag.Bool("no-unicode", false, "Use text instead of Unicode symbols for boolean values")
	filterExpr := flag.String("filter", "", "Filter expression (e.g., 'dirty', 'git & ahead', 'jj | bare')")
	flag.StringVar(filterExpr, "f", "", "Filter expression (short form)")
	sortExpr := flag.String("sort", "", "Sort expression (e.g., 'name', '!dirty,name', 'vcs,ahead')")
	flag.StringVar(sortExpr, "s", "", "Sort expression (short form)")
	interactive := flag.Bool("interactive", false, "Launch interactive TUI")
	flag.BoolVar(interactive, "i", false, "Launch interactive TUI (short form)")
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

	if *filterExpr != "" {
		filter, err := ParseFilter(*filterExpr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error parsing filter: %v\n", err)
			os.Exit(1)
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
		os.Exit(1)
	}
	SortResults(results, sortKeys)

	fmt.Printf("% -30s % -10s % -10s % -7s % -7s % -7s\n", "Name", "VCS", "Corrupted", "Dirty", "Remote", "Ahead")
	for _, status := range results {
		fmt.Printf("% -30s % -10s % -10s % -7s % -7s % -7s\n",
			filepath.Base(status.Path),
			status.Type,
			formatBool(status.Corrupted, *noUnicode),
			formatBool(status.Dirty, *noUnicode),
			formatBool(status.Remote, *noUnicode),
			formatBool(status.Ahead, *noUnicode))
	}
}
