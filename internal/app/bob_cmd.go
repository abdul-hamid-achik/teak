package app

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"teak/internal/bob"
)

// bobResultMsg carries the result of a bob operation.
type bobResultMsg struct {
	kind  string
	plan  *bob.PlanResult
	check *bob.CheckResult
	err   error
}

func (m Model) runBobPlan() tea.Cmd {
	rootDir := m.rootDir
	return func() tea.Msg {
		plan, err := bob.Plan(context.Background(), rootDir)
		return bobResultMsg{kind: "plan", plan: plan, err: err}
	}
}

func (m Model) runBobCheck() tea.Cmd {
	rootDir := m.rootDir
	return func() tea.Msg {
		check, err := bob.Check(context.Background(), rootDir)
		return bobResultMsg{kind: "check", check: check, err: err}
	}
}

func (m *Model) handleBobResult(msg bobResultMsg) {
	if msg.err != nil {
		m.status = fmt.Sprintf("Bob: %v", msg.err)
		return
	}
	switch msg.kind {
	case "plan":
		if msg.plan == nil {
			m.status = "Bob: no plan available"
			return
		}
		creates, updates, conflicts := 0, 0, 0
		for _, a := range msg.plan.Actions {
			switch a.Action {
			case "create", "adopt":
				creates++
			case "update":
				updates++
			case "conflict":
				conflicts++
			}
		}
		if conflicts > 0 {
			m.status = fmt.Sprintf("Bob plan: %d create, %d update, %d CONFLICT", creates, updates, conflicts)
		} else {
			m.status = fmt.Sprintf("Bob plan: %d create, %d update, digest %s", creates, updates, truncDigest(msg.plan.Digest))
		}
	case "check":
		if msg.check == nil {
			m.status = "Bob: no check result"
			return
		}
		if msg.check.OK {
			m.status = "Bob: no drift detected ✓"
		} else {
			m.status = fmt.Sprintf("Bob: drift in %d files", len(msg.check.Drifted))
		}
	}
}

func truncDigest(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}
