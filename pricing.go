package main

import (
	"sort"
	"strings"
)

type modelPricing struct {
	InputPerMTok       float64
	OutputPerMTok      float64
	CacheReadPerMTok   float64
	CacheWritePerMTok  float64
}

var pricingTable = map[string]modelPricing{
	"claude-opus-4-6":   {15.0, 75.0, 3.75, 18.75},
	"claude-opus-4-5":   {15.0, 75.0, 3.75, 18.75},
	"claude-sonnet-4-5": {3.0, 15.0, 0.30, 3.75},
	"claude-sonnet-4-6": {3.0, 15.0, 0.30, 3.75},
	"claude-haiku-4-5":  {0.80, 4.0, 0.08, 1.0},
}

// sortedPrefixes returns pricing table keys sorted by length descending for longest-prefix matching.
var sortedPrefixes []string

func init() {
	sortedPrefixes = make([]string, 0, len(pricingTable))
	for k := range pricingTable {
		sortedPrefixes = append(sortedPrefixes, k)
	}
	sort.Slice(sortedPrefixes, func(i, j int) bool {
		return len(sortedPrefixes[i]) > len(sortedPrefixes[j])
	})
}

func lookupPricing(model string) (modelPricing, bool) {
	if p, ok := pricingTable[model]; ok {
		return p, true
	}
	for _, prefix := range sortedPrefixes {
		if strings.HasPrefix(model, prefix) {
			return pricingTable[prefix], true
		}
	}
	return modelPricing{}, false
}

func CalculateCost(model string, usage UsageData) float64 {
	p, ok := lookupPricing(model)
	if !ok {
		return 0
	}
	cost := float64(usage.InputTokens) / 1_000_000 * p.InputPerMTok
	cost += float64(usage.OutputTokens) / 1_000_000 * p.OutputPerMTok
	cost += float64(usage.CacheReadInputTokens) / 1_000_000 * p.CacheReadPerMTok
	cost += float64(usage.CacheCreationInputTokens) / 1_000_000 * p.CacheWritePerMTok
	return cost
}

// NormalizeModelName strips date suffixes for display grouping.
// e.g. "claude-sonnet-4-5-20250929" -> "claude-sonnet-4-5"
func NormalizeModelName(model string) string {
	for _, prefix := range sortedPrefixes {
		if strings.HasPrefix(model, prefix) {
			return prefix
		}
	}
	return model
}
