package actions

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"hash"

	"github.com/osteele/gitsync/internal/vcs"
)

// DescriptionInputHash returns a digest of the VCS change input consumed by
// the repository's AI commit tool. It uses supported CLI output and does not
// read either VCS's private state.
func DescriptionInputHash(path string) (string, error) {
	repoType := vcs.DetectRepoType(path)
	digest := sha256.New()
	switch repoType {
	case vcs.Git:
		if err := hashGitDescriptionInput(digest, path); err != nil {
			return "", err
		}
	case vcs.Jujutsu:
		diff, err := vcs.RunVCSOutputWithin(path, vcs.StatusTimeout, "jj", "diff", "--no-pager", "--git", "-r", "@")
		if err != nil {
			return "", fmt.Errorf("hash jj diff: %w", err)
		}
		writeHashSection(digest, "jj-diff", diff)
	default:
		return "", fmt.Errorf("not a repository")
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func hashGitDescriptionInput(digest hash.Hash, path string) error {
	if _, err := vcs.RunVCSOutputWithin(path, vcs.StatusTimeout, "git", "rev-parse", "--verify", "HEAD"); err == nil {
		diff, diffErr := vcs.RunVCSOutputWithin(path, vcs.StatusTimeout, "git", "diff", "HEAD")
		if diffErr != nil {
			return fmt.Errorf("hash git diff: %w", diffErr)
		}
		writeHashSection(digest, "git-diff", diff)
	} else {
		// An unborn repository has no HEAD. This is the same decomposition
		// git-ai-commit uses: index versus the empty tree, then worktree
		// versus index.
		cached, cachedErr := vcs.RunVCSOutputWithin(path, vcs.StatusTimeout, "git", "diff", "--cached")
		if cachedErr != nil {
			return fmt.Errorf("hash unborn git index diff: %w", cachedErr)
		}
		worktree, worktreeErr := vcs.RunVCSOutputWithin(path, vcs.StatusTimeout, "git", "diff")
		if worktreeErr != nil {
			return fmt.Errorf("hash unborn git worktree diff: %w", worktreeErr)
		}
		writeHashSection(digest, "git-index-diff", cached)
		writeHashSection(digest, "git-worktree-diff", worktree)
	}

	untracked, err := vcs.RunVCSOutputWithin(path, vcs.StatusTimeout, "git", "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return fmt.Errorf("hash git untracked files: %w", err)
	}
	writeHashSection(digest, "git-untracked", untracked)
	files := bytes.Split(bytes.TrimSuffix(untracked, []byte{0}), []byte{0})
	if len(files) == 1 && len(files[0]) == 0 {
		return nil
	}
	// Hash file contents through Git in bounded batches. The name list alone
	// is insufficient: editing an already-untracked file must invalidate a
	// cached description even though `git ls-files --others` is unchanged.
	const batchSize = 128
	for start := 0; start < len(files); start += batchSize {
		end := min(start+batchSize, len(files))
		args := []string{"hash-object", "--no-filters", "--"}
		for _, file := range files[start:end] {
			args = append(args, string(file))
		}
		contents, hashErr := vcs.RunVCSOutputWithin(path, vcs.StatusTimeout, "git", args...)
		if hashErr != nil {
			return fmt.Errorf("hash git untracked contents: %w", hashErr)
		}
		writeHashSection(digest, fmt.Sprintf("git-untracked-content-%d", start), contents)
	}
	return nil
}

func writeHashSection(digest hash.Hash, name string, contents []byte) {
	_, _ = fmt.Fprintf(digest, "%s:%d\x00", name, len(contents))
	_, _ = digest.Write(contents)
}
