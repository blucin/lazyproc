package ui

import (
	"fmt"
	"strings"

	"github.com/blucin/lazyproc/internal/git"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Messages ──────────────────────────────────────────────────────────────────

// worktreeSelectedMsg is sent when the user confirms a worktree switch.
type worktreeSelectedMsg struct {
	worktree git.Worktree
}

// worktreeDismissMsg is sent when the user cancels the picker (Esc / q).
type worktreeDismissMsg struct{}

// ── Picker state ──────────────────────────────────────────────────────────────

// worktreePicker is a self-contained modal component for selecting a git
// worktree. It is embedded in the root Model and rendered as an overlay when
// active.
type worktreePicker struct {
	worktrees []git.Worktree
	cursor    int
	// currentPath is the path of the worktree that is already active, used to
	// mark it in the list and prevent a no-op switch.
	currentPath string
}

// newWorktreePicker constructs a picker pre-populated with the given worktrees.
// currentPath is the filesystem path of the currently-active worktree.
func newWorktreePicker(worktrees []git.Worktree, currentPath string) worktreePicker {
	// Start the cursor on the currently-active worktree so the user can see
	// where they are before moving.
	cursor := 0
	for i, wt := range worktrees {
		if wt.Path == currentPath {
			cursor = i
			break
		}
	}
	return worktreePicker{
		worktrees:   worktrees,
		cursor:      cursor,
		currentPath: currentPath,
	}
}

// Update handles keyboard input for the picker and returns an optional
// tea.Cmd. The returned cmd will be either a worktreeSelectedMsg (enter),
// a worktreeDismissMsg (esc/q), or nil (navigation only).
func (p *worktreePicker) Update(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}

	case "down", "j":
		if p.cursor < len(p.worktrees)-1 {
			p.cursor++
		}

	case "enter":
		if len(p.worktrees) == 0 {
			return dismiss()
		}
		selected := p.worktrees[p.cursor]
		if selected.Path == p.currentPath {
			// Already on this worktree — dismiss without doing anything.
			return dismiss()
		}
		return func() tea.Msg { return worktreeSelectedMsg{worktree: selected} }

	case "esc", "q":
		return dismiss()
	}

	return nil
}

// View renders the picker as a modal box. width is the total terminal width,
// used to horizontally centre the modal.
func (p *worktreePicker) View(termWidth, termHeight int) string {
	const modalWidth = 56

	var sb strings.Builder

	sb.WriteString(StyleModalTitle.Render("switch worktree") + "\n")

	if len(p.worktrees) == 0 {
		sb.WriteString(StyleModalHint.Render("no worktrees found"))
	} else {
		for i, wt := range p.worktrees {
			active := wt.Path == p.currentPath

			branch := wt.ShortBranch()
			if wt.IsBare {
				branch = "(bare)"
			}

			// Show a marker for the currently active worktree.
			marker := "  "
			if active {
				marker = "* "
			}

			label := fmt.Sprintf("%s%-20s  %s", marker, branch, shortenPath(wt.Path, 28))

			if i == p.cursor {
				sb.WriteString(StyleModalItemSelected.Render(label) + "\n")
			} else {
				sb.WriteString(StyleModalItem.Render(label) + "\n")
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(StyleModalHint.Render("↑/k up  ↓/j down  enter confirm  esc cancel"))

	modal := StyleModal.Width(modalWidth).Render(sb.String())

	// Centre the modal both horizontally and vertically.
	modalLines := strings.Count(modal, "\n") + 1
	topPad := (termHeight - modalLines) / 2
	if topPad < 0 {
		topPad = 0
	}
	leftPad := (termWidth - lipgloss.Width(modal)) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	prefix := strings.Repeat("\n", topPad) + strings.Repeat(" ", leftPad)
	// Indent every line of the modal so it appears centred.
	indented := indentBlock(modal, strings.Repeat(" ", leftPad))

	_ = prefix
	return strings.Repeat("\n", topPad) + indented
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func dismiss() tea.Cmd {
	return func() tea.Msg { return worktreeDismissMsg{} }
}

// shortenPath truncates a filesystem path from the left to at most maxLen
// runes, prepending "…" when truncation occurs.
func shortenPath(path string, maxLen int) string {
	runes := []rune(path)
	if len(runes) <= maxLen {
		return path
	}
	return "…" + string(runes[len(runes)-(maxLen-1):])
}

// indentBlock prepends prefix to every line of a multi-line string.
func indentBlock(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
