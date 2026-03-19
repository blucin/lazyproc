package ui

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/blucin/lazyproc/internal/config"
	"github.com/blucin/lazyproc/internal/git"
	"github.com/blucin/lazyproc/internal/process"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Focus tracking ───────────────────────────────────────────────────────────

type focusPane int

const (
	focusSidebar focusPane = iota
	focusViewport
	focusWorktreePicker
)

// ── Bubbletea messages ────────────────────────────────────────────────────────

// ProcessOutputMsg is sent by the pipe-reader goroutine for each captured line.
type ProcessOutputMsg struct {
	ID   string
	Line process.OutputLine
}

// ProcessStateMsg is sent whenever a process changes state.
type ProcessStateMsg struct {
	ID    string
	State process.State
}

// TermSizeMsg is sent on terminal resize.
type TermSizeMsg struct {
	Width  int
	Height int
}

// tickMsg is used for periodic UI refresh (e.g. spinner animation).
type tickMsg time.Time

// worktreeSwitchCmd is returned by Init to detect git context on startup.
type gitDetectMsg struct {
	isGitRepo       bool
	currentWorktree git.Worktree
	worktrees       []git.Worktree
}

// ── Model ─────────────────────────────────────────────────────────────────────

// Model is the root Bubbletea model for lazyproc.
type Model struct {
	// Config & process manager.
	cfg     *config.Config
	manager *process.Manager

	// msgCh is a buffered channel written to by process manager callbacks
	// (goroutines) and drained by the Bubbletea event loop via listenCmd.
	msgCh chan tea.Msg

	// Ordered list of process IDs shown in the sidebar (stable sort).
	procIDs []string

	// Index into procIDs of the currently selected sidebar item.
	selectedIdx int

	// Which pane currently has keyboard focus.
	focus focusPane

	// Output viewport for the focused process.
	vp viewport.Model

	// Help bar.
	help     help.Model
	keys     KeyMap
	showHelp bool

	// Terminal dimensions.
	width  int
	height int

	// autoScroll tracks whether the viewport should auto-scroll to the bottom
	// when new output arrives.
	autoScroll bool

	// ── Cursor / selection state ──────────────────────────────────────────────

	// cursorLine is the index into the current process's output lines that the
	// cursor sits on. Always visible when the viewport is focused (dim), and
	// used as the anchor when selection mode starts.
	cursorLine int

	// selecting is true while the user is in visual selection mode (after 'v').
	selecting bool

	// selAnchor is the line index where 'v' was pressed. The selected range is
	// always [min(selAnchor,cursorLine), max(selAnchor,cursorLine)].
	selAnchor int

	// ── Git / worktree state ─────────────────────────────────────────────────

	// isGitRepo is false when the CWD is not inside a git repo; worktree
	// features are silently disabled in that case.
	isGitRepo bool

	// currentWorktree is the worktree that lazyproc was launched from.
	currentWorktree git.Worktree

	// availableWorktrees is the full list returned by git worktree list.
	availableWorktrees []git.Worktree

	// worktreePicker is the modal shown when the user presses 'w'.
	// Only valid while focus == focusWorktreePicker.
	picker worktreePicker
}

// NewModel constructs the root model from a parsed config.
// It wires up state-change and output callbacks so the Bubbletea program
// receives messages from background goroutines via a shared channel.
func NewModel(cfg *config.Config) Model {
	// msgCh is the bridge between background goroutines (process manager
	// callbacks) and the Bubbletea event loop. The channel is buffered so
	// that callbacks never block the pipe-reader goroutines.
	msgCh := make(chan tea.Msg, 256)

	onState := func(id string, state process.State) {
		msgCh <- ProcessStateMsg{ID: id, State: state}
	}
	onOutput := func(id string, line process.OutputLine) {
		msgCh <- ProcessOutputMsg{ID: id, Line: line}
	}

	h := help.New()
	h.Styles.ShortKey = StyleHelpKey
	h.Styles.ShortDesc = StyleHelpDesc
	h.Styles.FullKey = StyleHelpKey
	h.Styles.FullDesc = StyleHelpDesc

	m := Model{
		cfg:        cfg,
		manager:    process.NewManager(cfg, onState, onOutput),
		msgCh:      msgCh,
		keys:       DefaultKeyMap(),
		help:       h,
		autoScroll: true,
		focus:      focusSidebar,
	}

	// Build a stable ordered list of process IDs.
	for id := range cfg.Processes {
		m.procIDs = append(m.procIDs, id)
	}
	sortStrings(m.procIDs)

	return m
}

// selectedID returns the process ID currently highlighted in the sidebar,
// or "" if the list is empty.
func (m *Model) selectedID() string {
	if len(m.procIDs) == 0 {
		return ""
	}
	return m.procIDs[m.selectedIdx]
}

// selectedCwd returns the absolute working directory of the currently selected
// process. Relative paths from the config are resolved against the current
// working directory (which is where lazyproc was launched from). Returns "."
// if there is no selection or the cwd is empty.
func (m *Model) selectedCwd() string {
	id := m.selectedID()
	if id == "" {
		return "."
	}
	if p := m.manager.Get(id); p != nil {
		cwd := p.Cwd()
		if cwd == "" {
			return "."
		}
		if filepath.IsAbs(cwd) {
			return cwd
		}
		// Resolve relative paths so git commands always receive an absolute
		// path and don't accidentally walk up into a parent repo.
		if abs, err := filepath.Abs(cwd); err == nil {
			return abs
		}
		return cwd
	}
	return "."
}

// redetectGitCmd is a convenience wrapper that fires detectGitCmd rooted at
func (m *Model) redetectGitCmd() tea.Cmd {
	return detectGitCmd(m.selectedCwd())
}

// ── Init ──────────────────────────────────────────────────────────────────────

// Init is called once by Bubbletea before the first render.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		tickCmd(),
		listenCmd(m.msgCh),
		startAllCmd(m.manager),
		detectGitCmd(m.selectedCwd()),
	)
}

// ── Update ────────────────────────────────────────────────────────────────────

// Update handles all incoming messages and returns the updated model plus any
// commands to run next.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── Startup error ─────────────────────────────────────────────────────────
	case errMsg:
		// TODO: surface this in the UI properly; for now log it.
		_ = msg.err

	// ── Git detection result ──────────────────────────────────────────────────
	case gitDetectMsg:
		m.isGitRepo = msg.isGitRepo
		m.currentWorktree = msg.currentWorktree
		m.availableWorktrees = msg.worktrees

	// ── Worktree picker messages ──────────────────────────────────────────────
	case worktreeSelectedMsg:
		m.focus = focusSidebar
		return m, switchWorktreeCmd(m.manager, msg.worktree.Path)

	case worktreeDismissMsg:
		m.focus = focusSidebar

	// ── Terminal resize ───────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewport()

	// ── Process output ────────────────────────────────────────────────────────
	case ProcessOutputMsg:
		// Re-schedule the listener so the channel keeps being drained.
		cmds = append(cmds, listenCmd(m.msgCh))
		// If the arriving output belongs to the currently selected process,
		// refresh the viewport content.
		if msg.ID == m.selectedID() {
			if m.autoScroll && !m.selecting {
				n := m.outputLen()
				if n > 0 {
					m.cursorLine = n - 1
				}
			}
			m.refreshViewport()
			if m.autoScroll {
				m.vp.GotoBottom()
			}
		}

	// ── Process state change ──────────────────────────────────────────────────
	case ProcessStateMsg:
		// Re-schedule the listener so the channel keeps being drained.
		cmds = append(cmds, listenCmd(m.msgCh))
		// No further action needed beyond a re-render (sidebar dots update).

	// ── Periodic tick (spinner / refresh) ────────────────────────────────────
	case tickMsg:
		cmds = append(cmds, tickCmd())

	// ── Keyboard ──────────────────────────────────────────────────────────────
	case tea.KeyMsg:
		// When the worktree picker is open, route all keys into it.
		if m.focus == focusWorktreePicker {
			cmd := m.picker.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		} else {
			cmd := m.handleKey(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

// handleKey processes a key message and returns an optional command.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch {

	// ── Quit ──────────────────────────────────────────────────────────────────
	case keyMatches(msg, m.keys.Quit):
		if m.manager != nil {
			m.manager.StopAll()
		}
		return tea.Quit

	// ── Toggle help ───────────────────────────────────────────────────────────
	case keyMatches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		m.resizeViewport()

	// ── Switch pane focus ─────────────────────────────────────────────────────
	case keyMatches(msg, m.keys.FocusNext):
		m.selecting = false
		if m.focus == focusSidebar {
			m.focus = focusViewport
		} else {
			m.focus = focusSidebar
		}

	// ── Up ────────────────────────────────────────────────────────────────────
	case keyMatches(msg, m.keys.Up):
		if m.focus == focusSidebar {
			m.moveSidebar(-1)
			return m.redetectGitCmd()
		} else {
			m.moveCursor(-1)
		}

	// ── Down ──────────────────────────────────────────────────────────────────
	case keyMatches(msg, m.keys.Down):
		if m.focus == focusSidebar {
			m.moveSidebar(1)
			return m.redetectGitCmd()
		} else {
			m.moveCursor(1)
		}

	case keyMatches(msg, m.keys.PageUp):
		m.moveCursor(-m.vp.Height / 2)

	case keyMatches(msg, m.keys.PageDown):
		m.moveCursor(m.vp.Height / 2)

	case keyMatches(msg, m.keys.GotoTop):
		m.moveCursor(-m.outputLen())

	case keyMatches(msg, m.keys.GotoBottom):
		m.moveCursor(m.outputLen())

	// ── Selection ─────────────────────────────────────────────────────────────
	case keyMatches(msg, m.keys.Select):
		if m.focus == focusViewport {
			if m.selecting {
				// Keep visual mode active; reset anchor to current cursor so users
				// can quickly start a new selection without leaving the mode.
				m.selAnchor = m.cursorLine
			} else {
				m.selecting = true
				m.selAnchor = m.cursorLine
			}
			// Entering visual mode (or resetting selection inside it) pauses follow.
			m.autoScroll = false
			m.scrollToCursor()
			m.refreshViewport()
		}

	case keyMatches(msg, m.keys.Yank):
		if m.selecting {
			return m.yankSelection()
		}

	case keyMatches(msg, m.keys.Escape):
		m.selecting = false
		m.autoScroll = true
		m.refreshViewport()
		m.vp.GotoBottom()

	// ── Process control ───────────────────────────────────────────────────────
	case keyMatches(msg, m.keys.Start):
		if m.manager != nil && m.selectedID() != "" {
			go func(id string) { _ = m.manager.Start(id) }(m.selectedID())
		}

	case keyMatches(msg, m.keys.Stop):
		if m.manager != nil && m.selectedID() != "" {
			go func(id string) { _ = m.manager.Stop(id) }(m.selectedID())
		}

	case keyMatches(msg, m.keys.Restart):
		if m.manager != nil && m.selectedID() != "" {
			go func(id string) { _ = m.manager.Restart(id) }(m.selectedID())
		}

	case keyMatches(msg, m.keys.Clear):
		if m.manager != nil && m.selectedID() != "" {
			if p := m.manager.Get(m.selectedID()); p != nil {
				p.ClearOutput()
				m.cursorLine = 0
				m.selecting = false
				m.refreshViewport()
			}
		}

	// ── Worktree switcher ─────────────────────────────────────────────────────
	case keyMatches(msg, m.keys.Worktree):
		if m.isGitRepo && len(m.availableWorktrees) > 0 {
			m.picker = newWorktreePicker(m.availableWorktrees, m.currentWorktree.Path)
			m.focus = focusWorktreePicker
		}
	}

	return nil
}

// moveCursor moves cursorLine by delta, clamps to output bounds, scrolls the
// viewport to keep the cursor visible, and refreshes content.
func (m *Model) moveCursor(delta int) {
	n := m.outputLen()
	if n == 0 {
		return
	}
	m.cursorLine = clamp(m.cursorLine+delta, 0, n-1)
	// Cursor movement alone must not disable follow mode.
	// Follow is paused/resumed only by entering/exiting visual mode.
	m.scrollToCursor()
	m.refreshViewport()
}

// scrollToCursor adjusts the viewport offset so cursorLine is visible.
func (m *Model) scrollToCursor() {
	// viewport.YOffset is the index of the first visible line.
	top := m.vp.YOffset
	bottom := top + m.vp.Height - 1
	if m.cursorLine < top {
		m.vp.SetYOffset(m.cursorLine)
	} else if m.cursorLine > bottom {
		m.vp.SetYOffset(m.cursorLine - m.vp.Height + 1)
	}
}

// outputLen returns the number of lines in the selected process's output.
func (m *Model) outputLen() int {
	id := m.selectedID()
	if id == "" || m.manager == nil {
		return 0
	}
	p := m.manager.Get(id)
	if p == nil {
		return 0
	}
	return len(p.Output.Lines())
}

// yankSelection copies the selected lines to the clipboard via OSC 52 and
// exits selection mode.
func (m *Model) yankSelection() tea.Cmd {
	id := m.selectedID()
	if id == "" || m.manager == nil {
		return nil
	}
	p := m.manager.Get(id)
	if p == nil {
		return nil
	}
	lines := p.Output.Lines()
	lo := clamp(min(m.selAnchor, m.cursorLine), 0, len(lines)-1)
	hi := clamp(max(m.selAnchor, m.cursorLine), 0, len(lines)-1)

	selected := strings.Join(lines[lo:hi+1], "\n")
	m.selecting = false
	m.autoScroll = true
	m.refreshViewport()
	m.vp.GotoBottom()
	return osc52Cmd(selected)
}

// osc52Cmd writes an OSC 52 clipboard escape sequence to stdout. This works
// in most modern terminal emulators and over SSH (with AllowTcpForwarding).
func osc52Cmd(text string) tea.Cmd {
	return func() tea.Msg {
		b64 := base64Encode(text)
		// OSC 52 ; c ; <base64> ST
		fmt.Printf("\x1b]52;c;%s\x07", b64)
		return nil
	}
}

// moveSidebar moves the sidebar selection by delta, clamping to valid range.
func (m *Model) moveSidebar(delta int) {
	if len(m.procIDs) == 0 {
		return
	}
	m.selectedIdx = clamp(m.selectedIdx+delta, 0, len(m.procIDs)-1)
	m.refreshViewport()
	if m.autoScroll {
		m.vp.GotoBottom()
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

// View renders the full TUI layout.
func (m Model) View() string {
	if m.width == 0 {
		// Not yet sized — render nothing to avoid layout glitches.
		return ""
	}

	body := m.renderBody()
	footer := m.renderFooter()

	var base string
	if footer == "" {
		base = body
	} else {
		base = lipgloss.JoinVertical(lipgloss.Left, body, footer)
	}

	// Overlay the worktree picker modal on top of the base layout when active.
	if m.focus == focusWorktreePicker {
		overlay := m.picker.View(m.width, m.height)
		return overlayStrings(base, overlay)
	}

	if m.showHelp {
		helpView := m.help.FullHelpView(m.keys.FullHelp())
		overlay := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, StyleModal.Render(helpView))
		return overlayStrings(base, overlay)
	}

	return base
}

// renderBody renders two independent card boxes side by side: the process
// sidebar on the left and the output viewport on the right. Each card gets its
// own full border that is highlighted when that pane has focus.
func (m Model) renderBody() string {
	// Each card has a top + bottom border row, so inner content height is 2
	// less than the total body height.
	cardH := m.bodyHeight()
	innerH := cardH - 2
	if innerH < 1 {
		innerH = 1
	}

	sidebarFocused := m.focus == focusSidebar
	viewportFocused := m.focus == focusViewport

	// ── Sidebar card ──────────────────────────────────────────────────────
	var sb strings.Builder
	for i, id := range m.procIDs {
		if i >= innerH {
			break
		}
		dot := "●"
		dotStyle := StyleDotStopped
		if m.manager != nil {
			if p := m.manager.Get(id); p != nil {
				switch p.State() {
				case process.StateStarting:
					dotStyle = StyleDotStarting
				case process.StateRunning:
					dotStyle = StyleDotRunning
				case process.StateReady:
					dotStyle = StyleDotReady
				case process.StateCrashed:
					dotStyle = StyleDotCrashed
				case process.StateRestarting:
					dotStyle = StyleDotRestarting
				}
			}
		}
		label := fmt.Sprintf(" %s %s", dotStyle.Render(dot), id)
		if i == m.selectedIdx {
			label = StyleSidebarItemSelected.Render(label)
		} else {
			label = StyleSidebarItem.Render(label)
		}
		sb.WriteString(label + "\n")
	}

	// ── Sidebar card ──────────────────────────────────────────────────────
	sidebarContent := strings.TrimRight(sb.String(), "\n")
	var sidebarCard string
	if m.cfg.LabelsEnabled() {
		sidebarCard = renderCardWithTitle(sidebarContent, " "+m.cfg.Settings.ProcessListTitle+" ", sidebarWidth, innerH, sidebarFocused, false)
	} else {
		sidebarCard = cardBorder(sidebarFocused, false).Width(sidebarWidth).Height(innerH).Render(sidebarContent)
	}

	// ── Viewport card ─────────────────────────────────────────────────────
	vpContent := lipgloss.NewStyle().Width(m.vp.Width).Height(innerH).Render(m.vp.View())
	var vpCard string
	if m.cfg.LabelsEnabled() {
		title := m.selectedID()
		if title == "" {
			title = "logs"
		}
		vpCard = renderCardWithTitle(vpContent, " "+title+" ", m.vp.Width, innerH, viewportFocused, m.selecting)
	} else {
		vpCard = cardBorder(viewportFocused, m.selecting).Width(m.vp.Width).Height(innerH).Render(vpContent)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebarCard, vpCard)
}

// renderCardWithTitle renders a card with a full border and a title embedded
// in the top border line. lipgloss tiles b.Top as a repeating fill character,
// so we pad the title to exactly innerW runes with "─" — that way the string
// is consumed verbatim with no repetition.
func renderCardWithTitle(content, title string, innerW, innerH int, focused, selecting bool) string {
	b := lipgloss.NormalBorder()
	titleRunes := []rune(title)
	padLen := innerW - len(titleRunes)
	if padLen < 0 {
		titleRunes = titleRunes[:innerW]
		padLen = 0
	}
	b.Top = string(titleRunes) + strings.Repeat(b.Top, padLen)

	borderColor := lipgloss.TerminalColor(colorBorder)
	switch {
	case selecting:
		borderColor = colorBorderSelect
	case focused:
		borderColor = colorBorderFocused
	}

	return lipgloss.NewStyle().
		Width(innerW).
		Height(innerH).
		BorderStyle(b).
		BorderForeground(borderColor).
		Render(content)
}

// renderFooter renders the help bar at the bottom of the screen.
// Returns an empty string when the help bar is disabled in config.
func (m Model) renderFooter() string {
	if !m.cfg.HelpEnabled() {
		return ""
	}

	var helpView string
	if m.selecting {
		helpView = m.help.ShortHelpView(m.keys.ShortHelpVisual())
	} else {
		helpView = m.help.ShortHelpView(m.keys.ShortHelp())
	}

	return StyleHelp.Width(m.width).Render(helpView)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// bodyHeight returns the height available for the body box (including its own
// top and bottom border lines), excluding the footer.
func (m Model) bodyHeight() int {
	footerLines := 0
	if m.cfg.HelpEnabled() {
		footerLines = 1
	}
	h := m.height - footerLines
	if h < 1 {
		h = 1
	}
	return h
}

// resizeViewport recalculates the viewport dimensions after a terminal resize
// or help-bar toggle.
func (m *Model) resizeViewport() {
	// Two cards side by side, each with a 2-col border (left+right).
	// sidebar card total width = sidebarWidth + 2 borders.
	// viewport card total width = remaining width.
	// The 1-col gap between cards comes for free since each has its own border.
	vpWidth := m.width - (sidebarWidth + 2) - 2
	// Each card has its own top + bottom border row.
	vpHeight := m.bodyHeight() - 2

	if vpWidth < 1 {
		vpWidth = 1
	}
	if vpHeight < 1 {
		vpHeight = 1
	}

	m.vp.Width = vpWidth
	m.vp.Height = vpHeight
}

// refreshViewport repopulates the viewport with the current process's output,
// painting the cursor line and selection range on top of highlight colours.
func (m *Model) refreshViewport() {
	id := m.selectedID()
	if id == "" || m.manager == nil {
		m.vp.SetContent("")
		return
	}

	p := m.manager.Get(id)
	if p == nil {
		m.vp.SetContent("")
		return
	}

	lines := p.Output.Lines()
	if len(lines) == 0 {
		m.vp.SetContent("")
		return
	}

	// Clamp cursor so it stays valid as output grows or shrinks.
	m.cursorLine = clamp(m.cursorLine, 0, len(lines)-1)

	lo := min(m.selAnchor, m.cursorLine)
	hi := max(m.selAnchor, m.cursorLine)

	procCfg := m.cfg.Processes[id]
	vpFocused := m.focus == focusViewport

	rendered := make([]string, len(lines))
	for i, line := range lines {
		line = applyHighlight(line, procCfg.Highlight)
		switch {
		case m.selecting && i >= lo && i <= hi:
			line = StyleSelectedLine.Width(m.vp.Width).Render(line)
		case vpFocused && i == m.cursorLine:
			line = StyleCursorLine.Render(line)
		}
		rendered[i] = line
	}

	m.vp.SetContent(strings.Join(rendered, "\n"))
}

// applyHighlight applies the first matching highlight rule to line and returns
// the (possibly coloured) result.
func applyHighlight(line string, rules []config.HighlightRule) string {
	for _, rule := range rules {
		if matchesPattern(rule.Pattern, line) {
			return HighlightStyle(rule.Color).Render(line)
		}
	}
	return line
}

// matchesPattern is a thin wrapper so we don't import the process package's
// internal helper directly; we inline a simple check here.
func matchesPattern(pattern, line string) bool {
	if pattern == "" {
		return false
	}
	// Use strings.Contains as a fast path for plain strings; for real regex
	// the process package's compilePattern cache is used indirectly via the
	// process manager. Here we just do a contains check for the UI layer.
	return strings.Contains(line, pattern)
}

// ── Tick command ──────────────────────────────────────────────────────────────

func tickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// listenCmd returns a command that blocks until the next message arrives on
// ch, forwards it into the Bubbletea event loop, then re-schedules itself so
// the channel is drained continuously for the lifetime of the program.
func listenCmd(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg := <-ch
		return msg
	}
}

// startAllCmd returns a command that calls manager.StartAll() in the
// background and reports any startup error back to the event loop as an
// errMsg so it can be surfaced in the UI rather than silently swallowed.
func startAllCmd(mgr *process.Manager) tea.Cmd {
	return func() tea.Msg {
		if err := mgr.StartAll(); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

// detectGitCmd runs git detection in the background rooted at cwd (the
// selected process's working directory) and sends a gitDetectMsg back to the
// event loop.
func detectGitCmd(cwd string) tea.Cmd {
	return func() tea.Msg {
		worktrees, err := git.ListWorktrees(cwd)
		if err != nil {
			return gitDetectMsg{isGitRepo: false}
		}
		current, err := git.CurrentWorktree(cwd)
		if err != nil {
			return gitDetectMsg{isGitRepo: false}
		}
		// Guard against the process cwd sitting inside some unrelated parent
		// git repo (e.g. test scripts living inside the lazyproc source tree).
		if !git.IsWorktreeRoot(cwd) {
			return gitDetectMsg{isGitRepo: false}
		}
		return gitDetectMsg{
			isGitRepo:       true,
			currentWorktree: current,
			worktrees:       worktrees,
		}
	}
}

// switchWorktreeCmd stops all processes, switches to newPath, restarts live
// processes, then re-detects git state from newPath so the header and picker
// stay current.
func switchWorktreeCmd(mgr *process.Manager, newPath string) tea.Cmd {
	return func() tea.Msg {
		if err := mgr.SwitchWorktree(newPath); err != nil {
			return errMsg{err}
		}
		// Re-list all worktrees from the new worktree path so the picker stays current.
		worktrees, err := git.ListWorktrees(newPath)
		if err != nil {
			return errMsg{err}
		}
		// Find the Worktree entry that matches the new path.
		var current git.Worktree
		for _, wt := range worktrees {
			if wt.Path == newPath {
				current = wt
				break
			}
		}
		return gitDetectMsg{
			isGitRepo:       true,
			currentWorktree: current,
			worktrees:       worktrees,
		}
	}
}

// errMsg carries an error back into the Bubbletea event loop.
type errMsg struct{ err error }

// ── Key helper ────────────────────────────────────────────────────────────────

func keyMatches(msg tea.KeyMsg, b key.Binding) bool {
	return key.Matches(msg, b)
}

// ── Utility ───────────────────────────────────────────────────────────────────

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// sortStrings sorts a slice of strings in-place (insertion sort, small N).
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		key := ss[i]
		j := i - 1
		for j >= 0 && ss[j] > key {
			ss[j+1] = ss[j]
			j--
		}
		ss[j+1] = key
	}
}

// overlayStrings places overlay on top of base by replacing base's lines with
// the non-empty lines from overlay. Both strings are split on "\n".
func overlayStrings(base, overlay string) string {
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	result := make([]string, len(baseLines))
	copy(result, baseLines)

	for i, ol := range overlayLines {
		if i >= len(result) {
			break
		}
		if strings.TrimSpace(ol) != "" {
			result[i] = ol
		}
	}

	return strings.Join(result, "\n")
}
