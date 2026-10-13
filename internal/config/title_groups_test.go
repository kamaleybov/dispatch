package config

import (
	"strings"
	"testing"
)

func TestValidateTitleGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  *TitleGroupsConfig
		path string
	}{
		{
			name: "valid regular and remaining groups",
			cfg: &TitleGroupsConfig{
				Groups: []TitleGroupRule{
					{ParentPattern: `^Journal$`, SortBy: TitleGroupSortName},
					{ParentPattern: `^p0--`, ChildPattern: `^p0-i\d+--`, SortBy: TitleGroupSortUpdated},
					{Title: "Other", Remaining: true},
				},
				UngroupedSort: TitleGroupSortUpdated,
			},
		},
		{
			name: "missing parent",
			cfg:  &TitleGroupsConfig{Groups: []TitleGroupRule{{ChildPattern: `^child`}}},
			path: "title_groups.groups[0].parent_pattern",
		},
		{
			name: "invalid parent regex",
			cfg:  &TitleGroupsConfig{Groups: []TitleGroupRule{{ParentPattern: `(`}}},
			path: "title_groups.groups[0].parent_pattern",
		},
		{
			name: "invalid child regex",
			cfg:  &TitleGroupsConfig{Groups: []TitleGroupRule{{ParentPattern: `^p`, ChildPattern: `(`}}},
			path: "title_groups.groups[0].child_pattern",
		},
		{
			name: "remaining requires title",
			cfg:  &TitleGroupsConfig{Groups: []TitleGroupRule{{Remaining: true}}},
			path: "title_groups.groups[0].title",
		},
		{
			name: "remaining forbids patterns",
			cfg: &TitleGroupsConfig{Groups: []TitleGroupRule{{
				Remaining: true, Title: "Other", ParentPattern: `^p`,
			}}},
			path: "title_groups.groups[0].parent_pattern",
		},
		{
			name: "duplicate remaining",
			cfg: &TitleGroupsConfig{Groups: []TitleGroupRule{
				{Remaining: true, Title: "Other"},
				{Remaining: true, Title: "Everything else"},
			}},
			path: "title_groups.groups[1].remaining",
		},
		{
			name: "invalid group sort",
			cfg: &TitleGroupsConfig{Groups: []TitleGroupRule{{
				ParentPattern: `^p`, SortBy: "created",
			}}},
			path: "title_groups.groups[0].sort_by",
		},
		{
			name: "invalid ungrouped sort",
			cfg:  &TitleGroupsConfig{UngroupedSort: "turns"},
			path: "title_groups.ungrouped_sort",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := Default()
			cfg.TitleGroups = tt.cfg
			diags := ValidateConfig(cfg)
			if tt.path == "" {
				for _, diag := range diags {
					if strings.HasPrefix(diag.Path, "title_groups") {
						t.Fatalf("unexpected diagnostic: %+v", diag)
					}
				}
				return
			}
			for _, diag := range diags {
				if diag.Path == tt.path {
					return
				}
			}
			t.Fatalf("missing diagnostic for %s; got %+v", tt.path, diags)
		})
	}
}
