package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/blucin/lazyproc/internal/config"
	"github.com/blucin/lazyproc/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Getenv("DEBUG")) > 0 {
		f, err := tea.LogToFile("debug.log", "debug")
		if err != nil {
			fmt.Println("fatal:", err)
			os.Exit(1)
		}
		defer f.Close()
	}

	configPath := flag.String("config", "lazyproc.yaml", "path to config file")
	title := flag.String("title", "", "title for the process list")
	labels := flag.String("labels", "", "comma-separated labels for the processes")
	flag.Parse()

	var cfg *config.Config
	var err error

	if len(flag.Args()) > 0 {
		var labelSlice []string
		if *labels != "" {
			labelSlice = strings.Split(*labels, ",")
			for i := range labelSlice {
				labelSlice[i] = strings.TrimSpace(labelSlice[i])
			}
		}
		cfg, err = config.FromArgs(flag.Args(), labelSlice, *title)
	} else {
		cfg, err = config.Load(*configPath)
		if err == nil && *title != "" {
			cfg.Settings.ProcessListTitle = *title
		}
	}

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
