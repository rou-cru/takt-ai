package catalog_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

func TestSelectableOrder(t *testing.T) {
	order, err := catalog.SelectableOrder()
	if err != nil {
		t.Fatalf("SelectableOrder() error = %v", err)
	}
	if len(order) == 0 {
		t.Fatal("SelectableOrder() returned no components")
	}
	manifest, err := catalog.LoadCapabilities()
	if err != nil {
		t.Fatalf("LoadCapabilities() error = %v", err)
	}
	selectable := make(map[string]bool)
	for _, c := range manifest.Components {
		if c.Selectable {
			selectable[c.ID] = true
		}
	}
	for _, id := range order {
		if !selectable[string(id)] {
			t.Errorf("SelectableOrder() returned %q, which the manifest does not mark selectable", id)
		}
	}
}

func TestComponentPurposeKnownAndUnknown(t *testing.T) {
	order, err := catalog.SelectableOrder()
	if err != nil || len(order) == 0 {
		t.Fatalf("SelectableOrder() = %v, %v", order, err)
	}
	if purpose := catalog.ComponentPurpose(order[0]); purpose == "" {
		t.Errorf("ComponentPurpose(%q) is empty, want a non-empty purpose", order[0])
	}

	unknown := model.ComponentID("does-not-exist")
	if got := catalog.ComponentPurpose(unknown); got != string(unknown) {
		t.Errorf("ComponentPurpose(unknown) = %q, want the id echoed back (%q)", got, unknown)
	}
}

func TestReconcileSelection(t *testing.T) {
	order, err := catalog.SelectableOrder()
	if err != nil || len(order) == 0 {
		t.Fatalf("SelectableOrder() = %v, %v", order, err)
	}
	kept, removals, err := catalog.ReconcileSelection(order)
	if err != nil {
		t.Fatalf("ReconcileSelection() error = %v", err)
	}
	if len(kept) != len(order) {
		t.Errorf("ReconcileSelection() with every selectable component selected kept %d, want %d (removals: %+v)", len(kept), len(order), removals)
	}
}
