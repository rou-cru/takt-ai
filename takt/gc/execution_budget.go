package gc

import (
	"fmt"
	"time"
)

// Default execution timeouts when gc.json omits them: checks get the largest
// budget, analyzers a bit less, and version/AST probes the least.
const (
	DefaultCheckTimeout    = 15 * time.Minute
	DefaultAnalyzerTimeout = 10 * time.Minute
	DefaultVersionTimeout  = 30 * time.Second
	DefaultASTTimeout      = 30 * time.Second
)

func validateTimeouts(cfg ProjectConfig) error {
	for _, value := range []string{cfg.CheckTimeout, cfg.AnalyzerTimeout} {
		if value == "" {
			continue
		}
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return fmt.Errorf("gc: invalid execution timeout %q; use a positive duration", value)
		}
	}
	return nil
}

// Callers validate config first; empty values in old gc.json/state use defaults.
func durationOrDefault(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	d, _ := time.ParseDuration(value)
	return d
}

func analyzerTimeout(cfg ProjectConfig) time.Duration {
	return durationOrDefault(cfg.AnalyzerTimeout, DefaultAnalyzerTimeout)
}
