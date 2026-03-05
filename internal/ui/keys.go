package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all keybinding definitions for lazyproc.
// Using bubbles/key so bindings integrate cleanly with the bubbles/help bar.
type KeyMap struct {
	// ── Navigation ───────────────────────────────────────────────────────────
	Up         key.Binding
	Down       key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	GotoTop    key.Binding
	GotoBottom key.Binding

	// ── Pane focus ───────────────────────────────────────────────────────────
	FocusNext key.Binding // Tab — cycle focus between sidebar / viewport

	// ── Process control ──────────────────────────────────────────────────────
	Start   key.Binding // s — start focused process
	Stop    key.Binding // x — stop focused process
	Restart key.Binding // r — restart focused process
	Clear   key.Binding // c — clear output buffer of focused process

	// ── Worktree ─────────────────────────────────────────────────────────────
	Worktree key.Binding // w — open worktree switcher

	// ── Search (later stage) ─────────────────────────────────────────────────
	Search key.Binding // / — open search input

	// ── Application ──────────────────────────────────────────────────────────
	Quit key.Binding // q / ctrl+c
	Help key.Binding // ? — toggle full help
}

// DefaultKeyMap returns the default production keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "scroll up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "scroll down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup/ctrl+u", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdn/ctrl+d", "page down"),
		),
		GotoTop: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "top"),
		),
		GotoBottom: key.NewBinding(
			key.WithKeys("G"),
			key.WithHelp("G", "bottom"),
		),

		FocusNext: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch pane"),
		),

		Start: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "start"),
		),
		Stop: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "stop"),
		),
		Restart: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "restart"),
		),
		Clear: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "clear output"),
		),

		Worktree: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "switch worktree"),
		),

		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search output"),
		),

		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
	}
}

// ShortHelp returns the condensed set of bindings shown in the compact help bar.
// Implements the help.KeyMap interface from bubbles/help.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Up,
		k.Down,
		k.FocusNext,
		k.Restart,
		k.Quit,
		k.Help,
	}
}

// FullHelp returns the full set of bindings shown when the user presses '?'.
// Implements the help.KeyMap interface from bubbles/help.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		// Column 1 — navigation
		{
			k.Up,
			k.Down,
			k.PageUp,
			k.PageDown,
			k.GotoTop,
			k.GotoBottom,
		},
		// Column 2 — process control
		{
			k.Start,
			k.Stop,
			k.Restart,
			k.Clear,
		},
		// Column 3 — application
		{
			k.FocusNext,
			k.Worktree,
			k.Search,
			k.Quit,
			k.Help,
		},
	}
}
