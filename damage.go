package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type GitDamageType int

const (
	MissingObjects GitDamageType = iota
	MissingHead
	InvalidHeadRef
	MissingRefFile
	CorruptedIndex
	MissingRefsDir
	EmptyGitDir
	InvalidRemoteRef
)

func (d GitDamageType) String() string {
	switch d {
	case MissingObjects:
		return "missing objects"
	case MissingHead:
		return "missing HEAD"
	case InvalidHeadRef:
		return "invalid HEAD ref"
	case MissingRefFile:
		return "missing ref file"
	case CorruptedIndex:
		return "corrupted index"
	case MissingRefsDir:
		return "missing refs directory"
	case EmptyGitDir:
		return "empty .git directory"
	case InvalidRemoteRef:
		return "invalid remote ref"
	default:
		return "unknown damage"
	}
}

// detectGitDamageQuick returns true if the repository at path appears to be corrupted.
// It performs a fast check of the .git directory structure without running expensive fsck.
func detectGitDamageQuick(path string) bool {
	damage, _ := detectGitDamage(path)
	return len(damage) > 0
}

// detectGitDamage inspects the .git directory and reports specific damage types.
// A freshly initialized repo (HEAD pointing to a non-existent branch, empty index)
// is considered healthy because those states are normal before the first commit.
func detectGitDamage(path string) ([]GitDamageType, error) {
	gitDir := filepath.Join(path, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return nil, fmt.Errorf("no .git directory at %s", path)
	}
	if !info.IsDir() {
		return []GitDamageType{EmptyGitDir}, nil
	}

	entries, err := os.ReadDir(gitDir)
	if err != nil {
		return []GitDamageType{EmptyGitDir}, nil
	}
	if len(entries) == 0 {
		return []GitDamageType{EmptyGitDir}, nil
	}

	var damage []GitDamageType

	// Check for objects directory
	if _, err := os.Stat(filepath.Join(gitDir, "objects")); err != nil {
		damage = append(damage, MissingObjects)
	}

	// Check for refs directory
	refsDir := filepath.Join(gitDir, "refs")
	if _, err := os.Stat(refsDir); err != nil {
		damage = append(damage, MissingRefsDir)
	}

	// Check HEAD
	headPath := filepath.Join(gitDir, "HEAD")
	headData, err := os.ReadFile(headPath)
	if err != nil {
		damage = append(damage, MissingHead)
	} else {
		head := strings.TrimSpace(string(headData))
		if strings.HasPrefix(head, "ref: ") {
			refFile := strings.TrimPrefix(head, "ref: ")
			// HEAD may point to a branch that has no commits yet; that is normal.
			// Only report damage if the ref path is malformed or the refs directory
			// itself is missing.
			if strings.Contains(refFile, "..") || !strings.HasPrefix(refFile, "refs/") {
				damage = append(damage, InvalidHeadRef)
			}
		} else if len(head) != 40 {
			// HEAD should either be a symbolic ref or a full SHA
			damage = append(damage, InvalidHeadRef)
		}
	}

	// Check index: a missing index is normal for a fresh repo. If an index
	// file exists and has content, sanity-check the Git index magic bytes.
	indexPath := filepath.Join(gitDir, "index")
	if info, err := os.Stat(indexPath); err == nil && info.Size() > 0 {
		data, err := os.ReadFile(indexPath)
		if err != nil || len(data) < 4 || string(data[:4]) != "DIRC" {
			damage = append(damage, CorruptedIndex)
		}
	}

	return damage, nil
}
