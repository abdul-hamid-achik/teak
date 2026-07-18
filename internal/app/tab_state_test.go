package app

import (
	"testing"

	"teak/internal/config"
)

func TestNewUntitledTabUsesModelCounterAsSourceOfTruth(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", "", cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	t.Cleanup(model.cleanup)

	firstAny, _ := model.newUntitledTab()
	first := firstAny.(Model)
	secondAny, _ := first.newUntitledTab()
	second := secondAny.(Model)

	if second.untitledCounter != 2 {
		t.Fatalf("untitledCounter = %d, want 2", second.untitledCounter)
	}
	if got := second.tabBar.Tabs[len(second.tabBar.Tabs)-2].Label; got != "Untitled-1" {
		t.Fatalf("first untitled label = %q, want Untitled-1", got)
	}
	if got := second.tabBar.Tabs[len(second.tabBar.Tabs)-1].Label; got != "Untitled-2" {
		t.Fatalf("second untitled label = %q, want Untitled-2", got)
	}
}
