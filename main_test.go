package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFormatBool(t *testing.T) {
	testCases := []struct {
		value     bool
		noUnicode bool
		expected  string
	}{
		{true, false, "✓"},
		{false, false, "✗"},
		{true, true, "true"},
		{false, true, "false"},
	}

	for _, tc := range testCases {
		result := formatBool(tc.value, tc.noUnicode)
		if result != tc.expected {
			t.Errorf("formatBool(%v, %v) = %q, expected %q", tc.value, tc.noUnicode, result, tc.expected)
		}
	}
}

func TestGetDefaultDirectory(t *testing.T) {
	// Test in a git repository
	gitDir, err := os.MkdirTemp("", "test-git")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(gitDir) }()

	cmd := exec.Command("git", "init")
	cmd.Dir = gitDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory
	subDir := filepath.Join(gitDir, "subdir")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Change to subdirectory and test that it returns git root
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	if err := os.Chdir(subDir); err != nil {
		t.Fatal(err)
	}

	result := getDefaultDirectory()
	// Resolve symlinks for comparison (macOS /tmp is symlinked to /private/tmp)
	resolvedResult, _ := filepath.EvalSymlinks(result)
	resolvedGitDir, _ := filepath.EvalSymlinks(gitDir)
	if resolvedResult != resolvedGitDir {
		t.Errorf("getDefaultDirectory() in git subdir = %q, expected %q", result, gitDir)
	}

	// Test in a jj repository
	jjDir, err := os.MkdirTemp("", "test-jj")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(jjDir) }()

	cmd = exec.Command("jj", "git", "init")
	cmd.Dir = jjDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test User", "GIT_AUTHOR_EMAIL=test@example.com")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// Configure jj user settings
	cmd = exec.Command("jj", "config", "set", "--repo", "user.name", "Test User")
	cmd.Dir = jjDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("jj", "config", "set", "--repo", "user.email", "test@example.com")
	cmd.Dir = jjDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory
	jjSubDir := filepath.Join(jjDir, "subdir")
	if err := os.Mkdir(jjSubDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(jjSubDir); err != nil {
		t.Fatal(err)
	}

	result = getDefaultDirectory()
	// Resolve symlinks for comparison (macOS /tmp is symlinked to /private/tmp)
	resolvedResult, _ = filepath.EvalSymlinks(result)
	resolvedJjDir, _ := filepath.EvalSymlinks(jjDir)
	if resolvedResult != resolvedJjDir {
		t.Errorf("getDefaultDirectory() in jj subdir = %q, expected %q", result, jjDir)
	}

	// Test outside any repository
	tmpDir, err := os.MkdirTemp("", "test-bare")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	result = getDefaultDirectory()
	if result != "." {
		t.Errorf("getDefaultDirectory() outside repo = %q, expected %q", result, ".")
	}
}

func TestGetSubdirectories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create subdirectories
	_ = os.Mkdir(filepath.Join(tmpDir, "dir1"), 0o755)
	_ = os.Mkdir(filepath.Join(tmpDir, "dir2"), 0o755)
	// Create hidden directory (should be ignored)
	_ = os.Mkdir(filepath.Join(tmpDir, ".hidden"), 0o755)
	// Create directory starting with _ (should be ignored)
	_ = os.Mkdir(filepath.Join(tmpDir, "_internal"), 0o755)
	// Create a file, which should be ignored
	_ = os.WriteFile(filepath.Join(tmpDir, "file1"), []byte(""), 0o644)

	subdirs, err := getSubdirectories(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(subdirs) != 2 {
		t.Errorf("expected 2 subdirectories, got %d", len(subdirs))
	}
}

func TestGetRepoStatus(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a bare directory
	bareDir := filepath.Join(tmpDir, "bare")
	_ = os.Mkdir(bareDir, 0o755)

	// Create a git directory
	gitDir := filepath.Join(tmpDir, "git")
	_ = os.Mkdir(gitDir, 0o755)
	cmd := exec.Command("git", "init")
	cmd.Dir = gitDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Create a jujutsu directory with colocated git
	jjDir := filepath.Join(tmpDir, "jj")
	_ = os.Mkdir(jjDir, 0o755)
	cmd = exec.Command("jj", "git", "init", "--colocate")
	cmd.Dir = jjDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test User", "GIT_AUTHOR_EMAIL=test@example.com")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// Configure jj user settings
	cmd = exec.Command("jj", "config", "set", "--repo", "user.name", "Test User")
	cmd.Dir = jjDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("jj", "config", "set", "--repo", "user.email", "test@example.com")
	cmd.Dir = jjDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("jj", "bookmark", "create", "main")
	cmd.Dir = jjDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("jj", "new", "-m", "initial commit")
	cmd.Dir = jjDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Create a jujutsu directory without colocated git
	jjGitDir := filepath.Join(tmpDir, "jj-git")
	_ = os.Mkdir(jjGitDir, 0o755)
	cmd = exec.Command("jj", "git", "init")
	cmd.Dir = jjGitDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test User", "GIT_AUTHOR_EMAIL=test@example.com")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// Configure jj user settings
	cmd = exec.Command("jj", "config", "set", "--repo", "user.name", "Test User")
	cmd.Dir = jjGitDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("jj", "config", "set", "--repo", "user.email", "test@example.com")
	cmd.Dir = jjGitDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("jj", "bookmark", "create", "main")
	cmd.Dir = jjGitDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("jj", "new", "-m", "initial commit")
	cmd.Dir = jjGitDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		dir      string
		repoType RepoType
	}{
		{bareDir, Bare},
		{gitDir, Git},
		{jjDir, Jujutsu},
		{jjGitDir, Jujutsu},
	}

	for _, tc := range testCases {
		status, err := getRepoStatus(tc.dir)
		if err != nil {
			t.Fatal(err)
		}
		if status.Type != tc.repoType {
			t.Errorf("for %s, expected repo type %s, got %s", tc.dir, tc.repoType, status.Type)
		}
	}
}

func TestGetGitStatus(t *testing.T) {
	// setup a git repo
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	status := &RepoStatus{Path: tmpDir}
	if err := getGitStatus(status); err != nil {
		t.Fatal(err)
	}
	if status.Dirty {
		t.Error("expected clean repo to be not dirty")
	}
	if status.Remote {
		t.Error("expected repo without remote to have no remote")
	}
	if status.Ahead {
		t.Error("expected repo without remote to have no ahead commits")
	}

	// dirty repo
	_ = os.WriteFile(filepath.Join(tmpDir, "file"), []byte(""), 0o644)
	if err := getGitStatus(status); err != nil {
		t.Fatal(err)
	}
	if !status.Dirty {
		t.Error("expected dirty repo to be dirty")
	}

	// add a remote
	remoteDir, err := os.MkdirTemp("", "remote")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(remoteDir) }()
	cmd = exec.Command("git", "init", "--bare")
	cmd.Dir = remoteDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "remote", "add", "origin", remoteDir)
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := getGitStatus(status); err != nil {
		t.Fatal(err)
	}
	if !status.Remote {
		t.Error("expected repo with remote to have a remote")
	}

	// add a commit and push it
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test User", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test User", "GIT_COMMITTER_EMAIL=test@example.com")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "branch", "-M", "main")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "push", "-u", "origin", "main")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := getGitStatus(status); err != nil {
		t.Fatal(err)
	}
	if status.Ahead {
		t.Error("expected repo with no ahead commits to have no ahead commits")
	}

	// add another commit
	_ = os.WriteFile(filepath.Join(tmpDir, "file2"), []byte(""), 0o644)
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "commit", "-m", "second commit")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test User", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test User", "GIT_COMMITTER_EMAIL=test@example.com")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := getGitStatus(status); err != nil {
		t.Fatal(err)
	}
	if !status.Ahead {
		t.Error("expected repo with ahead commits to have ahead commits")
	}
}

func TestGetJujutsuStatus(t *testing.T) {
	// setup a jj repo
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cmd := exec.Command("jj", "git", "init", "--colocate")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test User", "GIT_AUTHOR_EMAIL=test@example.com")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// Configure jj user settings
	cmd = exec.Command("jj", "config", "set", "--repo", "user.name", "Test User")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("jj", "config", "set", "--repo", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// Describe current revision to make it non-dirty
	cmd = exec.Command("jj", "describe", "-m", "initial commit")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("jj", "bookmark", "create", "main")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	status := &RepoStatus{Path: tmpDir}
	if err := getJujutsuStatus(status); err != nil {
		t.Fatal(err)
	}
	if status.Dirty {
		t.Error("expected clean repo to be not dirty")
	}
	if status.Remote {
		t.Error("expected repo without remote to have no remote")
	}
	if status.Ahead {
		t.Error("expected repo without remote to have no ahead commits")
	}

	// dirty repo - create a new revision without description
	cmd = exec.Command("jj", "new")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(tmpDir, "file"), []byte(""), 0o644)
	if err := getJujutsuStatus(status); err != nil {
		t.Fatal(err)
	}
	if !status.Dirty {
		t.Error("expected dirty repo to be dirty")
	}

	// add a remote
	remoteDir, err := os.MkdirTemp("", "remote")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(remoteDir) }()
	cmd = exec.Command("git", "init", "--bare")
	cmd.Dir = remoteDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("jj", "git", "remote", "add", "origin", remoteDir)
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := getJujutsuStatus(status); err != nil {
		t.Fatal(err)
	}
	if !status.Remote {
		t.Error("expected repo with remote to have a remote")
	}

	// commit current changes and push
	cmd = exec.Command("jj", "commit", "-m", "with file")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// Move bookmark to the committed revision and push
	cmd = exec.Command("jj", "bookmark", "set", "main", "-r", "@-")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("jj", "git", "push", "--bookmark", "main", "--allow-new")
	cmd.Dir = tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("push failed: %v\nOutput: %s", err, output)
	}
	if err := getJujutsuStatus(status); err != nil {
		t.Fatal(err)
	}
	// Debug: check what jj thinks is ahead
	debugCmd := exec.Command("jj", "log", "-r", "all() & ~ remote_bookmarks() & ~ root() & ~ empty()", "--no-graph", "-T", "commit_id")
	debugCmd.Dir = tmpDir
	debugOutput, _ := debugCmd.CombinedOutput()
	if status.Ahead {
		t.Errorf("expected repo with no ahead commits to have no ahead commits. Debug output: %q", string(debugOutput))
	}

	// add another commit
	cmd = exec.Command("jj", "new")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(tmpDir, "file3"), []byte(""), 0o644)
	if err := getJujutsuStatus(status); err != nil {
		t.Fatal(err)
	}
	if !status.Ahead {
		t.Error("expected repo with ahead commits to have ahead commits")
	}
}
