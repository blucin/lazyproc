package ui

import "github.com/charmbracelet/lipgloss"

// Palette — base colours used throughout the UI.
// Using adaptive colours so the UI looks reasonable on both light and dark
// terminals.
var (
	colorPrimary   = lipgloss.AdaptiveColor{Light: "#1A1A2E", Dark: "#CDD6F4"}
	colorSubtle    = lipgloss.AdaptiveColor{Light: "#666666", Dark: "#6C7086"}
	colorHighlight = lipgloss.AdaptiveColor{Light: "#0066CC", Dark: "#89B4FA"}
	colorBorder    = lipgloss.AdaptiveColor{Light: "#CCCCCC", Dark: "#313244"}

	// Status dot colours.
	colorStopped    = lipgloss.AdaptiveColor{Light: "#888888", Dark: "#6C7086"}
	colorStarting   = lipgloss.AdaptiveColor{Light: "#CC8800", Dark: "#F9E2AF"}
	colorRunning    = lipgloss.AdaptiveColor{Light: "#0088CC", Dark: "#89B4FA"}
	colorReady      = lipgloss.AdaptiveColor{Light: "#008800", Dark: "#A6E3A1"}
	colorCrashed    = lipgloss.AdaptiveColor{Light: "#CC0000", Dark: "#F38BA8"}
	colorRestarting = lipgloss.AdaptiveColor{Light: "#AA5500", Dark: "#FAB387"}
)

// ── Layout dimensions ───────────────────────────────────────────────────────

const (
	sidebarWidth  = 28
	headerHeight  = 1
	footerHeight  = 1
	statusDotRune = "●"
)

// ── Sidebar ──────────────────────────────────────────────────────────────────

var (
	StyleSidebarLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorHighlight).
				Padding(0, 1)

	StyleSidebarItem = lipgloss.NewStyle().
				Padding(0, 1)

	StyleSidebarItemSelected = lipgloss.NewStyle().
					Padding(0, 1).
					Background(colorHighlight).
					Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1E1E2E"}).
					Bold(true)
)

// ── Viewport label ────────────────────────────────────────────────────────────

var StyleViewportLabel = lipgloss.NewStyle().
	Bold(true).
	Foreground(colorSubtle).
	Padding(0, 1)

// ── Status dots ──────────────────────────────────────────────────────────────

var (
	StyleDotStopped    = lipgloss.NewStyle().Foreground(colorStopped)
	StyleDotStarting   = lipgloss.NewStyle().Foreground(colorStarting)
	StyleDotRunning    = lipgloss.NewStyle().Foreground(colorRunning)
	StyleDotReady      = lipgloss.NewStyle().Foreground(colorReady)
	StyleDotCrashed    = lipgloss.NewStyle().Foreground(colorCrashed)
	StyleDotRestarting = lipgloss.NewStyle().Foreground(colorRestarting)
)

// ── Output line highlight colours ────────────────────────────────────────────
// These are used by the viewport renderer to colour-match lines according to
// the per-process highlight rules defined in the config. The mapping below
// covers the colour names accepted in the YAML config; anything not found
// here is tried as a raw hex value by lipgloss.

var highlightColorMap = map[string]lipgloss.TerminalColor{
	"red":     lipgloss.Color("#F38BA8"),
	"green":   lipgloss.Color("#A6E3A1"),
	"yellow":  lipgloss.Color("#F9E2AF"),
	"blue":    lipgloss.Color("#89B4FA"),
	"magenta": lipgloss.Color("#CBA6F7"),
	"cyan":    lipgloss.Color("#89DCEB"),
	"white":   lipgloss.Color("#CDD6F4"),
	"orange":  lipgloss.Color("#FAB387"),
	"gray":    lipgloss.Color("#6C7086"),
	"grey":    lipgloss.Color("#6C7086"),
}

// HighlightStyle returns a lipgloss Style that applies the named colour as a
// foreground. If the name is not in the palette map it is forwarded directly
// to lipgloss as a hex/ANSI colour string.
func HighlightStyle(colorName string) lipgloss.Style {
	if c, ok := highlightColorMap[colorName]; ok {
		return lipgloss.NewStyle().Foreground(c)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colorName))
}

// ── Footer / help bar ────────────────────────────────────────────────────────

var (
	StyleHelp = lipgloss.NewStyle().
			Foreground(colorSubtle).
			Padding(0, 1)

	StyleHelpKey = lipgloss.NewStyle().
			Foreground(colorHighlight).
			Bold(true)

	StyleHelpDesc = lipgloss.NewStyle().
			Foreground(colorSubtle)
)

// ── Worktree picker modal ─────────────────────────────────────────────────────

var (
	// StyleModal is the outer box of the worktree picker overlay.
	StyleModal = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorHighlight).
			Padding(0, 1)

	StyleModalTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorHighlight).
			Padding(0, 1)

	StyleModalItem = lipgloss.NewStyle().
			Padding(0, 1)

	StyleModalItemSelected = lipgloss.NewStyle().
				Padding(0, 1).
				Background(colorHighlight).
				Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1E1E2E"}).
				Bold(true)

	StyleModalHint = lipgloss.NewStyle().
			Foreground(colorSubtle).
			Padding(0, 1)
)
