package query

import (
	"fmt"
	"strings"

	"github.com/osteele/gitsync/internal/vcs"
)

// Filter represents a filter expression that can match against repository status
type Filter interface {
	Match(status *vcs.RepoStatus) bool
}

// ParseFilter parses a filter expression string into a Filter
func ParseFilter(expr string) (Filter, error) {
	if expr == "" {
		return &allFilter{}, nil
	}

	parser := &filterParser{input: expr, pos: 0}
	filter, err := parser.parse()
	if err != nil {
		return nil, err
	}

	// Check if there's any remaining unparsed input
	parser.skipWhitespace()
	if parser.pos < len(parser.input) {
		return nil, fmt.Errorf("unexpected character '%c' at position %d", parser.input[parser.pos], parser.pos)
	}

	return filter, nil
}

// Basic filter implementations

type allFilter struct{}

func (f *allFilter) Match(status *vcs.RepoStatus) bool {
	return true
}

type andFilter struct {
	left, right Filter
}

func (f *andFilter) Match(status *vcs.RepoStatus) bool {
	return f.left.Match(status) && f.right.Match(status)
}

type orFilter struct {
	left, right Filter
}

func (f *orFilter) Match(status *vcs.RepoStatus) bool {
	return f.left.Match(status) || f.right.Match(status)
}

type notFilter struct {
	filter Filter
}

func (f *notFilter) Match(status *vcs.RepoStatus) bool {
	return !f.filter.Match(status)
}

// Term filters

type dirtyFilter struct{}

func (f *dirtyFilter) Match(status *vcs.RepoStatus) bool {
	return status.Dirty
}

type cleanFilter struct{}

func (f *cleanFilter) Match(status *vcs.RepoStatus) bool {
	return !status.Dirty
}

type aheadFilter struct{}

func (f *aheadFilter) Match(status *vcs.RepoStatus) bool {
	return status.Ahead.Positive()
}

type remoteFilter struct{}

func (f *remoteFilter) Match(status *vcs.RepoStatus) bool {
	return status.Remote
}

type localFilter struct{}

func (f *localFilter) Match(status *vcs.RepoStatus) bool {
	return !status.Remote
}

type behindFilter struct{}

func (f *behindFilter) Match(status *vcs.RepoStatus) bool {
	return status.Behind.Positive()
}

type corruptedFilter struct{}

func (f *corruptedFilter) Match(status *vcs.RepoStatus) bool {
	return status.Corrupted
}

type gitFilter struct{}

func (f *gitFilter) Match(status *vcs.RepoStatus) bool {
	return status.Type == vcs.Git
}

type jjFilter struct{}

func (f *jjFilter) Match(status *vcs.RepoStatus) bool {
	return status.Type == vcs.Jujutsu
}

type bareFilter struct{}

func (f *bareFilter) Match(status *vcs.RepoStatus) bool {
	return status.Type == vcs.Bare
}

// Parser

type filterParser struct {
	input string
	pos   int
}

func (p *filterParser) parse() (Filter, error) {
	return p.parseOr()
}

func (p *filterParser) parseOr() (Filter, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}

	for {
		p.skipWhitespace()
		if p.consumeKeyword("or") || p.consume("|") {
			right, err := p.parseAnd()
			if err != nil {
				return nil, err
			}
			left = &orFilter{left: left, right: right}
		} else {
			break
		}
	}

	return left, nil
}

func (p *filterParser) parseAnd() (Filter, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}

	for {
		p.skipWhitespace()
		// Check for explicit 'and' or '&' operator
		if p.consumeKeyword("and") || p.consume("&") {
			right, err := p.parseNot()
			if err != nil {
				return nil, err
			}
			left = &andFilter{left: left, right: right}
		} else if p.peek() != "" && !p.isAtOperator() && p.peek() != ")" {
			// Implicit AND when terms are adjacent
			right, err := p.parseNot()
			if err != nil {
				return nil, err
			}
			left = &andFilter{left: left, right: right}
		} else {
			break
		}
	}

	return left, nil
}

func (p *filterParser) parseNot() (Filter, error) {
	p.skipWhitespace()
	if p.consumeKeyword("not") || p.consume("!") {
		filter, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &notFilter{filter: filter}, nil
	}
	return p.parseTerm()
}

func (p *filterParser) parseTerm() (Filter, error) {
	p.skipWhitespace()

	// Check for parentheses
	if p.consume("(") {
		filter, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.consume(")") {
			return nil, fmt.Errorf("expected ')' at position %d", p.pos)
		}
		return filter, nil
	}

	// Parse term keywords
	term := p.parseWord()
	if term == "" {
		if p.pos >= len(p.input) {
			return nil, fmt.Errorf("unexpected end of expression")
		}
		return nil, fmt.Errorf("unexpected character '%c' at position %d", p.input[p.pos], p.pos)
	}

	switch strings.ToLower(term) {
	case "dirty":
		return &dirtyFilter{}, nil
	case "clean":
		return &cleanFilter{}, nil
	case "ahead":
		return &aheadFilter{}, nil
	case "behind":
		return &behindFilter{}, nil
	case "remote":
		return &remoteFilter{}, nil
	case "local":
		return &localFilter{}, nil
	case "corrupted":
		return &corruptedFilter{}, nil
	case "git":
		return &gitFilter{}, nil
	case "jj", "jujutsu":
		return &jjFilter{}, nil
	case "bare":
		return &bareFilter{}, nil
	default:
		return nil, fmt.Errorf("unknown filter term: %s", term)
	}
}

func (p *filterParser) skipWhitespace() {
	for p.pos < len(p.input) && (p.input[p.pos] == ' ' || p.input[p.pos] == '\t') {
		p.pos++
	}
}

func (p *filterParser) consume(s string) bool {
	p.skipWhitespace()
	if p.pos+len(s) <= len(p.input) && p.input[p.pos:p.pos+len(s)] == s {
		p.pos += len(s)
		return true
	}
	return false
}

func (p *filterParser) consumeKeyword(s string) bool {
	p.skipWhitespace()
	if p.pos+len(s) <= len(p.input) && strings.EqualFold(p.input[p.pos:p.pos+len(s)], s) {
		// Make sure it's a complete word (not part of another word)
		if p.pos+len(s) < len(p.input) {
			next := p.input[p.pos+len(s)]
			if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') {
				return false
			}
		}
		p.pos += len(s)
		return true
	}
	return false
}

func (p *filterParser) parseWord() string {
	p.skipWhitespace()
	start := p.pos
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
			p.pos++
		} else {
			break
		}
	}
	return p.input[start:p.pos]
}

func (p *filterParser) peek() string {
	p.skipWhitespace()
	if p.pos >= len(p.input) {
		return ""
	}
	// Try to peek at the next word or operator
	tempPos := p.pos
	word := p.parseWord()
	p.pos = tempPos
	if word != "" {
		return word
	}
	// Check for single character operators
	return string(p.input[p.pos])
}

func (p *filterParser) isAtOperator() bool {
	peek := p.peek()
	return peek == "|" || peek == "&" || strings.EqualFold(peek, "or") || strings.EqualFold(peek, "and")
}
