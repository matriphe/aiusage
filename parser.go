package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ProjectDir struct {
	Path string
	Name string // encoded directory name
}

func discoverProjects(claudeDir string) []ProjectDir {
	projectsDir := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot read projects dir: %v\n", err)
		return nil
	}
	var dirs []ProjectDir
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, ProjectDir{
				Path: filepath.Join(projectsDir, e.Name()),
				Name: e.Name(),
			})
		}
	}
	return dirs
}

func discoverJSONLFiles(projectDir string) []string {
	var files []string

	// Top-level JSONL files
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(projectDir, e.Name()))
		}
	}

	// Subagent files: {session-id}/subagents/agent-*.jsonl
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subagentsDir := filepath.Join(projectDir, e.Name(), "subagents")
		subs, err := os.ReadDir(subagentsDir)
		if err != nil {
			continue
		}
		for _, s := range subs {
			if !s.IsDir() && strings.HasSuffix(s.Name(), ".jsonl") {
				files = append(files, filepath.Join(subagentsDir, s.Name()))
			}
		}
	}

	return files
}

// jsonLine is the first-pass parse structure
type jsonLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
	CWD       string          `json:"cwd"`
	SessionID string          `json:"sessionId"`
}

type jsonMessage struct {
	ID    string    `json:"id"`
	Role  string    `json:"role"`
	Model string    `json:"model"`
	Usage jsonUsage `json:"usage"`
}

type jsonUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

func parseJSONLFile(path string) []MessageRecord {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024)

	// Use ordered slice to preserve last occurrence per message ID
	type indexedRecord struct {
		record MessageRecord
		order  int
	}
	seen := make(map[string]*indexedRecord)
	var orderCounter int

	for scanner.Scan() {
		line := scanner.Bytes()

		var jl jsonLine
		if err := json.Unmarshal(line, &jl); err != nil {
			continue
		}
		if jl.Type != "assistant" || len(jl.Message) == 0 {
			continue
		}

		var msg jsonMessage
		if err := json.Unmarshal(jl.Message, &msg); err != nil {
			continue
		}
		if msg.Role != "assistant" || msg.ID == "" || msg.Model == "" {
			continue
		}
		// Skip unknown models (e.g. <synthetic>)
		if _, ok := lookupPricing(msg.Model); !ok {
			continue
		}

		ts, _ := time.Parse(time.RFC3339Nano, jl.Timestamp)

		rec := MessageRecord{
			MessageID: msg.ID,
			Model:     msg.Model,
			Timestamp: ts,
			SessionID: jl.SessionID,
			CWD:       jl.CWD,
			Usage: UsageData{
				InputTokens:              msg.Usage.InputTokens,
				OutputTokens:             msg.Usage.OutputTokens,
				CacheCreationInputTokens: msg.Usage.CacheCreationInputTokens,
				CacheReadInputTokens:     msg.Usage.CacheReadInputTokens,
			},
		}

		// Keep last occurrence (streaming: later chunks have accumulated totals)
		if existing, ok := seen[msg.ID]; ok {
			existing.record = rec
		} else {
			seen[msg.ID] = &indexedRecord{record: rec, order: orderCounter}
			orderCounter++
		}
	}

	records := make([]MessageRecord, 0, len(seen))
	for _, ir := range seen {
		records = append(records, ir.record)
	}
	return records
}

// projectNameFromCWD extracts the project name from a CWD path.
// Handles worktree paths like /Users/x/Projects/foo/.claude/worktrees/bar -> "foo"
func projectNameFromCWD(cwd string) string {
	if cwd == "" {
		return ""
	}
	// Detect worktree: .../<project>/.claude/worktrees/<name>
	if idx := strings.Index(cwd, "/.claude/worktrees/"); idx != -1 {
		return filepath.Base(cwd[:idx])
	}
	return filepath.Base(cwd)
}

// projectNameFromDirName decodes the encoded directory name.
// e.g. "-Users-mza-Projects-delivery-logistics" -> "delivery-logistics"
func projectNameFromDirName(dirName string) string {
	// Strip worktree suffix: --claude-worktrees-*
	if idx := strings.Index(dirName, "--claude-worktrees-"); idx != -1 {
		dirName = dirName[:idx]
	}
	// Take last segment after the last single dash that follows a path separator pattern
	// The encoding replaces / with - so "-Users-mza-Projects-foo" means /Users/mza/Projects/foo
	parts := strings.Split(dirName, "-")
	// Find "Projects" and take everything after it
	for i, p := range parts {
		if p == "Projects" && i+1 < len(parts) {
			return strings.Join(parts[i+1:], "-")
		}
	}
	// Fallback: last non-empty segment
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return dirName
}

// normalizeProjectDirName strips worktree suffixes so worktrees merge with parent.
func normalizeProjectDirName(dirName string) string {
	if idx := strings.Index(dirName, "--claude-worktrees-"); idx != -1 {
		return dirName[:idx]
	}
	return dirName
}

func parseAllProjects(claudeDir string) []MessageRecord {
	projectDirs := discoverProjects(claudeDir)

	// Group project dirs by normalized name (merge worktrees)
	type projectGroup struct {
		normalizedName string
		dirs           []ProjectDir
	}
	groups := make(map[string]*projectGroup)
	for _, pd := range projectDirs {
		norm := normalizeProjectDirName(pd.Name)
		if g, ok := groups[norm]; ok {
			g.dirs = append(g.dirs, pd)
		} else {
			groups[norm] = &projectGroup{
				normalizedName: norm,
				dirs:           []ProjectDir{pd},
			}
		}
	}

	var allRecords []MessageRecord
	for _, g := range groups {
		projectName := projectNameFromDirName(g.normalizedName)

		for _, pd := range g.dirs {
			files := discoverJSONLFiles(pd.Path)
			for _, f := range files {
				records := parseJSONLFile(f)
				for i := range records {
					if records[i].Project == "" {
						if records[i].CWD != "" {
							records[i].Project = projectNameFromCWD(records[i].CWD)
						} else {
							records[i].Project = projectName
						}
					}
				}
				allRecords = append(allRecords, records...)
			}
		}
	}

	return allRecords
}
