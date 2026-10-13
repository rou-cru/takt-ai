package catalog_test

import (
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

func TestVFSAuthorsAreExactlyTheWriteGrantedInstances(t *testing.T) {
	packages, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	authors := packages.VFSAuthors()
	if len(authors) == 0 || !slices.IsSorted(authors) {
		t.Fatalf("VFSAuthors() = %v; want a sorted, non-empty list", authors)
	}
	for _, def := range packages.Agents {
		for instance, grants := range def.VFSGrants {
			if slices.Contains(grants, model.VFSCapabilityWrite) != slices.Contains(authors, instance) {
				t.Errorf("instance %s: write grant %v, listed as author %v", instance, slices.Contains(grants, model.VFSCapabilityWrite), slices.Contains(authors, instance))
			}
		}
	}
}
