package tui

import (
	"testing"

	"github.com/jongio/dispatch/internal/config"
	"github.com/jongio/dispatch/internal/data"
)

func titleSession(id, title, updated string) data.Session {
	return data.Session{ID: id, Summary: title, LastActiveAt: updated}
}

func TestBuildTitleLayout(t *testing.T) {
	t.Parallel()

	candidates := []data.Session{
		titleSession("journal", "Journal", "2026-03-01"),
		titleSession("p0", "p0--operations", "2026-03-02"),
		titleSession("p0-i10", "p0-i10--later", "2026-03-05"),
		titleSession("p0-i2", "p0-i2--earlier", "2026-03-04"),
		titleSession("other", "Unrelated", "2026-03-06"),
	}
	cfg := &config.TitleGroupsConfig{
		Groups: []config.TitleGroupRule{
			{ParentPattern: `^Journal$`},
			{ParentPattern: `^p0--`, ChildPattern: `^p0-i\d+--`, SortBy: config.TitleGroupSortName},
		},
		UngroupedSort: config.TitleGroupSortUpdated,
	}

	result := buildTitleLayout(cfg, candidates, candidates, nil, false)
	if len(result.layout.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(result.layout.Groups))
	}
	if got := result.layout.Groups[0].Parent.ID; got != "journal" {
		t.Fatalf("first parent = %q, want journal", got)
	}
	children := result.layout.Groups[1].Children
	if len(children) != 2 || children[0].ID != "p0-i2" || children[1].ID != "p0-i10" {
		t.Fatalf("children = %#v, want natural name order", children)
	}
	if len(result.layout.Ungrouped) != 1 || result.layout.Ungrouped[0].ID != "other" {
		t.Fatalf("ungrouped = %#v, want other", result.layout.Ungrouped)
	}
}

func TestBuildTitleLayoutShowsFilteredParentContext(t *testing.T) {
	t.Parallel()

	parent := titleSession("parent", "p1--oncall", "2026-03-01")
	child := titleSession("child", "p1-i1--incident", "2026-03-02")
	cfg := &config.TitleGroupsConfig{Groups: []config.TitleGroupRule{{
		ParentPattern: `^p1--`,
		ChildPattern:  `^p1-i`,
	}}}

	result := buildTitleLayout(cfg, []data.Session{parent, child}, []data.Session{child}, nil, false)
	if len(result.layout.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(result.layout.Groups))
	}
	group := result.layout.Groups[0]
	if !group.ParentContext {
		t.Fatal("filtered parent should be marked as context")
	}
	if group.Parent.ID != parent.ID || len(group.Children) != 1 || group.Children[0].ID != child.ID {
		t.Fatalf("unexpected context group: %#v", group)
	}
}

func TestBuildTitleLayoutHiddenParentSuppressesChildren(t *testing.T) {
	t.Parallel()

	parent := titleSession("parent", "p1--oncall", "2026-03-01")
	child := titleSession("child", "p1-i1--incident", "2026-03-02")
	cfg := &config.TitleGroupsConfig{Groups: []config.TitleGroupRule{{
		ParentPattern: `^p1--`,
		ChildPattern:  `^p1-i`,
	}}}
	hidden := map[string]struct{}{"parent": {}}

	result := buildTitleLayout(cfg, []data.Session{parent, child}, []data.Session{child}, hidden, false)
	if len(result.layout.Groups) != 0 || len(result.layout.Ungrouped) != 0 {
		t.Fatalf("hidden parent should suppress its subtree: %#v", result.layout)
	}

	result = buildTitleLayout(cfg, []data.Session{parent, child}, []data.Session{parent, child}, hidden, true)
	if len(result.layout.Groups) != 1 || len(result.layout.Groups[0].Children) != 1 {
		t.Fatalf("show hidden should restore subtree: %#v", result.layout)
	}
}

func TestBuildTitleLayoutRemainingGroupUsesConfiguredPosition(t *testing.T) {
	t.Parallel()

	journal := titleSession("journal", "Journal", "2026-03-01")
	other := titleSession("other", "Other session", "2026-03-02")
	cfg := &config.TitleGroupsConfig{Groups: []config.TitleGroupRule{
		{Title: "Other", Remaining: true},
		{ParentPattern: `^Journal$`},
	}}

	result := buildTitleLayout(cfg, []data.Session{journal, other}, []data.Session{journal, other}, nil, false)
	if len(result.layout.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(result.layout.Groups))
	}
	if result.layout.Groups[0].Header != "Other" || result.layout.Groups[0].Children[0].ID != "other" {
		t.Fatalf("remaining group not first: %#v", result.layout.Groups[0])
	}
	if result.layout.Groups[1].Parent.ID != "journal" {
		t.Fatalf("configured parent not second: %#v", result.layout.Groups[1])
	}
}

func TestBuildTitleLayoutAmbiguousParentLeavesSessionsUngrouped(t *testing.T) {
	t.Parallel()

	candidates := []data.Session{
		titleSession("p1", "project-one", "2026-03-01"),
		titleSession("p2", "project-two", "2026-03-02"),
		titleSession("child", "child-one", "2026-03-03"),
	}
	cfg := &config.TitleGroupsConfig{Groups: []config.TitleGroupRule{{
		ParentPattern: `^project-`,
		ChildPattern:  `^child-`,
	}}}

	result := buildTitleLayout(cfg, candidates, candidates, nil, false)
	if len(result.layout.Groups) != 0 || len(result.layout.Ungrouped) != 3 {
		t.Fatalf("ambiguous group should remain ungrouped: %#v", result.layout)
	}
	if len(result.warnings) != 1 {
		t.Fatalf("warnings = %v, want one ambiguity warning", result.warnings)
	}
}

func TestBuildTitleLayoutReservesAllParentMatchesBeforeChildren(t *testing.T) {
	t.Parallel()

	candidates := []data.Session{
		titleSession("root", "Root", "2026-03-01"),
		titleSession("p1", "project-one", "2026-03-02"),
		titleSession("p2", "project-two", "2026-03-03"),
	}
	cfg := &config.TitleGroupsConfig{Groups: []config.TitleGroupRule{
		{ParentPattern: `^Root$`, ChildPattern: `.*`},
		{ParentPattern: `^project-`},
	}}

	result := buildTitleLayout(cfg, candidates, candidates, nil, false)
	if len(result.layout.Groups) != 1 || len(result.layout.Groups[0].Children) != 0 {
		t.Fatalf("ambiguous parent candidates were claimed as children: %#v", result.layout)
	}
	if len(result.layout.Ungrouped) != 2 {
		t.Fatalf("ambiguous parent candidates should remain ungrouped: %#v", result.layout.Ungrouped)
	}
}
