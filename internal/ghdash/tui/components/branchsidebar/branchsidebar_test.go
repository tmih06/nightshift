package branchsidebar

import (
	"testing"

	gitm "github.com/aymanbagabas/git-module"
	"github.com/stretchr/testify/require"

	"github.com/tmih06/nightshift/internal/ghdash/data"
	"github.com/tmih06/nightshift/internal/ghdash/git"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/branch"
	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
	"github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

func testCtx() *context.ProgramContext {
	return &context.ProgramContext{
		Theme:               *theme.DefaultTheme,
		DynamicPreviewWidth: 20,
	}
}

func TestView_NoBranch(t *testing.T) {
	m := NewModel(testCtx())
	m.ctx = testCtx()
	m.status = &gitm.NameStatus{}

	got := m.View()
	require.Equal(t, "No branch selected", got)
}

func TestView_StatusLoading(t *testing.T) {
	m := NewModel(testCtx())
	m.ctx = testCtx()
	m.branch = &branch.BranchData{Data: git.Branch{Name: "main"}}

	got := m.View()
	require.Contains(t, got, "Loading...")
	require.Contains(t, got, "main")
}

func TestView_NoChanges(t *testing.T) {
	m := NewModel(testCtx())
	m.ctx = testCtx()
	m.branch = &branch.BranchData{Data: git.Branch{Name: "main"}}
	m.status = &gitm.NameStatus{}

	got := m.View()
	require.Contains(t, got, "No changes")
	require.Contains(t, got, "main")
}

func TestView_WithFileChanges(t *testing.T) {
	m := NewModel(testCtx())
	m.ctx = testCtx()
	m.branch = &branch.BranchData{Data: git.Branch{Name: "feature"}}
	m.status = &gitm.NameStatus{
		Added:    []string{"new.go"},
		Removed:  []string{"old.go"},
		Modified: []string{"changed.go"},
	}

	got := m.View()
	require.Contains(t, got, "A new.go")
	require.Contains(t, got, "D old.go")
	require.Contains(t, got, "M changed.go")
	require.Contains(t, got, "feature")
}

func TestView_WithPR(t *testing.T) {
	m := NewModel(testCtx())
	m.ctx = testCtx()
	m.branch = &branch.BranchData{
		Data: git.Branch{Name: "feature"},
		PR:   &data.PullRequestData{Number: 42, Title: "Add feature"},
	}
	m.status = &gitm.NameStatus{}

	got := m.View()
	require.Contains(t, got, "#42 Add feature")
	require.Contains(t, got, "feature")
}
