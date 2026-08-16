package main

import (
	"io"
	"strings"
	"testing"
)

func TestFlagsAfterPositionalAreParsed(t *testing.T) {
	// The scan directory is positional; flags written after it must behave
	// exactly as if written before it.
	before, err := parseArgs([]string{"-f", "dirty", "-s", "name", "DIR"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	after, err := parseArgs([]string{"DIR", "-f", "dirty", "-s", "name"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("flag order changed the result: before=%+v after=%+v", before, after)
	}
	if after.scanDir != "DIR" || after.filter != "dirty" || after.sort != "name" {
		t.Fatalf("unexpected options: %+v", after)
	}
}

func TestUnknownFlagAfterPositionalIsError(t *testing.T) {
	if _, err := parseArgs([]string{"DIR", "--tui"}, io.Discard); err == nil {
		t.Fatal("expected an unknown flag after the directory to be an error")
	}
	if _, err := parseArgs([]string{"--tui", "DIR"}, io.Discard); err == nil {
		t.Fatal("expected an unknown flag before the directory to be an error")
	}
}

func TestTwoPositionalsAreError(t *testing.T) {
	_, err := parseArgs([]string{"DIR1", "DIR2"}, io.Discard)
	if err == nil {
		t.Fatal("expected two directories to be a usage error")
	}
	if !strings.Contains(err.Error(), "DIR2") {
		t.Fatalf("expected the error to name the extra argument, got %v", err)
	}
}

func TestMissingFlagValueAfterPositionalIsError(t *testing.T) {
	if _, err := parseArgs([]string{"DIR", "-f"}, io.Discard); err == nil {
		t.Fatal("expected a flag missing its value to be an error")
	}
}

func TestRecursiveAndDepthFlags(t *testing.T) {
	// --depth implies --recursive.
	opts, err := parseArgs([]string{"--depth", "2", "DIR"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.recursive || opts.depth != 2 || opts.scanDir != "DIR" {
		t.Fatalf("unexpected options: %+v", opts)
	}

	// -r alone recurses with the default depth of 4.
	opts, err = parseArgs([]string{"-r", "DIR"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.recursive || opts.depth != 4 {
		t.Fatalf("unexpected options: %+v", opts)
	}

	// The default stays non-recursive.
	opts, err = parseArgs([]string{"DIR"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if opts.recursive {
		t.Fatalf("unexpected options: %+v", opts)
	}

	if _, err := parseArgs([]string{"--depth", "0"}, io.Discard); err == nil {
		t.Fatal("expected a non-positive depth to be an error")
	}
}
