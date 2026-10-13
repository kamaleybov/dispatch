package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jongio/dispatch/internal/config"
	"github.com/jongio/dispatch/internal/data"
	"github.com/jongio/dispatch/internal/tui/components"
)

type titleLayoutResult struct {
	layout   components.TitleLayout
	sessions []data.Session
	warnings []string
}

type compiledTitleGroup struct {
	rule        config.TitleGroupRule
	parent      *data.Session
	childRegexp *regexp.Regexp
	skip        bool
}

func buildTitleLayout(
	cfg *config.TitleGroupsConfig,
	candidates []data.Session,
	matches []data.Session,
	hiddenSet map[string]struct{},
	showHidden bool,
) titleLayoutResult {
	if cfg == nil || len(cfg.Groups) == 0 {
		ungrouped := append([]data.Session(nil), matches...)
		if cfg != nil {
			sortTitleSessions(ungrouped, cfg.UngroupedSort)
		}
		return titleLayoutResult{
			layout:   components.TitleLayout{Ungrouped: ungrouped},
			sessions: ungrouped,
		}
	}

	matchedByID := make(map[string]data.Session, len(matches))
	for _, session := range matches {
		matchedByID[session.ID] = session
	}

	compiled := make([]compiledTitleGroup, len(cfg.Groups))
	parentIDs := make(map[string]int)
	reservedParentIDs := make(map[string]struct{})
	var warnings []string

	for i, rule := range cfg.Groups {
		compiled[i].rule = rule
		if rule.Remaining {
			if strings.TrimSpace(rule.Title) == "" {
				compiled[i].skip = true
				warnings = append(warnings, fmt.Sprintf("title group %d remaining group has no title", i+1))
			}
			continue
		}
		if strings.TrimSpace(rule.ParentPattern) == "" {
			compiled[i].skip = true
			warnings = append(warnings, fmt.Sprintf("title group %d has no parent pattern", i+1))
			continue
		}

		parentRegexp, err := regexp.Compile(rule.ParentPattern)
		if err != nil {
			compiled[i].skip = true
			warnings = append(warnings, fmt.Sprintf("title group %d has an invalid parent pattern", i+1))
			continue
		}
		if rule.ChildPattern != "" {
			childRegexp, err := regexp.Compile(rule.ChildPattern)
			if err != nil {
				compiled[i].skip = true
				warnings = append(warnings, fmt.Sprintf("title group %d has an invalid child pattern", i+1))
				continue
			}
			compiled[i].childRegexp = childRegexp
		}

		var parentMatches []data.Session
		for _, session := range candidates {
			if parentRegexp.MatchString(session.Summary) {
				parentMatches = append(parentMatches, session)
				reservedParentIDs[session.ID] = struct{}{}
			}
		}
		switch len(parentMatches) {
		case 0:
			compiled[i].skip = true
		case 1:
			parent := parentMatches[0]
			if first, exists := parentIDs[parent.ID]; exists {
				compiled[i].skip = true
				warnings = append(warnings, fmt.Sprintf(
					"title groups %d and %d match the same parent %q",
					first+1,
					i+1,
					parent.Summary,
				))
				continue
			}
			parentIDs[parent.ID] = i
			compiled[i].parent = &parent
		default:
			compiled[i].skip = true
			warnings = append(warnings, fmt.Sprintf(
				"title group %d parent pattern matches %d sessions",
				i+1,
				len(parentMatches),
			))
		}
	}

	ownerByID := make(map[string]int)
	for id, groupIndex := range parentIDs {
		ownerByID[id] = groupIndex
	}
	for i := range compiled {
		group := &compiled[i]
		if group.skip || group.parent == nil || group.childRegexp == nil {
			continue
		}
		for _, session := range candidates {
			if _, reserved := reservedParentIDs[session.ID]; reserved {
				continue
			}
			if _, claimed := ownerByID[session.ID]; claimed {
				continue
			}
			if group.childRegexp.MatchString(session.Summary) {
				ownerByID[session.ID] = i
			}
		}
	}

	childrenByGroup := make([][]data.Session, len(compiled))
	var unclaimed []data.Session
	for _, session := range matches {
		owner, claimed := ownerByID[session.ID]
		if !claimed {
			unclaimed = append(unclaimed, session)
			continue
		}
		if compiled[owner].parent != nil && session.ID == compiled[owner].parent.ID {
			continue
		}
		childrenByGroup[owner] = append(childrenByGroup[owner], session)
	}

	var layout components.TitleLayout
	remainingUsed := false
	for i, group := range compiled {
		if group.rule.Remaining {
			if group.skip || remainingUsed || len(unclaimed) == 0 {
				continue
			}
			remainingUsed = true
			children := append([]data.Session(nil), unclaimed...)
			sortTitleSessions(children, group.rule.SortBy)
			layout.Groups = append(layout.Groups, components.TitleGroup{
				Header:   strings.TrimSpace(group.rule.Title),
				Children: children,
			})
			continue
		}
		if group.skip || group.parent == nil {
			continue
		}
		if !showHidden {
			if _, hidden := hiddenSet[group.parent.ID]; hidden {
				continue
			}
		}

		children := childrenByGroup[i]
		sortTitleSessions(children, group.rule.SortBy)
		parent, parentVisible := matchedByID[group.parent.ID]
		if !parentVisible && len(children) == 0 {
			continue
		}
		if !parentVisible {
			parent = *group.parent
		}
		layout.Groups = append(layout.Groups, components.TitleGroup{
			Parent:        parent,
			HasParent:     true,
			ParentContext: !parentVisible,
			Children:      children,
		})
	}

	if !remainingUsed {
		layout.Ungrouped = append([]data.Session(nil), unclaimed...)
		sortTitleSessions(layout.Ungrouped, cfg.UngroupedSort)
	}

	result := titleLayoutResult{layout: layout, warnings: warnings}
	for _, group := range layout.Groups {
		if group.HasParent {
			result.sessions = append(result.sessions, group.Parent)
		}
		result.sessions = append(result.sessions, group.Children...)
	}
	result.sessions = append(result.sessions, layout.Ungrouped...)
	return result
}

func sortTitleSessions(sessions []data.Session, sortBy string) {
	switch sortBy {
	case config.TitleGroupSortName:
		slices.SortStableFunc(sessions, func(a, b data.Session) int {
			if result := naturalTitleCompare(a.Summary, b.Summary); result != 0 {
				return result
			}
			return strings.Compare(a.ID, b.ID)
		})
	case config.TitleGroupSortUpdated:
		slices.SortStableFunc(sessions, func(a, b data.Session) int {
			if result := strings.Compare(b.LastActiveAt, a.LastActiveAt); result != 0 {
				return result
			}
			return strings.Compare(a.ID, b.ID)
		})
	}
}

func naturalTitleCompare(a, b string) int {
	a = strings.ToLower(a)
	b = strings.ToLower(b)
	for len(a) > 0 && len(b) > 0 {
		aDigit := a[0] >= '0' && a[0] <= '9'
		bDigit := b[0] >= '0' && b[0] <= '9'
		if aDigit && bDigit {
			aEnd := digitPrefixLen(a)
			bEnd := digitPrefixLen(b)
			aNumber, _ := strconv.ParseUint(a[:aEnd], 10, 64)
			bNumber, _ := strconv.ParseUint(b[:bEnd], 10, 64)
			switch {
			case aNumber < bNumber:
				return -1
			case aNumber > bNumber:
				return 1
			case aEnd < bEnd:
				return -1
			case aEnd > bEnd:
				return 1
			}
			a = a[aEnd:]
			b = b[bEnd:]
			continue
		}
		if a[0] < b[0] {
			return -1
		}
		if a[0] > b[0] {
			return 1
		}
		a = a[1:]
		b = b[1:]
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	default:
		return 0
	}
}

func digitPrefixLen(value string) int {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return i
		}
	}
	return len(value)
}
