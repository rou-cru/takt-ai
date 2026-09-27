// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package setup

import "fmt"

// DefaultPlanRequest returns a PlanRequest selecting every applicable
// component; component choice is resolved in BuildTargetPlans.
func DefaultPlanRequest() (PlanRequest, error) {
	allComponents, err := AllComponents()
	if err != nil {
		return PlanRequest{}, fmt.Errorf("default setup request: %w", err)
	}
	componentNames := make([]string, len(allComponents))
	for i, component := range allComponents {
		componentNames[i] = string(component)
	}

	return PlanRequest{
		Components: componentNames,
	}, nil
}
