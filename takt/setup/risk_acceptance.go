package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// RiskAcceptanceFilename records uncertain conflicts the user chose to keep.
// Re-asking stops only for identical content with identical impact; it never authorizes overwrites.
const RiskAcceptanceFilename = ".takt-accepted-risks.json"

// RiskAcceptance is one kept file as reviewed: its path, the SHA-256 of the
// content the user saw ("" for a missing file) and the assessed impact.
type RiskAcceptance struct {
	// Path is the kept file's root-relative path.
	Path string `json:"path"`
	// SHA256 is the digest of the reviewed content ("" when the file is missing).
	SHA256 string `json:"sha256"`
	// Impact is the assessed impact the user accepted.
	Impact string `json:"impact"`
}

// RecordRiskAcceptances upserts accepted into rootDir's record by path.
func RecordRiskAcceptances(rootDir string, accepted []RiskAcceptance) error {
	if len(accepted) == 0 {
		return nil
	}
	byPath := loadRiskAcceptances(rootDir)
	for _, acceptance := range accepted {
		byPath[acceptance.Path] = acceptance
	}
	return saveRiskAcceptances(rootDir, byPath)
}

// saveRiskAcceptances persists acceptances sorted by path for stable reads.
func saveRiskAcceptances(rootDir string, byPath map[string]RiskAcceptance) error {
	records := make([]RiskAcceptance, 0, len(byPath))
	for _, acceptance := range byPath {
		records = append(records, acceptance)
	}
	slices.SortFunc(records, func(a, b RiskAcceptance) int { return strings.Compare(a.Path, b.Path) })
	content, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal accepted risks: %w", err)
	}
	staged, err := stageFile(rootDir, append(content, '\n'), ManagedFileMode)
	if err != nil {
		return fmt.Errorf("write accepted risks: %w", err)
	}
	if err := renameFile(staged, filepath.Join(rootDir, RiskAcceptanceFilename)); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("write accepted risks: %w", err)
	}
	return nil
}

// loadRiskAcceptances reads the record keyed by path. A missing or unreadable
// record means nothing was accepted, so the user is simply asked again.
func loadRiskAcceptances(rootDir string) map[string]RiskAcceptance {
	byPath := map[string]RiskAcceptance{}
	raw, err := os.ReadFile(filepath.Join(rootDir, RiskAcceptanceFilename))
	if err != nil {
		return byPath
	}
	var records []RiskAcceptance
	if json.Unmarshal(raw, &records) != nil {
		return byPath
	}
	for _, record := range records {
		byPath[record.Path] = record
	}
	return byPath
}
