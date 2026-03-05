package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/blucin/lazyproc/internal/config"
	"github.com/blucin/lazyproc/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	configPath := flag.String("config", "lazyproc.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	m := ui.NewModel(cfg)

	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running program: %v\n", err)
		os.Exit(1)
	}
}
