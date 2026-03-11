package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	var flags CLIFlags

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}
	defaultClaudeDir := filepath.Join(homeDir, ".claude")

	flag.StringVar(&flags.Since, "since", "", "Start date (YYYY-MM-DD)")
	flag.StringVar(&flags.Until, "until", "", "End date (YYYY-MM-DD)")
	flag.BoolVar(&flags.Daily, "daily", false, "Show daily breakdown")
	flag.BoolVar(&flags.Model, "model", false, "Show per-model breakdown")
	flag.StringVar(&flags.ClaudeDir, "claude-dir", defaultClaudeDir, "Path to Claude data directory")
	flag.Parse()

	records := parseAllProjects(flags.ClaudeDir)
	if len(records) == 0 {
		fmt.Println("No usage data found.")
		return
	}

	records = filterByDate(records, flags.Since, flags.Until)
	if len(records) == 0 {
		fmt.Println("No usage data found for the specified period.")
		return
	}

	stats := aggregate(records, flags)
	printReport(records, stats, flags)
}
