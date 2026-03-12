package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	colLabel    = 38 // left-aligned project/model name
	colTokens   = 9  // right-aligned token columns
	colCost     = 12 // right-aligned cost column
	colDate     = 12 // left-aligned date column
)

// totalWidth is the full width of the table separator line.
// 6 fields separated by 5 spaces: label + 4 token cols + cost col
var totalWidth = colLabel + 4*colTokens + 5 + colCost

func repeatChar(ch byte, n int) string {
	return strings.Repeat(string(ch), n)
}

func formatCost(cost float64) string {
	negative := cost < 0
	if negative {
		cost = -cost
	}
	whole := int64(cost)
	frac := int64((cost - float64(whole)) * 100 + 0.5)
	if frac >= 100 {
		whole++
		frac -= 100
	}

	// Format whole part with comma grouping
	s := fmt.Sprintf("%d", whole)
	if len(s) > 3 {
		var parts []string
		for len(s) > 3 {
			parts = append([]string{s[len(s)-3:]}, parts...)
			s = s[:len(s)-3]
		}
		parts = append([]string{s}, parts...)
		s = strings.Join(parts, ",")
	}

	prefix := "$"
	if negative {
		prefix = "-$"
	}
	return fmt.Sprintf("%s%s.%02d", prefix, s, frac)
}

func formatRow(label string, labelWidth int, input, output, cacheRd, cacheWr int64, cost float64) string {
	return fmt.Sprintf("%-*s %*s %*s %*s %*s %*s",
		labelWidth, label,
		colTokens, formatTokens(input),
		colTokens, formatTokens(output),
		colTokens, formatTokens(cacheRd),
		colTokens, formatTokens(cacheWr),
		colCost, formatCost(cost),
	)
}

func formatHeaderRow(label string, labelWidth int) string {
	return fmt.Sprintf("%-*s %*s %*s %*s %*s %*s",
		labelWidth, label,
		colTokens, "Input",
		colTokens, "Output",
		colTokens, "Cache Rd",
		colTokens, "Cache Wr",
		colCost, "Cost",
	)
}

// formatLabelCostRow prints a label left-aligned with cost right-aligned at the end of the row.
func formatLabelCostRow(label string, cost float64) string {
	costStr := formatCost(cost)
	padWidth := totalWidth - len(label) - len(costStr)
	if padWidth < 1 {
		padWidth = 1
	}
	return label + strings.Repeat(" ", padWidth) + costStr
}

func filterByProject(records []MessageRecord, project string) []MessageRecord {
	if project == "" {
		return records
	}
	target := strings.ToLower(project)
	var filtered []MessageRecord
	for _, r := range records {
		if strings.Contains(strings.ToLower(r.Project), target) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func filterByDate(records []MessageRecord, since, until string) []MessageRecord {
	var sinceTime, untilTime time.Time
	if since != "" {
		sinceTime, _ = time.Parse("2006-01-02", since)
	}
	if until != "" {
		untilTime, _ = time.Parse("2006-01-02", until)
		// Include the entire "until" day
		untilTime = untilTime.Add(24*time.Hour - time.Nanosecond)
	}

	var filtered []MessageRecord
	for _, r := range records {
		if !sinceTime.IsZero() && r.Timestamp.Before(sinceTime) {
			continue
		}
		if !untilTime.IsZero() && r.Timestamp.After(untilTime) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

func aggregate(records []MessageRecord, flags CLIFlags) []ProjectStats {
	projectMap := make(map[string]*ProjectStats)

	for _, r := range records {
		ps, ok := projectMap[r.Project]
		if !ok {
			ps = &ProjectStats{
				Name:        r.Project,
				Models:      make(map[string]*ModelStats),
				DailyModels: make(map[string]map[string]*ModelStats),
			}
			projectMap[r.Project] = ps
		}

		normalizedModel := NormalizeModelName(r.Model)
		cost := CalculateCost(r.Model, r.Usage)

		// Per-model stats
		ms, ok := ps.Models[normalizedModel]
		if !ok {
			ms = &ModelStats{Model: normalizedModel}
			ps.Models[normalizedModel] = ms
		}
		addUsage(&ms.Usage, r.Usage)
		ms.Cost += cost
		ms.CallCount++

		// Daily stats
		if flags.Daily {
			dateKey := r.Timestamp.Format("2006-01-02")
			if ps.DailyModels[dateKey] == nil {
				ps.DailyModels[dateKey] = make(map[string]*ModelStats)
			}
			dm, ok := ps.DailyModels[dateKey][normalizedModel]
			if !ok {
				dm = &ModelStats{Model: normalizedModel}
				ps.DailyModels[dateKey][normalizedModel] = dm
			}
			addUsage(&dm.Usage, r.Usage)
			dm.Cost += cost
			dm.CallCount++
		}

		// Project totals
		addUsage(&ps.TotalUsage, r.Usage)
		ps.TotalCost += cost
	}

	stats := make([]ProjectStats, 0, len(projectMap))
	for _, ps := range projectMap {
		stats = append(stats, *ps)
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].TotalCost > stats[j].TotalCost
	})
	return stats
}

func addUsage(dst *UsageData, src UsageData) {
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CacheCreationInputTokens += src.CacheCreationInputTokens
	dst.CacheReadInputTokens += src.CacheReadInputTokens
}

func printReport(records []MessageRecord, stats []ProjectStats, flags CLIFlags) {
	fmt.Println("Claude Code Usage Report")
	fmt.Println("========================")
	fmt.Println()

	printPeriod(records, flags)

	if !flags.Model && !flags.Daily {
		printSummaryTable(stats)
	} else if flags.Model && !flags.Daily {
		printModelTable(stats)
	} else if flags.Daily && !flags.Model {
		printDailyTable(stats)
	} else {
		printDailyModelTable(stats)
	}
}

func printSummaryTable(stats []ProjectStats) {
	fmt.Println(formatHeaderRow("Project", colLabel))
	fmt.Println(repeatChar('-', totalWidth))

	var total UsageData
	var totalCost float64
	for _, ps := range stats {
		fmt.Println(formatRow(
			truncate(ps.Name, colLabel),
			colLabel,
			ps.TotalUsage.InputTokens,
			ps.TotalUsage.OutputTokens,
			ps.TotalUsage.CacheReadInputTokens,
			ps.TotalUsage.CacheCreationInputTokens,
			ps.TotalCost,
		))
		addUsage(&total, ps.TotalUsage)
		totalCost += ps.TotalCost
	}
	fmt.Println(repeatChar('-', totalWidth))
	fmt.Println(formatRow(
		"TOTAL",
		colLabel,
		total.InputTokens,
		total.OutputTokens,
		total.CacheReadInputTokens,
		total.CacheCreationInputTokens,
		totalCost,
	))
}

func printModelTable(stats []ProjectStats) {
	fmt.Println(formatHeaderRow("Project / Model", colLabel))
	fmt.Println(repeatChar('-', totalWidth))

	var total UsageData
	var totalCost float64
	for i, ps := range stats {
		if i > 0 {
			fmt.Println()
		}
		fmt.Println(formatLabelCostRow(ps.Name, ps.TotalCost))

		models := sortedModelStats(ps.Models)
		for _, ms := range models {
			fmt.Println(formatRow(
				"  "+ms.Model,
				colLabel,
				ms.Usage.InputTokens,
				ms.Usage.OutputTokens,
				ms.Usage.CacheReadInputTokens,
				ms.Usage.CacheCreationInputTokens,
				ms.Cost,
			))
		}
		addUsage(&total, ps.TotalUsage)
		totalCost += ps.TotalCost
	}
	fmt.Println(repeatChar('-', totalWidth))
	fmt.Println(formatRow(
		"TOTAL",
		colLabel,
		total.InputTokens,
		total.OutputTokens,
		total.CacheReadInputTokens,
		total.CacheCreationInputTokens,
		totalCost,
	))
}

func printDailyTable(stats []ProjectStats) {
	dailyLabelWidth := colDate + colLabel
	dailyTotalWidth := dailyLabelWidth + 4*(colTokens+1) + colCost

	fmt.Println(fmt.Sprintf("%-*s %-*s %*s %*s %*s %*s %*s",
		colDate, "Date",
		colLabel, "Project",
		colTokens, "Input",
		colTokens, "Output",
		colTokens, "Cache Rd",
		colTokens, "Cache Wr",
		colCost, "Cost",
	))
	fmt.Println(repeatChar('-', dailyTotalWidth))

	// Collect all date/project combos and sort by date
	type dailyRow struct {
		date    string
		project string
		usage   UsageData
		cost    float64
	}
	var rows []dailyRow
	for _, ps := range stats {
		dates := sortedKeys(ps.DailyModels)
		for _, date := range dates {
			models := ps.DailyModels[date]
			var dayUsage UsageData
			var dayCost float64
			for _, ms := range models {
				addUsage(&dayUsage, ms.Usage)
				dayCost += ms.Cost
			}
			rows = append(rows, dailyRow{date, ps.Name, dayUsage, dayCost})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].date != rows[j].date {
			return rows[i].date < rows[j].date
		}
		return rows[i].cost > rows[j].cost
	})

	for _, r := range rows {
		fmt.Println(fmt.Sprintf("%-*s %-*s %*s %*s %*s %*s %*s",
			colDate, r.date,
			colLabel, truncate(r.project, colLabel),
			colTokens, formatTokens(r.usage.InputTokens),
			colTokens, formatTokens(r.usage.OutputTokens),
			colTokens, formatTokens(r.usage.CacheReadInputTokens),
			colTokens, formatTokens(r.usage.CacheCreationInputTokens),
			colCost, formatCost(r.cost),
		))
	}
}

func printDailyModelTable(stats []ProjectStats) {
	fmt.Println(formatHeaderRow("Project / Date / Model", colLabel))
	fmt.Println(repeatChar('-', totalWidth))

	for i, ps := range stats {
		if i > 0 {
			fmt.Println()
		}
		fmt.Println(formatLabelCostRow(ps.Name, ps.TotalCost))

		dates := sortedKeys(ps.DailyModels)
		for _, date := range dates {
			// Calculate day total for this project
			var dayCost float64
			models := sortedModelStats(ps.DailyModels[date])
			for _, ms := range models {
				dayCost += ms.Cost
			}
			fmt.Println(formatLabelCostRow("  "+date, dayCost))

			for _, ms := range models {
				fmt.Println(formatRow(
					"    "+ms.Model,
					colLabel,
					ms.Usage.InputTokens,
					ms.Usage.OutputTokens,
					ms.Usage.CacheReadInputTokens,
					ms.Usage.CacheCreationInputTokens,
					ms.Cost,
				))
			}
		}
	}
}

func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func sortedModelStats(m map[string]*ModelStats) []*ModelStats {
	stats := make([]*ModelStats, 0, len(m))
	for _, ms := range m {
		stats = append(stats, ms)
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Cost > stats[j].Cost
	})
	return stats
}

func sortedKeys(m map[string]map[string]*ModelStats) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func computeDateRange(records []MessageRecord) (string, string) {
	if len(records) == 0 {
		return "", ""
	}
	minT := records[0].Timestamp
	maxT := records[0].Timestamp
	for _, r := range records[1:] {
		if r.Timestamp.Before(minT) {
			minT = r.Timestamp
		}
		if r.Timestamp.After(maxT) {
			maxT = r.Timestamp
		}
	}
	return minT.Format("2006-01-02"), maxT.Format("2006-01-02")
}

func printPeriod(records []MessageRecord, flags CLIFlags) {
	if flags.Since != "" || flags.Until != "" {
		since := flags.Since
		until := flags.Until
		if since == "" {
			since, _ = computeDateRange(records)
		}
		if until == "" {
			_, until = computeDateRange(records)
		}
		fmt.Printf("Period: %s to %s\n\n", since, until)
	} else if len(records) > 0 {
		since, until := computeDateRange(records)
		fmt.Printf("Period: %s to %s\n\n", since, until)
	}
}
