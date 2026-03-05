package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/blucin/lazyproc/internal/config"
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

// ── Model ─────────────────────────────────────────────────────────────────────

// Model is the root Bubbletea model for lazyproc.
type Model struct {
	// Config & process manager.
	cfg     *config.Config
	manager *process.Manager

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
}

// NewModel constructs the root model from a parsed config.
// It wires up state-change and output callbacks so the Bubbletea program
// receives messages from background goroutines.
func NewModel(cfg *config.Config) Model {
	// We need a reference to the tea.Program to send messages, but at
	// construction time we don't have one yet. We store channels here and
	// inject them via WithProgram after tea.NewProgram is created.
	// For now the callbacks are left nil; they are set in Init via commands.

	m := Model{
		cfg:        cfg,
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
	)
}

// ── Update ────────────────────────────────────────────────────────────────────

// Update handles all incoming messages and returns the updated model plus any
// commands to run next.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── Terminal resize ───────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewport()

	// ── Process output ────────────────────────────────────────────────────────
	case ProcessOutputMsg:
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
		// Re-render sidebar to reflect the new state dot / label.
		// No explicit action needed beyond a re-render (return m).

	// ── Periodic tick (spinner / refresh) ────────────────────────────────────
	case tickMsg:
		cmds = append(cmds, tickCmd())

	// ── Keyboard ──────────────────────────────────────────────────────────────
	case tea.KeyMsg:
		cmd := m.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
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

	sidebar := m.renderSidebar()
	output := m.renderOutputPane()

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, output)

	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}

// renderSidebar returns the rendered sidebar string.
func (m Model) renderSidebar() string {
	var sb strings.Builder

	title := StyleSidebarTitle.Render("processes")
	sb.WriteString(title + "\n")

	for i, id := range m.procIDs {
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

		dotStr := dotStyle.Render(dot)
		label := fmt.Sprintf(" %s %s", dotStr, id)

		if i == m.selectedIdx {
			label = StyleSidebarItemSelected.Render(label)
		} else {
			label = StyleSidebarItem.Render(label)
		}

		sb.WriteString(label + "\n")
	}

	sidebarHeight := m.bodyHeight()
	content := sb.String()

	return StyleSidebar.
		Height(sidebarHeight).
		Render(content)
}

// renderOutputPane returns the viewport pane as a string.
func (m Model) renderOutputPane() string {
	id := m.selectedID()
	title := StyleViewportTitle.Render(fmt.Sprintf("output: %s", id))

	vpWidth := m.width - sidebarWidth - 2 // -2 for border
	if vpWidth < 1 {
		vpWidth = 1
	}

	pane := lipgloss.JoinVertical(lipgloss.Left,
		title,
		m.vp.View(),
	)

	borderStyle := StyleDim
	if m.focus == focusViewport {
		borderStyle = StyleFocused
	}

	return borderStyle.Width(vpWidth).Render(pane)
}

// renderFooter renders the help bar at the bottom of the screen.
func (m Model) renderFooter() string {
	var helpView string
	if m.showHelp {
		helpView = m.help.View(m.keys)
	} else {
		helpView = m.help.ShortHelpView(m.keys.ShortHelp())
	}
	return StyleHelp.Width(m.width).Render(helpView)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// bodyHeight returns the height available for the sidebar + viewport body,
// excluding the footer.
func (m Model) bodyHeight() int {
	footerLines := 1
	if m.showHelp {
		footerLines = 4
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
	vpWidth := m.width - sidebarWidth - 4 // sidebar + borders
	vpHeight := m.bodyHeight() - 2        // title row + border

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
