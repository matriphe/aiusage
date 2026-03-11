package main

import "time"

type UsageData struct {
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
}

type MessageRecord struct {
	MessageID string
	Model     string
	Timestamp time.Time
	SessionID string
	Project   string
	CWD       string
	Usage     UsageData
}

type ProjectStats struct {
	Name        string
	Models      map[string]*ModelStats
	DailyModels map[string]map[string]*ModelStats // date -> model -> stats
	TotalUsage  UsageData
	TotalCost   float64
}

type ModelStats struct {
	Model     string
	Usage     UsageData
	Cost      float64
	CallCount int
}

type CLIFlags struct {
	Since    string
	Until    string
	Daily    bool
	Model    bool
	ClaudeDir string
}
