package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// HighlightRule defines a regex pattern and its associated color for output highlighting.
type HighlightRule struct {
	Pattern string `yaml:"pattern"`
	Color   string `yaml:"color"`
}

// ReadyWhen defines the condition under which a process is considered ready.
type ReadyWhen struct {
	Stdout string `yaml:"stdout"`
}

// Process holds the configuration for a single managed process.
type Process struct {
	Cmd       string          `yaml:"cmd"`
	Cwd       string          `yaml:"cwd"`
	DependsOn []string        `yaml:"depends_on"`
	ReadyWhen ReadyWhen       `yaml:"ready_when"`
	Highlight []HighlightRule `yaml:"highlight"`
	EnvFile   string          `yaml:"env_file"`
}

// Settings holds global lazyproc settings.
type Settings struct {
	LogLimit int    `yaml:"log_limit"`
	Shell    string `yaml:"shell"`
	// ShowHelp controls whether the help bar is rendered at the bottom.
	// Defaults to true when omitted from config.
	ShowHelp *bool `yaml:"show_help"`
	// ShowLabels controls whether the "processes" / process-name label row
	// is rendered inside each pane. Defaults to true when omitted from config.
	ShowLabels *bool `yaml:"show_labels"`
}

// Config is the top-level structure parsed from lazyproc.yaml.
type Config struct {
	Settings  Settings           `yaml:"settings"`
	Processes map[string]Process `yaml:"processes"`
}

// defaults applies sensible default values to a Config after parsing.
func (c *Config) defaults() {
	if c.Settings.LogLimit == 0 {
		c.Settings.LogLimit = 10000
	}
	if c.Settings.Shell == "" {
		c.Settings.Shell = "/bin/sh"
	}
	if c.Settings.ShowHelp == nil {
		t := true
		c.Settings.ShowHelp = &t
	}
	if c.Settings.ShowLabels == nil {
		t := true
		c.Settings.ShowLabels = &t
	}
}

// validate performs basic sanity checks on the parsed config.
func (c *Config) validate() error {
	for name, proc := range c.Processes {
		if proc.Cmd == "" {
			return fmt.Errorf("process %q: cmd must not be empty", name)
		}
		for _, dep := range proc.DependsOn {
			if _, ok := c.Processes[dep]; !ok {
				return fmt.Errorf("process %q: depends_on references unknown process %q", name, dep)
			}
		}
	}
	return nil
}

// HelpEnabled reports whether the help bar should be shown.
func (c *Config) HelpEnabled() bool {
	return c.Settings.ShowHelp != nil && *c.Settings.ShowHelp
}

// LabelsEnabled reports whether pane labels should be shown.
func (c *Config) LabelsEnabled() bool {
	return c.Settings.ShowLabels != nil && *c.Settings.ShowLabels
}

// Load reads and parses a lazyproc.yaml config file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	cfg.defaults()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}
