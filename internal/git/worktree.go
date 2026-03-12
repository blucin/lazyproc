package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree represents a single git worktree entry.
type Worktree struct {
	// Path is the absolute filesystem path to the worktree.
	Path string
	// Branch is the checked-out branch name (e.g. "refs/heads/main").
	// Empty string for detached HEAD worktrees.
	Branch string
	// HEAD is the commit SHA the worktree is currently on.
	HEAD string
	// IsBare is true when the worktree is a bare clone.
	IsBare bool
}

// ShortBranch returns the branch name with the "refs/heads/" prefix stripped,
// or "(detached)" when the worktree is in detached HEAD state.
func (w Worktree) ShortBranch() string {
	if w.Branch == "" {
		return "(detached)"
	}
	return strings.TrimPrefix(w.Branch, "refs/heads/")
}

// RepoRoot returns the toplevel directory of the git repository that contains
// cwd, or an error if cwd is not inside a git repository.
func RepoRoot(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository (or git is not in PATH): %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// IsGitRepo reports whether cwd is inside a git repository.
// It returns false (without an error) when git is unavailable or the directory
// is not tracked by git.
func IsGitRepo(cwd string) bool {
	_, err := RepoRoot(cwd)
	return err == nil
}

// IsWorktreeRoot reports whether dir is the root of a git worktree
func IsWorktreeRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	// Accept both a .git directory (main worktree) and a .git file (linked
	// worktree created by `git worktree add`).
	return info.IsDir() || info.Mode().IsRegular()
}

// ListWorktrees returns all worktrees for the repository that contains cwd.
// The list is parsed from the output of `git worktree list --porcelain`.
func ListWorktrees(cwd string) ([]Worktree, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = cwd

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("listing worktrees: %w", err)
	}

	return parseWorktrees(out), nil
}

// parseWorktrees parses the porcelain output of `git worktree list --porcelain`.
// Each worktree block is separated by a blank line and has the form:
//
//	worktree /path/to/worktree
//	HEAD <sha>
//	branch refs/heads/<name>   (or "detached" for detached HEAD)
//	[bare]
func parseWorktrees(data []byte) []Worktree {
	var worktrees []Worktree

	// Split into per-worktree blocks separated by blank lines.
	blocks := bytes.Split(bytes.TrimSpace(data), []byte("\n\n"))

	for _, block := range blocks {
		if len(bytes.TrimSpace(block)) == 0 {
			continue
		}

		var wt Worktree

		lines := bytes.Split(block, []byte("\n"))
		for _, line := range lines {
			line = bytes.TrimSpace(line)
			switch {
			case bytes.HasPrefix(line, []byte("worktree ")):
				wt.Path = string(bytes.TrimPrefix(line, []byte("worktree ")))
			case bytes.HasPrefix(line, []byte("HEAD ")):
				wt.HEAD = string(bytes.TrimPrefix(line, []byte("HEAD ")))
			case bytes.HasPrefix(line, []byte("branch ")):
				wt.Branch = string(bytes.TrimPrefix(line, []byte("branch ")))
			case bytes.Equal(line, []byte("bare")):
				wt.IsBare = true
			case bytes.Equal(line, []byte("detached")):
				// Detached HEAD, leave Branch as "".
			}
		}

		if wt.Path != "" {
			worktrees = append(worktrees, wt)
		}
	}

	return worktrees
}

// CurrentWorktree returns the Worktree entry that corresponds to cwd, or an
// error if it cannot be determined.
func CurrentWorktree(cwd string) (Worktree, error) {
	worktrees, err := ListWorktrees(cwd)
	if err != nil {
		return Worktree{}, err
	}

	root, err := RepoRoot(cwd)
	if err != nil {
		return Worktree{}, err
	}

	for _, wt := range worktrees {
		if wt.Path == root {
			return wt, nil
		}
	}

	return Worktree{}, fmt.Errorf("could not find current worktree for path %q", cwd)
}
