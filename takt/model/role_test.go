package model_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestValidateRoleClassAcceptsEveryClass(t *testing.T) {
	classes := []model.RoleClass{
		model.RoleOrchestrator,
		model.RoleDirectInterlocutor,
		model.RolePlanningAuthor,
		model.RoleExecution,
		model.RoleVerification,
		model.RoleMaintenance,
	}
	for _, class := range classes {
		if err := model.ValidateRoleClass("test", class); err != nil {
			t.Errorf("ValidateRoleClass(%q) = %v; want nil", class, err)
		}
	}
	if model.RoleMaintenance != "maintenance" {
		t.Errorf("RoleMaintenance = %q; want %q", model.RoleMaintenance, "maintenance")
	}
}

func TestValidateRoleClassRejectsUnknownAndEmpty(t *testing.T) {
	for _, class := range []model.RoleClass{"invented", ""} {
		if err := model.ValidateRoleClass("test", class); err == nil {
			t.Errorf("ValidateRoleClass(%q) = nil; want error", class)
		}
	}
}

func TestMaintenanceHoldsNoInterface(t *testing.T) {
	if model.RoleMaintenance.HoldsInterface() {
		t.Error("maintenance HoldsInterface = true; want false")
	}
}
