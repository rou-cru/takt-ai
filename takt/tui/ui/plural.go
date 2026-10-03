package ui

import "fmt"

// AgentsAssigned states how many agents got a new model, in proper number.
func AgentsAssigned(count int) string {
	if count == 1 {
		return TextModelsAssignedOne
	}
	return fmt.Sprintf(TextModelsAssignedFmt, count)
}
