package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
)

func main() {
	var flags CLIFlags

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}
	defaultClaudeDir := filepath.Join(homeDir, ".claude")

	cmd := &cli.Command{
		Name:  "aiusage",
		Usage: "A CLI tool to analyze standard Claude Cost and Token usage from logs",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "since",
				Usage:       "Start date (YYYY-MM-DD)",
				Destination: &flags.Since,
			},
			&cli.StringFlag{
				Name:        "until",
				Usage:       "End date (YYYY-MM-DD)",
				Destination: &flags.Until,
			},
			&cli.BoolFlag{
				Name:        "daily",
				Usage:       "Show daily breakdown",
				Destination: &flags.Daily,
			},
			&cli.BoolFlag{
				Name:        "model",
				Usage:       "Show per-model breakdown",
				Destination: &flags.Model,
			},
			&cli.StringFlag{
				Name:        "claude-dir",
				Usage:       "Path to Claude data directory",
				Value:       defaultClaudeDir,
				Destination: &flags.ClaudeDir,
			},
			&cli.StringFlag{
				Name:        "project",
				Usage:       "Filter by project name (substring match)",
				Destination: &flags.Project,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			records := parseAllProjects(flags.ClaudeDir)
			if len(records) == 0 {
				return fmt.Errorf("No usage data found.")
			}

			records = filterByDate(records, flags.Since, flags.Until)
			if len(records) == 0 {
				return fmt.Errorf("No usage data found for the specified period.")
			}

			// Compute overall (unfiltered) stats for global totals.
			allStats := aggregate(records, flags)

			// Apply project filter for the main report view, if requested.
			filteredRecords := filterByProject(records, flags.Project)
			if len(filteredRecords) == 0 {
				return fmt.Errorf("No usage data found for project %q.", flags.Project)
			}

			stats := aggregate(filteredRecords, flags)
			printReport(filteredRecords, stats, allStats, flags)
			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
