package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/LeGambiArt/wtmcp/internal/stats"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "View tool usage statistics",
	Long: `View accumulated tool usage statistics from the last server session.

Reads stats.json from the cache directory. The server does not need
to be running.`,
	RunE: runStats,
}

var statsConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "View or modify stats configuration",
	Long:  `View current stats configuration or set individual fields.`,
	RunE:  runStatsConfig,
}

var statsConfigSetCmd = &cobra.Command{
	Use:               "set <key> <value>",
	Short:             "Set a stats configuration field",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeStatsConfigSet,
	RunE:              runStatsConfigSet,
}

var statsResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Clear accumulated stats",
	Long:  `Delete stats.json to start fresh. The server does not need to be stopped.`,
	Args:  cobra.NoArgs,
	RunE:  runStatsReset,
}

// Valid stats config keys and their allowed values.
// nil means any value is accepted (validated by type).
var statsConfigKeys = map[string][]string{
	"enabled":        {"true", "false"},
	"tokenizer":      {"chars"},
	"log_calls":      {"true", "false"},
	"persist":        {"true", "false"},
	"retention_days": nil,
}

func init() {
	statsCmd.Flags().Bool("schemas", false, "Include tool schema token costs")
	statsCmd.Flags().Bool("resources", false, "Include resource read stats")
	statsCmd.Flags().StringP("sort", "s", "calls", "Sort by: calls, tokens, errors, name")
	statsCmd.Flags().BoolP("plain", "p", false, "Plain text output (no colors or borders)")
	statsCmd.Flags().Bool("json", false, "Raw JSON output")
	statsCmd.Flags().String("since", "", "Show stats from this date (YYYY-MM-DD)")
	statsCmd.Flags().String("until", "", "Show stats until this date (YYYY-MM-DD)")
	statsCmd.Flags().IntP("days", "d", 0, "Show stats for the last N days (today = 1)")

	statsConfigCmd.AddCommand(statsConfigSetCmd)
	statsCmd.AddCommand(statsConfigCmd, statsResetCmd)
}

func runStats(cmd *cobra.Command, _ []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	plain, _ := cmd.Flags().GetBool("plain")
	showSchemas, _ := cmd.Flags().GetBool("schemas")
	showResources, _ := cmd.Flags().GetBool("resources")
	sortBy, _ := cmd.Flags().GetString("sort")
	sinceStr, _ := cmd.Flags().GetString("since")
	untilStr, _ := cmd.Flags().GetString("until")
	days, _ := cmd.Flags().GetInt("days")

	// Validate date flag combinations.
	if days > 0 && (sinceStr != "" || untilStr != "") {
		return fmt.Errorf("--days cannot be combined with --since or --until")
	}
	if days < 0 {
		return fmt.Errorf("--days must be >= 1")
	}

	// Resolve date range.
	var filterSince, filterUntil string
	var dateRange string

	if days > 0 {
		now := time.Now()
		filterSince = now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
		filterUntil = now.Format("2006-01-02")
	}
	if sinceStr != "" {
		if _, err := time.Parse("2006-01-02", sinceStr); err != nil {
			return fmt.Errorf("invalid --since date %q (expected YYYY-MM-DD): %w", sinceStr, err)
		}
		filterSince = sinceStr
	}
	if untilStr != "" {
		if _, err := time.Parse("2006-01-02", untilStr); err != nil {
			return fmt.Errorf("invalid --until date %q (expected YYYY-MM-DD): %w", untilStr, err)
		}
		filterUntil = untilStr
	}
	if filterSince != "" && filterUntil != "" && filterSince > filterUntil {
		return fmt.Errorf("--since (%s) must be before --until (%s)", filterSince, filterUntil)
	}

	filtering := filterSince != "" || filterUntil != ""
	if filtering {
		switch {
		case filterSince != "" && filterUntil != "":
			dateRange = filterSince + " to " + filterUntil
		case filterSince != "":
			dateRange = "since " + filterSince
		default:
			dateRange = "until " + filterUntil
		}
	}

	snap, err := loadSnapshot()
	if err != nil {
		return err
	}
	if snap == nil {
		fmt.Println("No stats collected yet.")
		return nil
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(snap, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Build sorted tool summaries.
	var summaries []stats.ToolSummary
	switch {
	case filtering && snap.DailyAggregates != nil:
		summaries = stats.AggregateDailyRange(snap.DailyAggregates, filterSince, filterUntil)
	case filtering:
		fmt.Println("No daily data available for date filtering (stats were collected before daily bucketing was enabled).")
		return nil
	default:
		summaries = make([]stats.ToolSummary, 0, len(snap.Aggregates))
		for _, ts := range snap.Aggregates {
			summaries = append(summaries, ts)
		}
	}
	sortSummaries(summaries, sortBy)

	if plain {
		printStatsPlain(snap, summaries, showSchemas, showResources, dateRange)
	} else {
		printStatsTable(snap, summaries, showSchemas, showResources, dateRange)
	}
	return nil
}

func loadSnapshot() (*stats.Snapshot, error) {
	result, err := getDiscoveryResult()
	if err != nil {
		return nil, err
	}

	statsPath := filepath.Join(result.Config.Cache.Dir, "stats.json")
	data, err := os.ReadFile(statsPath) //nolint:gosec // path from config
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil //nolint:nilnil // nil signals "no file"
		}
		return nil, fmt.Errorf("read stats: %w", err)
	}

	var snap stats.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("parse stats: %w", err)
	}
	return &snap, nil
}

// nameLess is the tie-breaker: sort by (PluginName, ToolName) ascending.
func nameLess(a, b stats.ToolSummary) bool {
	if a.PluginName != b.PluginName {
		return a.PluginName < b.PluginName
	}
	return a.ToolName < b.ToolName
}

func sortSummaries(s []stats.ToolSummary, by string) {
	switch by {
	case "tokens":
		sort.Slice(s, func(i, j int) bool {
			ti := s[i].TotalInputTokens + s[i].TotalOutputTokens
			tj := s[j].TotalInputTokens + s[j].TotalOutputTokens
			if ti != tj {
				return ti > tj
			}
			return nameLess(s[i], s[j])
		})
	case "errors":
		sort.Slice(s, func(i, j int) bool {
			if s[i].ErrorCount != s[j].ErrorCount {
				return s[i].ErrorCount > s[j].ErrorCount
			}
			return nameLess(s[i], s[j])
		})
	case "name":
		sort.Slice(s, func(i, j int) bool {
			return nameLess(s[i], s[j])
		})
	default: // "calls"
		sort.Slice(s, func(i, j int) bool {
			if s[i].CallCount != s[j].CallCount {
				return s[i].CallCount > s[j].CallCount
			}
			return nameLess(s[i], s[j])
		})
	}
}

func printStatsPlain(snap *stats.Snapshot, summaries []stats.ToolSummary, showSchemas, showResources bool, dateRange string) {
	if snap.StartedAt != nil {
		fmt.Printf("# tokenizer: %s, since: %s\n", snap.Tokenizer, snap.StartedAt.Format("2006-01-02"))
	} else {
		fmt.Printf("# tokenizer: %s\n", snap.Tokenizer)
	}
	if dateRange != "" {
		fmt.Printf("# range: %s\n", dateRange)
	}

	var totalCalls, totalErrors, totalIn, totalOut int
	for _, ts := range summaries {
		fmt.Printf("%s\t%s\t%d\t%d\t%d\t%d\t%d\n",
			ts.PluginName, ts.ToolName, ts.CallCount, ts.ErrorCount,
			ts.TotalInputTokens, ts.TotalOutputTokens, ts.AvgDurationMs)
		totalCalls += ts.CallCount
		totalErrors += ts.ErrorCount
		totalIn += ts.TotalInputTokens
		totalOut += ts.TotalOutputTokens
	}
	fmt.Printf("# totals: %d calls, %d errors, %d input tokens, %d output tokens\n",
		totalCalls, totalErrors, totalIn, totalOut)

	if showSchemas {
		fmt.Println()
		schemaNames := make([]string, 0, len(snap.Schemas))
		for name := range snap.Schemas {
			schemaNames = append(schemaNames, name)
		}
		sort.Strings(schemaNames)
		for _, name := range schemaNames {
			se := snap.Schemas[name]
			fmt.Printf("schema\t%s\t%s\t%d\n", se.PluginName, name, se.TotalTokens)
		}
	}

	if showResources {
		fmt.Println()
		resourceURIs := make([]string, 0, len(snap.Resources))
		for uri := range snap.Resources {
			resourceURIs = append(resourceURIs, uri)
		}
		sort.Strings(resourceURIs)
		for _, uri := range resourceURIs {
			re := snap.Resources[uri]
			fmt.Printf("resource\t%s\t%s\t%s\t%d\t%d\t%d\n",
				re.PluginName, re.URI, re.ResourceType,
				re.ContentBytes, re.ContentTokens, re.ReadCount)
		}
	}
}

func printStatsTable(snap *stats.Snapshot, summaries []stats.ToolSummary, showSchemas, showResources bool, dateRange string) {
	w, _, _ := term.GetSize(os.Stdout.Fd())
	if w <= 0 {
		w = 100
	}

	switch {
	case dateRange != "":
		fmt.Printf("Tool Usage Stats (tokenizer: %s, %s)\n\n", snap.Tokenizer, dateRange)
	case snap.StartedAt != nil:
		fmt.Printf("Tool Usage Stats (tokenizer: %s, since %s)\n\n",
			snap.Tokenizer, snap.StartedAt.Format("2006-01-02"))
	default:
		fmt.Printf("Tool Usage Stats (tokenizer: %s)\n\n", snap.Tokenizer)
	}

	// Main calls table.
	var totalCalls, totalErrors, totalIn, totalOut int
	rows := make([][]string, 0, len(summaries))
	for _, ts := range summaries {
		rows = append(rows, []string{
			ts.PluginName,
			ts.ToolName,
			fmtInt(ts.CallCount),
			fmtInt(ts.ErrorCount),
			fmtInt(ts.TotalInputTokens),
			fmtInt(ts.TotalOutputTokens),
			fmtInt64(ts.AvgDurationMs),
		})
		totalCalls += ts.CallCount
		totalErrors += ts.ErrorCount
		totalIn += ts.TotalInputTokens
		totalOut += ts.TotalOutputTokens
	}

	if len(rows) > 0 {
		t := table.New().
			Width(w).
			Border(lipgloss.RoundedBorder()).
			BorderStyle(borderStyle).
			StyleFunc(func(row, _ int) lipgloss.Style {
				if row == table.HeaderRow {
					return headerStyle
				}
				return lipgloss.NewStyle()
			}).
			Headers("Plugin", "Tool", "Calls", "Errors", "In Tk", "Out Tk", "Avg ms").
			Rows(rows...)
		fmt.Println(t)
	}

	if dateRange != "" {
		fmt.Printf("\nTotals (%s): %s calls, %s errors, %s input tokens, %s output tokens\n",
			dateRange, fmtInt(totalCalls), fmtInt(totalErrors), fmtInt(totalIn), fmtInt(totalOut))
	} else {
		fmt.Printf("\nTotals: %s calls, %s errors, %s input tokens, %s output tokens\n",
			fmtInt(totalCalls), fmtInt(totalErrors), fmtInt(totalIn), fmtInt(totalOut))
	}

	// Schema overhead table.
	if showSchemas && len(snap.Schemas) > 0 {
		fmt.Println("\nSchema Overhead")

		type pluginSchema struct {
			name   string
			tools  int
			tokens int
		}
		byPlugin := make(map[string]*pluginSchema)
		totalSchemaTokens := 0
		for _, se := range snap.Schemas {
			ps, ok := byPlugin[se.PluginName]
			if !ok {
				ps = &pluginSchema{name: se.PluginName}
				byPlugin[se.PluginName] = ps
			}
			ps.tools++
			ps.tokens += se.TotalTokens
			totalSchemaTokens += se.TotalTokens
		}

		schemaRows := make([][]string, 0, len(byPlugin)+1)
		for _, ps := range byPlugin {
			schemaRows = append(schemaRows, []string{
				ps.name,
				fmtInt(ps.tools),
				fmtInt(ps.tokens),
			})
		}
		sort.Slice(schemaRows, func(i, j int) bool {
			return schemaRows[i][0] < schemaRows[j][0]
		})
		schemaRows = append(schemaRows, []string{
			"Total",
			fmtInt(len(snap.Schemas)),
			fmtInt(totalSchemaTokens),
		})

		st := table.New().
			Width(min(w, 50)).
			Border(lipgloss.RoundedBorder()).
			BorderStyle(borderStyle).
			StyleFunc(func(row, _ int) lipgloss.Style {
				if row == table.HeaderRow {
					return headerStyle
				}
				return lipgloss.NewStyle()
			}).
			Headers("Plugin", "Tools", "Tokens").
			Rows(schemaRows...)
		fmt.Println(st)
	}

	// Resource reads table.
	if showResources && len(snap.Resources) > 0 {
		fmt.Println("\nResource Reads")

		resRows := make([][]string, 0, len(snap.Resources))
		for _, re := range snap.Resources {
			resRows = append(resRows, []string{
				re.URI,
				re.PluginName,
				re.ResourceType,
				fmtInt(re.ContentBytes),
				fmtInt(re.ContentTokens),
				fmtInt(re.ReadCount),
			})
		}
		sort.Slice(resRows, func(i, j int) bool {
			return resRows[i][0] < resRows[j][0]
		})

		rt := table.New().
			Width(w).
			Border(lipgloss.RoundedBorder()).
			BorderStyle(borderStyle).
			StyleFunc(func(row, _ int) lipgloss.Style {
				if row == table.HeaderRow {
					return headerStyle
				}
				return lipgloss.NewStyle()
			}).
			Headers("URI", "Plugin", "Type", "Bytes", "Tokens", "Reads").
			Rows(resRows...)
		fmt.Println(rt)
	}
}

// --- stats config ---

func runStatsConfig(_ *cobra.Command, _ []string) error {
	result, err := getDiscoveryResult()
	if err != nil {
		return err
	}

	cfg := result.Config.Stats
	fmt.Printf("enabled:         %v\n", cfg.Enabled)
	fmt.Printf("tokenizer:       %s\n", cfg.Tokenizer)
	fmt.Printf("log_calls:       %v\n", cfg.LogCalls)
	fmt.Printf("persist:         %v\n", cfg.Persist)
	fmt.Printf("retention_days:  %d\n", cfg.RetentionDays)
	return nil
}

func runStatsConfigSet(_ *cobra.Command, args []string) error {
	key, value := args[0], args[1]

	validValues, ok := statsConfigKeys[key]
	if !ok {
		var keys []string
		for k := range statsConfigKeys {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return fmt.Errorf("unknown key %q; valid keys: %v", key, keys)
	}

	// Validate value. nil validValues means any value is accepted.
	if validValues != nil {
		valid := false
		for _, v := range validValues {
			if value == v {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid value %q for stats.%s; valid values: %v", value, key, validValues)
		}
	}

	result, err := getDiscoveryResult()
	if err != nil {
		return err
	}

	// Parse into typed value.
	var typedValue any
	if b, bErr := strconv.ParseBool(value); bErr == nil {
		typedValue = b
	} else {
		typedValue = value
	}

	if err := updateStatsConfig(result.ConfigPath, key, typedValue); err != nil {
		return err
	}

	fmt.Printf("stats.%s set to %v in %s\n", key, typedValue, result.ConfigPath)
	return nil
}

func updateStatsConfig(configPath, key string, value any) error {
	data, err := os.ReadFile(configPath) //nolint:gosec // config file path from user
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var doc map[string]any
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}
	if doc == nil {
		doc = make(map[string]any)
	}

	statsRaw, ok := doc["stats"]
	var statsMap map[string]any
	if ok {
		statsMap, _ = statsRaw.(map[string]any)
	}
	if statsMap == nil {
		statsMap = make(map[string]any)
	}

	statsMap[key] = value
	doc["stats"] = statsMap

	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	return atomicWriteFile(configPath, out, 0o600)
}

// --- stats reset ---

func runStatsReset(_ *cobra.Command, _ []string) error {
	result, err := getDiscoveryResult()
	if err != nil {
		return err
	}

	statsPath := filepath.Join(result.Config.Cache.Dir, "stats.json")
	if err := os.Remove(statsPath); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No stats to clear.")
			return nil
		}
		return fmt.Errorf("delete stats: %w", err)
	}

	fmt.Printf("Stats cleared (deleted %s)\n", statsPath)
	return nil
}

// --- number formatting ---

// fmtInt formats an integer with comma separators (e.g., 1234567 -> "1,234,567").
func fmtInt(n int) string {
	if n < 0 {
		return "-" + fmtInt(-n)
	}
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	r := len(s) % 3
	if r > 0 {
		b.WriteString(s[:r])
	}
	for i := r; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// fmtInt64 formats an int64 with comma separators.
func fmtInt64(n int64) string {
	if n < 0 {
		return "-" + fmtInt64(-n)
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	r := len(s) % 3
	if r > 0 {
		b.WriteString(s[:r])
	}
	for i := r; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// --- shell completion ---

func completeStatsConfigSet(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		// Complete key names.
		keys := make([]string, 0, len(statsConfigKeys))
		for k := range statsConfigKeys {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys, cobra.ShellCompDirectiveNoFileComp
	case 1:
		// Complete values for the given key.
		if values, ok := statsConfigKeys[args[0]]; ok {
			return values, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}
