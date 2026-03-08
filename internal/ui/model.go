package ui

import (
	"fmt"
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

	m := Model{
		cfg:        cfg,
		manager:    process.NewManager(cfg, onState, onOutput),
		msgCh:      msgCh,
		keys:       DefaultKeyMap(),
		help:       help.New(),
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

// ── Init ──────────────────────────────────────────────────────────────────────

// Init is called once by Bubbletea before the first render.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		tickCmd(),
		listenCmd(m.msgCh),
		startAllCmd(m.manager),
		detectGitCmd(),
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

	// Forward remaining messages to the viewport when it has focus so
	// bubbles/viewport can handle its own scroll state.
	if m.focus == focusViewport {
		var vpCmd tea.Cmd
		m.vp, vpCmd = m.vp.Update(msg)
		if vpCmd != nil {
			cmds = append(cmds, vpCmd)
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
		if m.focus == focusSidebar {
			m.focus = focusViewport
		} else {
			m.focus = focusSidebar
		}

	// ── Sidebar navigation ────────────────────────────────────────────────────
	case keyMatches(msg, m.keys.Up):
		if m.focus == focusSidebar {
			m.moveSidebar(-1)
		} else {
			m.vp.LineUp(1)
			m.autoScroll = false
		}

	case keyMatches(msg, m.keys.Down):
		if m.focus == focusSidebar {
			m.moveSidebar(1)
		} else {
			m.vp.LineDown(1)
			// Re-enable auto-scroll when the user scrolls to the bottom.
			if m.vp.AtBottom() {
				m.autoScroll = true
			}
		}

	case keyMatches(msg, m.keys.PageUp):
		m.vp.HalfViewUp()
		m.autoScroll = false

	case keyMatches(msg, m.keys.PageDown):
		m.vp.HalfViewDown()
		if m.vp.AtBottom() {
			m.autoScroll = true
		}

	case keyMatches(msg, m.keys.GotoTop):
		m.vp.GotoTop()
		m.autoScroll = false

	case keyMatches(msg, m.keys.GotoBottom):
		m.vp.GotoBottom()
		m.autoScroll = true

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

	return base
}

// renderBody renders the main area: a single outer border box containing the
// sidebar (right-border only as divider) and the viewport side by side.
func (m Model) renderBody() string {
	innerH := m.bodyHeight() - 2 // subtract outer top + bottom border
	if innerH < 1 {
		innerH = 1
	}

	// ── Sidebar ───────────────────────────────────────────────────────────
	// Right border acts as the vertical divider between the two panes.
	sidebarStyle := lipgloss.NewStyle().
		Width(sidebarWidth).
		Height(innerH).
		BorderStyle(lipgloss.NormalBorder()).
		BorderRight(true).
		BorderForeground(colorBorder)

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
	sidebar := sidebarStyle.Render(strings.TrimRight(sb.String(), "\n"))

	// ── Viewport ──────────────────────────────────────────────────────────
	// No border of its own — the outer box provides top/bottom/right edges.
	viewport := lipgloss.NewStyle().
		Width(m.vp.Width).
		Height(innerH).
		Render(m.vp.View())

	// ── Outer box ─────────────────────────────────────────────────────────
	// Wraps sidebar+viewport in a single border. When labels are enabled we
	// build a custom Border whose Top string embeds the pane titles so that
	// no content rows are consumed by labels.
	outerW := m.width - 2 // subtract outer left + right border
	inner := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, viewport)
	b := m.buildBorder(outerW)
	outer := lipgloss.NewStyle().
		Width(outerW).
		BorderStyle(b).
		BorderForeground(colorBorder).
		Render(inner)

	return outer
}

// buildBorder returns a lipgloss.Border for the outer body box.
// When labels are enabled the Top field is replaced with a pre-built string
// that embeds "─ processes ─┬─ <procname> ─" into the top border line.
// lipgloss uses the Top string as a tile source — because our string is
// already exactly outerW runes wide it is used verbatim with no tiling.
func (m Model) buildBorder(outerW int) lipgloss.Border {
	b := lipgloss.NormalBorder()
	if !m.cfg.LabelsEnabled() {
		return b
	}

	leftW := sidebarWidth // includes the divider │ column
	rightW := outerW - leftW

	// Left segment: "─ processes ─────" padded to leftW runes.
	const sidebarTitle = " processes "
	leftSeg := buildTitleSegment(sidebarTitle, leftW, b.Top)

	// Right segment: " <procname> ─────" padded to rightW runes.
	procName := m.selectedID()
	if procName == "" {
		procName = "logs"
	}
	rightSeg := buildTitleSegment(" "+procName+" ", rightW, b.Top)

	b.Top = leftSeg + rightSeg
	return b
}

// buildTitleSegment builds a border segment of exactly width runes consisting
// of a title string centred (left-biased) in a field of fill characters.
func buildTitleSegment(title string, width int, fill string) string {
	titleW := len([]rune(title))
	dashW := width - titleW
	if dashW < 0 {
		// Title longer than segment — truncate.
		return string([]rune(title)[:width])
	}
	left := dashW / 2
	right := dashW - left
	return strings.Repeat(fill, left) + title + strings.Repeat(fill, right)
}

// renderFooter renders the help bar at the bottom of the screen.
// Returns an empty string when the help bar is disabled in config.
func (m Model) renderFooter() string {
	if !m.cfg.HelpEnabled() {
		return ""
	}
	var helpView string
	if m.showHelp {
		helpView = m.help.View(m.keys)
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
		if m.showHelp {
			footerLines = 4
		} else {
			footerLines = 1
		}
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
	// Outer box: 2 border cols (left+right).
	// Sidebar: sidebarWidth cols + 1 divider border col.
	vpWidth := m.width - sidebarWidth - 3
	// Outer box: 2 border rows (top+bottom).
	// Labels are in the border itself — no content rows deducted.
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

// refreshViewport repopulates the viewport with the current process's output.
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

	// Apply highlight rules.
	procCfg := m.cfg.Processes[id]
	rendered := make([]string, len(lines))
	for i, line := range lines {
		rendered[i] = applyHighlight(line, procCfg.Highlight)
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

// detectGitCmd runs git detection in the background and sends a gitDetectMsg
// back to the event loop. It never returns an error — if git is unavailable
// or CWD is not a repo, isGitRepo is simply false.
func detectGitCmd() tea.Cmd {
	return func() tea.Msg {
		worktrees, err := git.ListWorktrees(".")
		if err != nil {
			return gitDetectMsg{isGitRepo: false}
		}
		current, err := git.CurrentWorktree(".")
		if err != nil {
			return gitDetectMsg{isGitRepo: false}
		}
		return gitDetectMsg{
			isGitRepo:       true,
			currentWorktree: current,
			worktrees:       worktrees,
		}
	}
}

// switchWorktreeCmd stops all processes, switches the worktree CWD, restarts
// live processes, then returns a gitDetectMsg so Update can update the model's
// git state cleanly without any shared-memory mutation from a goroutine.
func switchWorktreeCmd(mgr *process.Manager, newPath string) tea.Cmd {
	return func() tea.Msg {
		if err := mgr.SwitchWorktree(newPath); err != nil {
			return errMsg{err}
		}
		// Re-list all worktrees from the new path so the picker stays current.
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
