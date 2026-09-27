// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package opencodeapi

import "fmt"

// This file holds the wire types of the OpenCode v2 API responses and their
// conversion to the public types. json tags follow the server's schemas
// (packages/schema/src/*.ts in the opencode repository). Unknown fields are
// left out and therefore ignored by encoding/json; validation is applied only
// to the values Takt's decisions actually depend on.

type wireModel struct {
	ID           string `json:"id"`
	ModelID      string `json:"modelID"`
	ProviderID   string `json:"providerID"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Enabled      bool   `json:"enabled"`
	Capabilities struct {
		Tools  bool     `json:"tools"`
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"capabilities"`
	Limit struct {
		Context int   `json:"context"`
		Input   *int  `json:"input"`
		Output  int64 `json:"output"`
	} `json:"limit"`
	Time struct {
		Released float64 `json:"released"`
	} `json:"time"`
	Cost []struct {
		Tier *struct {
			Type string `json:"type"`
			Size int    `json:"size"`
		} `json:"tier"`
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
		Cache  struct {
			Read  float64 `json:"read"`
			Write float64 `json:"write"`
		} `json:"cache"`
	} `json:"cost"`
}

// toModel converts and validates. Identity and limits are what Takt branches
// on, so they are strict; status is kept as a raw string because the picker
// (R1) only surfaces it and a new status must not break listing.
func (w wireModel) toModel() (Model, error) {
	if w.ProviderID == "" || w.ModelID == "" {
		return Model{}, fmt.Errorf("model %q is missing providerID or modelID", w.ID)
	}
	if w.Limit.Context <= 0 || w.Limit.Output <= 0 {
		return Model{}, fmt.Errorf("model %s/%s has non-positive limits (context=%d output=%d)",
			w.ProviderID, w.ModelID, w.Limit.Context, w.Limit.Output)
	}
	model := Model{
		Ref: ModelRef{
			ProviderID: w.ProviderID,
			ModelID:    w.ModelID,
		},
		Name:         w.Name,
		Status:       w.Status,
		Enabled:      w.Enabled,
		Tools:        w.Capabilities.Tools,
		InputTypes:   append([]string(nil), w.Capabilities.Input...),
		OutputTypes:  append([]string(nil), w.Capabilities.Output...),
		ContextLimit: w.Limit.Context,
		OutputLimit:  int(w.Limit.Output),
		ReleasedAt:   int64(w.Time.Released),
	}
	for _, cost := range w.Cost {
		entry := ModelCost{
			Input:      cost.Input,
			Output:     cost.Output,
			CacheRead:  cost.Cache.Read,
			CacheWrite: cost.Cache.Write,
		}
		if cost.Tier != nil {
			entry.TierType = cost.Tier.Type
			entry.TierSize = cost.Tier.Size
		}
		model.Cost = append(model.Cost, entry)
	}
	return model, nil
}

type wireMCPServer struct {
	Name   string `json:"name"`
	Status struct {
		Status string  `json:"status"`
		Error  *string `json:"error"`
	} `json:"status"`
	IntegrationID *string `json:"integrationID"`
}

// toMCPServer validates the status against the known union. An unknown state
// must fail loudly: verification silently mapping it to "connected" (or to
// any assumed value) would defeat the whole point of asking the server.
func (w wireMCPServer) toMCPServer() (MCPServer, error) {
	if w.Name == "" {
		return MCPServer{}, fmt.Errorf("mcp server entry has no name")
	}
	state := MCPState(w.Status.Status)
	switch state {
	case MCPConnected, MCPPending, MCPDisabled, MCPFailed, MCPNeedsAuth:
	default:
		return MCPServer{}, fmt.Errorf("mcp server %q reports unknown state %q", w.Name, w.Status.Status)
	}
	server := MCPServer{Name: w.Name, State: state}
	if w.Status.Error != nil {
		server.Error = redactSensitiveText(*w.Status.Error)
	}
	if w.IntegrationID != nil {
		server.IntegrationID = *w.IntegrationID
	}
	return server, nil
}

type wireModelRef struct {
	ID         string  `json:"id"`
	ProviderID string  `json:"providerID"`
	Variant    *string `json:"variant"`
}

func (w wireModelRef) toModelRef() (ModelRef, error) {
	if w.ProviderID == "" || w.ID == "" {
		return ModelRef{}, fmt.Errorf("model ref is missing providerID or id")
	}
	ref := ModelRef{ProviderID: w.ProviderID, ModelID: w.ID}
	if w.Variant != nil {
		ref.Variant = *w.Variant
	}
	return ref, nil
}

type wireAgent struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description *string       `json:"description"`
	Mode        string        `json:"mode"`
	Hidden      bool          `json:"hidden"`
	Model       *wireModelRef `json:"model"`
}

func (w wireAgent) toAgent() (Agent, error) {
	if w.ID == "" {
		return Agent{}, fmt.Errorf("agent entry has no id")
	}
	agent := Agent{
		ID:     w.ID,
		Name:   w.Name,
		Mode:   w.Mode,
		Hidden: w.Hidden,
	}
	if w.Description != nil {
		agent.Description = *w.Description
	}
	if w.Model != nil {
		ref, err := w.Model.toModelRef()
		if err != nil {
			return Agent{}, fmt.Errorf("agent %q: %w", w.ID, err)
		}
		agent.Model = &ref
	}
	return agent, nil
}

type wireSkill struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Path        string  `json:"path"`
}

func (w wireSkill) toSkill() (Skill, error) {
	if w.ID == "" {
		return Skill{}, fmt.Errorf("skill entry has no id")
	}
	skill := Skill{ID: w.ID, Name: w.Name, Path: w.Path}
	if w.Description != nil {
		skill.Description = *w.Description
	}
	return skill, nil
}

type wirePlugin struct {
	ID     *string `json:"id"`
	Source struct {
		Type string `json:"type"`
	} `json:"source"`
	State struct {
		Status string  `json:"status"`
		Error  *string `json:"error"`
	} `json:"state"`
}

func (w wirePlugin) toPlugin() (Plugin, error) {
	if w.State.Status != "active" && w.State.Status != "failed" {
		return Plugin{}, fmt.Errorf("plugin reports unknown status %q", w.State.Status)
	}
	plugin := Plugin{Type: w.Source.Type, Status: w.State.Status}
	if w.ID != nil {
		plugin.ID = *w.ID
	}
	if w.State.Error != nil {
		plugin.Error = redactSensitiveText(*w.State.Error)
	}
	return plugin, nil
}
