package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/daemon"
	"github.com/jzila/canopy/pkg/rules"
)

var (
	rulesJSON bool
)

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "Manage task selection rules at runtime",
	Long: `View and modify task selection rules without restarting the daemon.

Rules control which tasks are selected for execution. You can filter by
priority, type, labels, assignee, and create custom rules with conditions.

Changes made via these commands are runtime-only and not persisted to config.
Config-sourced rules can be disabled but not deleted.

EXAMPLES
  # List all rules
  canopy rules list

  # Add a runtime rule to pause frontend tasks
  canopy rules add --name "pause-frontend" \
    --condition "'frontend' in labels" \
    --action skip \
    --reason "Frontend deploy in progress"

  # Enable or disable a rule
  canopy rules enable incident-mode
  canopy rules disable incident-mode

  # Delete a runtime rule
  canopy rules delete pause-frontend

  # Update config settings
  canopy rules config --priority-max 1
  canopy rules config --exclude-labels wip,blocked`,
}

var rulesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all rules",
	Long:  `Display all task selection rules including config-sourced and runtime rules.`,
	RunE:  runRulesList,
}

var (
	addRuleName      string
	addRuleCondition string
	addRuleAction    string
	addRuleReason    string
)

var rulesAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a runtime rule",
	Long: `Add a new runtime rule for task selection.

Conditions support:
  - Priority: priority > 1, priority <= 2
  - Type: type == bug, type != feature
  - Assignee: assignee == john
  - Labels: 'frontend' in labels, 'wip' not in labels
  - Text: title contains 'fix'
  - Compound: priority <= 1 and type == bug

Actions:
  - skip: Skip tasks matching the condition
  - include: Continue checking (no-op)
  - allow: Explicitly allow, skip remaining rules
  - boost:N: Add N to priority boost
  - limit:N: Limit concurrent tasks matching to N

EXAMPLES
  canopy rules add --name "urgent-bugs" \
    --condition "priority <= 1 and type == bug" \
    --action "allow" \
    --reason "Fast-track urgent bugs"

  canopy rules add --name "limit-api" \
    --condition "'api' in labels" \
    --action "limit:2"`,
	RunE: runRulesAdd,
}

var rulesEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a rule",
	Long:  `Enable a previously disabled rule by name.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runRulesEnable,
}

var rulesDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a rule",
	Long:  `Disable a rule by name. The rule will remain but won't be evaluated.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runRulesDisable,
}

var rulesDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a runtime rule",
	Long:  `Delete a runtime rule by name. Config-sourced rules cannot be deleted.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runRulesDelete,
}

var (
	persistAll bool
)

var rulesPersistCmd = &cobra.Command{
	Use:   "persist [name]",
	Short: "Persist runtime rules to config file",
	Long: `Persist runtime rules to .canopy/config.toml so they survive daemon restarts.

Use this after creating rules via the UI or CLI during an incident to save
useful rules for future use.

EXAMPLES
  # Persist a single rule
  canopy rules persist pause-frontend

  # Persist all runtime rules
  canopy rules persist --all`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRulesPersist,
}

var (
	configPriorityMin  int
	configPriorityMax  int
	configExcludeLabel string
)

var rulesConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Update config settings",
	Long: `Update task selection config settings at runtime.

EXAMPLES
  # Set maximum priority filter
  canopy rules config --priority-max 1

  # Add labels to exclude list
  canopy rules config --exclude-labels wip,blocked,frontend`,
	RunE: runRulesConfig,
}

func init() {
	// List subcommand
	rulesListCmd.Flags().BoolVar(&rulesJSON, "json", false, "Output as JSON")
	rulesCmd.AddCommand(rulesListCmd)

	// Add subcommand
	rulesAddCmd.Flags().StringVar(&addRuleName, "name", "", "Rule name (required)")
	rulesAddCmd.Flags().StringVar(&addRuleCondition, "condition", "", "Rule condition (required)")
	rulesAddCmd.Flags().StringVar(&addRuleAction, "action", "skip", "Rule action (skip, include, allow, boost:N, limit:N)")
	rulesAddCmd.Flags().StringVar(&addRuleReason, "reason", "", "Reason shown when rule causes skip")
	_ = rulesAddCmd.MarkFlagRequired("name")
	_ = rulesAddCmd.MarkFlagRequired("condition")
	rulesCmd.AddCommand(rulesAddCmd)

	// Enable/disable subcommands
	rulesCmd.AddCommand(rulesEnableCmd)
	rulesCmd.AddCommand(rulesDisableCmd)

	// Delete subcommand
	rulesCmd.AddCommand(rulesDeleteCmd)

	// Persist subcommand
	rulesPersistCmd.Flags().BoolVar(&persistAll, "all", false, "Persist all runtime rules")
	rulesCmd.AddCommand(rulesPersistCmd)

	// Config subcommand
	rulesConfigCmd.Flags().IntVar(&configPriorityMin, "priority-min", -1, "Minimum priority (0-4)")
	rulesConfigCmd.Flags().IntVar(&configPriorityMax, "priority-max", -100, "Maximum priority (-1 for no filter, 0-4)")
	rulesConfigCmd.Flags().StringVar(&configExcludeLabel, "exclude-labels", "", "Labels to exclude (comma-separated)")
	rulesCmd.AddCommand(rulesConfigCmd)

	rootCmd.AddCommand(rulesCmd)
}

func runRulesList(cmd *cobra.Command, args []string) error {
	if err := checkDaemonRunning(); err != nil {
		return err
	}

	// Query the rules API
	resp, err := http.Get("http://localhost:8080/api/rules")
	if err != nil {
		return fmt.Errorf("failed to query rules: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to get rules: %s", strings.TrimSpace(string(body)))
	}

	var result struct {
		ConfigRules  *config.RulesSettings `json:"config_rules"`
		CustomRules  []rules.RuntimeRule   `json:"custom_rules"`
		RuntimeRules []rules.RuntimeRule   `json:"runtime_rules"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if rulesJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	// Print config rules
	fmt.Println("Config Rules")
	fmt.Println(strings.Repeat("=", 50))
	if result.ConfigRules != nil {
		printConfigRules(result.ConfigRules)
	} else {
		fmt.Println("  (no config)")
	}
	fmt.Println()

	// Print custom rules (from config)
	fmt.Println("Custom Rules (from config)")
	fmt.Println(strings.Repeat("-", 50))
	if len(result.CustomRules) > 0 {
		for _, rule := range result.CustomRules {
			printRule(rule)
		}
	} else {
		fmt.Println("  (none)")
	}
	fmt.Println()

	// Print runtime rules
	fmt.Println("Runtime Rules")
	fmt.Println(strings.Repeat("-", 50))
	if len(result.RuntimeRules) > 0 {
		for _, rule := range result.RuntimeRules {
			printRule(rule)
		}
	} else {
		fmt.Println("  (none)")
	}

	return nil
}

func printConfigRules(cfg *config.RulesSettings) {
	fmt.Printf("  Priority: %d-%d", cfg.PriorityMin, cfg.PriorityMax)
	if cfg.PriorityMax == -1 {
		fmt.Print(" (no max)")
	}
	fmt.Println()

	if len(cfg.Types) > 0 {
		fmt.Printf("  Types: %s\n", strings.Join(cfg.Types, ", "))
	}
	if len(cfg.ExcludeTypes) > 0 {
		fmt.Printf("  Exclude Types: %s\n", strings.Join(cfg.ExcludeTypes, ", "))
	}
	if len(cfg.Labels) > 0 {
		fmt.Printf("  Labels: %s\n", strings.Join(cfg.Labels, ", "))
	}
	if len(cfg.ExcludeLabels) > 0 {
		fmt.Printf("  Exclude Labels: %s\n", strings.Join(cfg.ExcludeLabels, ", "))
	}
	if cfg.Assignee != "" && cfg.Assignee != "*" {
		fmt.Printf("  Assignee: %s\n", cfg.Assignee)
	}
	if cfg.MaxConcurrent > 0 {
		fmt.Printf("  Max Concurrent: %d\n", cfg.MaxConcurrent)
	}
}

func printRule(rule rules.RuntimeRule) {
	enabledStr := "enabled"
	if rule.Enabled != nil && !*rule.Enabled {
		enabledStr = "DISABLED"
	}
	persistedStr := "runtime"
	if rule.Persisted {
		persistedStr = "persisted"
	}
	fmt.Printf("  %s [%s] (%s)\n", rule.Name, enabledStr, persistedStr)
	fmt.Printf("    Condition: %s\n", rule.Condition)
	fmt.Printf("    Action: %s\n", rule.Action)
	if rule.Reason != "" {
		fmt.Printf("    Reason: %s\n", rule.Reason)
	}
}

func runRulesAdd(cmd *cobra.Command, args []string) error {
	if err := checkDaemonRunning(); err != nil {
		return err
	}

	reqBody := map[string]string{
		"name":      addRuleName,
		"condition": addRuleCondition,
		"action":    addRuleAction,
	}
	if addRuleReason != "" {
		reqBody["reason"] = addRuleReason
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	resp, err := http.Post("http://localhost:8080/api/rules", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to add rule: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Success bool              `json:"success"`
		Rule    rules.RuntimeRule `json:"rule,omitempty"`
		Error   string            `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("failed to add rule: %s", result.Error)
	}

	fmt.Printf("Added rule: %s\n", result.Rule.Name)
	return nil
}

func runRulesEnable(cmd *cobra.Command, args []string) error {
	return updateRuleEnabled(args[0], true)
}

func runRulesDisable(cmd *cobra.Command, args []string) error {
	return updateRuleEnabled(args[0], false)
}

func updateRuleEnabled(name string, enabled bool) error {
	if err := checkDaemonRunning(); err != nil {
		return err
	}

	reqBody := map[string]bool{"enabled": enabled}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	url := fmt.Sprintf("http://localhost:8080/api/rules/%s", name)
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update rule: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("failed to update rule: %s", result.Error)
	}

	action := "enabled"
	if !enabled {
		action = "disabled"
	}
	fmt.Printf("Rule %s: %s\n", action, name)
	return nil
}

func runRulesDelete(cmd *cobra.Command, args []string) error {
	if err := checkDaemonRunning(); err != nil {
		return err
	}

	url := fmt.Sprintf("http://localhost:8080/api/rules/%s", args[0])
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete rule: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("failed to delete rule: %s", result.Error)
	}

	fmt.Printf("Deleted rule: %s\n", args[0])
	return nil
}

func runRulesConfig(cmd *cobra.Command, args []string) error {
	if err := checkDaemonRunning(); err != nil {
		return err
	}

	// Build the update request based on flags provided
	update := make(map[string]interface{})

	if configPriorityMin >= 0 {
		update["priority_min"] = configPriorityMin
	}
	if configPriorityMax >= -1 && configPriorityMax != -100 {
		update["priority_max"] = configPriorityMax
	}
	if configExcludeLabel != "" {
		labels := strings.Split(configExcludeLabel, ",")
		for i := range labels {
			labels[i] = strings.TrimSpace(labels[i])
		}
		update["exclude_labels"] = labels
	}

	if len(update) == 0 {
		return fmt.Errorf("no config changes specified; use --priority-min, --priority-max, or --exclude-labels")
	}

	body, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPatch, "http://localhost:8080/api/rules/config", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Success     bool                  `json:"success"`
		ConfigRules *config.RulesSettings `json:"config_rules,omitempty"`
		Error       string                `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("failed to update config: %s", result.Error)
	}

	fmt.Println("Config updated successfully")
	if result.ConfigRules != nil {
		printConfigRules(result.ConfigRules)
	}
	return nil
}

func runRulesPersist(cmd *cobra.Command, args []string) error {
	if err := checkDaemonRunning(); err != nil {
		return err
	}

	// Determine if we're persisting one rule or all
	if persistAll {
		return persistAllRules()
	}

	if len(args) == 0 {
		return fmt.Errorf("rule name required (or use --all to persist all runtime rules)")
	}

	return persistSingleRule(args[0])
}

func persistSingleRule(name string) error {
	url := fmt.Sprintf("http://localhost:8080/api/rules/%s/persist", name)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to persist rule: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Success    bool              `json:"success"`
		Rule       rules.RuntimeRule `json:"rule,omitempty"`
		ConfigPath string            `json:"config_path,omitempty"`
		Error      string            `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("failed to persist rule: %s", result.Error)
	}

	fmt.Printf("Persisted rule: %s\n", result.Rule.Name)
	fmt.Printf("Config saved to: %s\n", result.ConfigPath)
	return nil
}

func persistAllRules() error {
	req, err := http.NewRequest(http.MethodPost, "http://localhost:8080/api/rules/persist-all", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to persist rules: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Success    bool     `json:"success"`
		Persisted  []string `json:"persisted,omitempty"`
		ConfigPath string   `json:"config_path,omitempty"`
		Error      string   `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("failed to persist rules: %s", result.Error)
	}

	if len(result.Persisted) == 0 {
		fmt.Println("No runtime rules to persist")
		return nil
	}

	fmt.Printf("Persisted %d rules: %s\n", len(result.Persisted), strings.Join(result.Persisted, ", "))
	fmt.Printf("Config saved to: %s\n", result.ConfigPath)
	return nil
}

func checkDaemonRunning() error {
	running, _, err := daemon.IsRunning()
	if err != nil {
		return fmt.Errorf("failed to check daemon status: %w", err)
	}
	if !running {
		return fmt.Errorf("daemon is not running (start with 'canopy daemon')")
	}
	return nil
}
