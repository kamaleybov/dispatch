package data

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/jongio/dispatch/internal/validate"
	"gopkg.in/yaml.v3"
)

const maxWorkspaceMetadataSize = 64 * 1024

type workspaceMetadata struct {
	Name      string `yaml:"name"`
	UserNamed bool   `yaml:"user_named"`
}

// ScanUserTitles returns authoritative titles explicitly assigned by the user
// with Copilot CLI's /rename command.
func ScanUserTitles() map[string]string {
	stateDir := sessionStatePath()
	if stateDir == "" {
		return nil
	}

	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil
	}

	titles := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() || !validate.SessionID(entry.Name()) {
			continue
		}
		if title, ok := readUserTitle(stateDir, entry.Name()); ok {
			titles[entry.Name()] = title
		}
	}
	if len(titles) == 0 {
		return nil
	}
	return titles
}

func readSessionUserTitle(sessionID string) (string, bool) {
	stateDir := sessionStatePath()
	if stateDir == "" {
		return "", false
	}
	return readUserTitle(stateDir, sessionID)
}

func readUserTitle(stateDir, sessionID string) (string, bool) {
	path, ok := workspaceMetadataPath(stateDir, sessionID)
	if !ok {
		return "", false
	}

	sessionInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil || !sessionInfo.IsDir() || sessionInfo.Mode()&os.ModeSymlink != 0 {
		return "", false
	}

	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxWorkspaceMetadataSize {
		return "", false
	}

	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxWorkspaceMetadataSize+1))
	if err != nil || len(content) > maxWorkspaceMetadataSize {
		return "", false
	}

	var metadata workspaceMetadata
	if err := yaml.Unmarshal(content, &metadata); err != nil || !metadata.UserNamed {
		return "", false
	}
	title := strings.TrimSpace(metadata.Name)
	if title == "" {
		return "", false
	}
	return title, true
}

func workspaceMetadataPath(stateDir, sessionID string) (string, bool) {
	if stateDir == "" || !validate.SessionID(sessionID) {
		return "", false
	}

	root := filepath.Clean(stateDir)
	path := filepath.Clean(filepath.Join(root, sessionID, "workspace.yaml"))
	prefix := root + string(filepath.Separator)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
		prefix = strings.ToLower(prefix)
	}
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return filepath.Join(root, sessionID, "workspace.yaml"), true
}

func applyUserTitles(sessions []Session, titles map[string]string) {
	for i := range sessions {
		applyUserTitle(&sessions[i], titles)
	}
}

func applyUserTitle(session *Session, titles map[string]string) {
	if title, ok := titles[session.ID]; ok {
		session.Summary = title
	}
}

func filterByUserTitles(filter FilterOptions, titles map[string]string) FilterOptions {
	if len(titles) == 0 {
		return filter
	}

	query := normalizeMatchText(filter.Query)
	if query != "" {
		for id, title := range titles {
			if strings.Contains(normalizeMatchText(title), query) {
				filter.userTitleMatchIDs = append(filter.userTitleMatchIDs, id)
			}
		}
		slices.Sort(filter.userTitleMatchIDs)
	}

	for _, excluded := range filter.ExcludedWords {
		excluded = normalizeMatchText(excluded)
		if excluded == "" {
			continue
		}
		for id, title := range titles {
			if strings.Contains(normalizeMatchText(title), excluded) {
				filter.userTitleExcludedIDs = append(filter.userTitleExcludedIDs, id)
			}
		}
	}
	filter.userTitleExcludedIDs = dedupeStrings(filter.userTitleExcludedIDs)
	slices.Sort(filter.userTitleExcludedIDs)
	return filter
}

func dedupeStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	result := values[:0]
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
