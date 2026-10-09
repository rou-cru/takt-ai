package gc

import (
	"fmt"
	"time"
)

// Default execution timeouts when gc.json omits them.
const (
	// DefaultCheckTimeout bounds one acceptance check command.
	DefaultCheckTimeout = 15 * time.Minute
	// DefaultAnalyzerTimeout bounds one analyzer run.
	DefaultAnalyzerTimeout = 10 * time.Minute
	// DefaultVersionTimeout bounds one tool-version probe.
	DefaultVersionTimeout = 30 * time.Second
	// DefaultASTTimeout bounds one AST extraction pass.
	DefaultASTTimeout = 30 * time.Second
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

// durationOrDefault falls back when the value is empty; malformed values use
// the fallback too because callers validate config first.
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
