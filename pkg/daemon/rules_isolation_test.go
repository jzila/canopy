package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/events"
	"github.com/jzila/canopy/pkg/orchestrator"
	"github.com/jzila/canopy/pkg/rules"
)

// setupTwoReposWithRules creates a test daemon with two repositories,
// each with its own orchestrator and rules engine.
func setupTwoReposWithRules(t *testing.T) (*Daemon, *rules.Engine, *rules.Engine) {
	t.Helper()

	daemon := newDaemonForTest(Config{}, nil, nil)
	daemon.Init()

	// Repo A setup
	repoAPath := "/tmp/test-repo-a"
	runAID := "run-a-123"
	rulesA := config.DefaultRulesSettings()
	engineA := rules.NewEngine(&rulesA)

	orchConfigA := &orchestrator.Config{
		WorkDir:     repoAPath,
		Concurrency: 1,
		MaxPriority: -1,
	}
	orchA, _ := orchestrator.New(orchConfigA)
	if orchA != nil {
		orchA.SetRulesEngine(engineA)
	}

	runStateA := &RunState{
		ID:        runAID,
		RepoPath:  repoAPath,
		Status:    RunStatusRunning,
		StartTime: time.Now(),
		orch:      orchA,
	}
	daemon.orchManager.runs.Store(runAID, runStateA)
	daemon.orchManager.runsByRepo.Store(repoAPath, runAID)

	// Repo B setup
	repoBPath := "/tmp/test-repo-b"
	runBID := "run-b-456"
	rulesB := config.DefaultRulesSettings()
	engineB := rules.NewEngine(&rulesB)

	orchConfigB := &orchestrator.Config{
		WorkDir:     repoBPath,
		Concurrency: 1,
		MaxPriority: -1,
	}
	orchB, _ := orchestrator.New(orchConfigB)
	if orchB != nil {
		orchB.SetRulesEngine(engineB)
	}

	runStateBB := &RunState{
		ID:        runBID,
		RepoPath:  repoBPath,
		Status:    RunStatusRunning,
		StartTime: time.Now(),
		orch:      orchB,
	}
	daemon.orchManager.runs.Store(runBID, runStateBB)
	daemon.orchManager.runsByRepo.Store(repoBPath, runBID)

	return daemon, engineA, engineB
}

// TestRulesIsolation_SeparateEnginesPerRepo verifies that each repository
// gets its own independent rules engine.
func TestRulesIsolation_SeparateEnginesPerRepo(t *testing.T) {
	daemon, engineA, engineB := setupTwoReposWithRules(t)

	// Verify engines are distinct objects
	if engineA == engineB {
		t.Fatal("expected different engine instances for different repos")
	}

	// Verify engines are retrievable by repo path
	retrievedA := daemon.orchManager.GetRulesEngineForRepo("/tmp/test-repo-a")
	retrievedB := daemon.orchManager.GetRulesEngineForRepo("/tmp/test-repo-b")

	if retrievedA != engineA {
		t.Error("GetRulesEngineForRepo returned wrong engine for repo A")
	}
	if retrievedB != engineB {
		t.Error("GetRulesEngineForRepo returned wrong engine for repo B")
	}

	// Verify engines are retrievable by run ID
	retrievedA = daemon.orchManager.GetRulesEngineForRun("run-a-123")
	retrievedB = daemon.orchManager.GetRulesEngineForRun("run-b-456")

	if retrievedA != engineA {
		t.Error("GetRulesEngineForRun returned wrong engine for run A")
	}
	if retrievedB != engineB {
		t.Error("GetRulesEngineForRun returned wrong engine for run B")
	}
}

// TestRulesIsolation_RuntimeRulesNotShared verifies that runtime rules added
// to one repository's engine don't appear in another repository's engine.
func TestRulesIsolation_RuntimeRulesNotShared(t *testing.T) {
	_, engineA, engineB := setupTwoReposWithRules(t)

	// Add a rule to repo A
	err := engineA.AddRuleWithValidation(config.CustomRule{
		Name:      "repo-a-only-rule",
		Condition: "priority > 2",
		Action:    "skip",
		Reason:    "skip high priority in repo A",
	})
	if err != nil {
		t.Fatalf("failed to add rule to engine A: %v", err)
	}

	// Verify rule exists in engine A
	ruleA := engineA.GetRule("repo-a-only-rule")
	if ruleA == nil {
		t.Error("expected rule to exist in engine A")
	}

	// Verify rule does NOT exist in engine B
	ruleB := engineB.GetRule("repo-a-only-rule")
	if ruleB != nil {
		t.Error("rule from repo A should not exist in repo B's engine")
	}

	// Add a different rule to repo B
	err = engineB.AddRuleWithValidation(config.CustomRule{
		Name:      "repo-b-only-rule",
		Condition: "type == bug",
		Action:    "allow",
		Reason:    "allow bugs in repo B",
	})
	if err != nil {
		t.Fatalf("failed to add rule to engine B: %v", err)
	}

	// Verify rule exists in engine B
	ruleB = engineB.GetRule("repo-b-only-rule")
	if ruleB == nil {
		t.Error("expected rule to exist in engine B")
	}

	// Verify rule does NOT exist in engine A
	ruleA = engineA.GetRule("repo-b-only-rule")
	if ruleA != nil {
		t.Error("rule from repo B should not exist in repo A's engine")
	}

	// Verify snapshot isolation
	snapshotA := engineA.GetSnapshot()
	snapshotB := engineB.GetSnapshot()

	// Repo A should have 1 runtime rule
	if len(snapshotA.RuntimeRules) != 1 {
		t.Errorf("expected 1 runtime rule in repo A, got %d", len(snapshotA.RuntimeRules))
	}
	if len(snapshotA.RuntimeRules) > 0 && snapshotA.RuntimeRules[0].Name != "repo-a-only-rule" {
		t.Errorf("expected repo-a-only-rule in repo A, got %s", snapshotA.RuntimeRules[0].Name)
	}

	// Repo B should have 1 runtime rule (different one)
	if len(snapshotB.RuntimeRules) != 1 {
		t.Errorf("expected 1 runtime rule in repo B, got %d", len(snapshotB.RuntimeRules))
	}
	if len(snapshotB.RuntimeRules) > 0 && snapshotB.RuntimeRules[0].Name != "repo-b-only-rule" {
		t.Errorf("expected repo-b-only-rule in repo B, got %s", snapshotB.RuntimeRules[0].Name)
	}
}

// TestRulesIsolation_ConfigSettingsNotShared verifies that config settings
// changes to one repository's engine don't affect another repository's engine.
func TestRulesIsolation_ConfigSettingsNotShared(t *testing.T) {
	_, engineA, engineB := setupTwoReposWithRules(t)

	// Update config settings in repo A
	err := engineA.UpdateConfigSettings(rules.ConfigSettingsUpdate{
		PriorityMax:   ptrInt(2),
		ExcludeLabels: &[]string{"wip", "blocked"},
	})
	if err != nil {
		t.Fatalf("failed to update config in engine A: %v", err)
	}

	// Get config from both engines
	configA := engineA.GetConfigSettings()
	configB := engineB.GetConfigSettings()

	// Verify repo A has the updated settings
	if configA.PriorityMax != 2 {
		t.Errorf("expected priority_max=2 in repo A, got %d", configA.PriorityMax)
	}
	if len(configA.ExcludeLabels) != 2 {
		t.Errorf("expected 2 exclude labels in repo A, got %d", len(configA.ExcludeLabels))
	}

	// Verify repo B still has default settings (unchanged)
	if configB.PriorityMax != -1 {
		t.Errorf("expected priority_max=-1 (default) in repo B, got %d", configB.PriorityMax)
	}
	if len(configB.ExcludeLabels) != 0 {
		t.Errorf("expected 0 exclude labels in repo B, got %d", len(configB.ExcludeLabels))
	}

	// Update repo B with different settings
	err = engineB.UpdateConfigSettings(rules.ConfigSettingsUpdate{
		PriorityMin: ptrInt(1),
		Types:       &[]string{"bug", "feature"},
	})
	if err != nil {
		t.Fatalf("failed to update config in engine B: %v", err)
	}

	// Refresh configs
	configA = engineA.GetConfigSettings()
	configB = engineB.GetConfigSettings()

	// Verify repo B has its settings
	if configB.PriorityMin != 1 {
		t.Errorf("expected priority_min=1 in repo B, got %d", configB.PriorityMin)
	}
	if len(configB.Types) != 2 {
		t.Errorf("expected 2 types in repo B, got %d", len(configB.Types))
	}

	// Verify repo A was not affected by repo B's changes
	if configA.PriorityMin != 0 {
		t.Errorf("expected priority_min=0 (default) in repo A, got %d", configA.PriorityMin)
	}
	if len(configA.Types) != 0 {
		t.Errorf("expected 0 types in repo A, got %d", len(configA.Types))
	}
}

// TestRulesIsolation_EvaluationNotAffected verifies that task evaluation
// in one repository is not affected by rules in another repository.
func TestRulesIsolation_EvaluationNotAffected(t *testing.T) {
	_, engineA, engineB := setupTwoReposWithRules(t)

	// Configure repo A to skip high priority tasks
	err := engineA.UpdateConfigSettings(rules.ConfigSettingsUpdate{
		PriorityMax: ptrInt(2),
	})
	if err != nil {
		t.Fatalf("failed to update config in engine A: %v", err)
	}

	// Configure repo B to skip low priority tasks
	err = engineB.UpdateConfigSettings(rules.ConfigSettingsUpdate{
		PriorityMin: ptrInt(2),
	})
	if err != nil {
		t.Fatalf("failed to update config in engine B: %v", err)
	}

	// Create test tasks
	highPriorityTask := &beads.Task{
		ID:       "task-high",
		Title:    "High priority task",
		Priority: 3,
		Type:     "feature",
		Status:   "open",
	}

	lowPriorityTask := &beads.Task{
		ID:       "task-low",
		Title:    "Low priority task",
		Priority: 1,
		Type:     "task",
		Status:   "open",
	}

	inFlight := make(map[string]bool)
	inFlightTasks := make(map[string]*beads.Task)

	// Evaluate high priority task in both repos
	resultA := engineA.Evaluate(highPriorityTask, inFlight, inFlightTasks)
	resultB := engineB.Evaluate(highPriorityTask, inFlight, inFlightTasks)

	// Repo A should skip (priority 3 > max 2)
	if !resultA.Skip {
		t.Error("expected repo A to skip high priority task")
	}
	// Repo B should allow (priority 3 >= min 2)
	if resultB.Skip {
		t.Error("expected repo B to allow high priority task")
	}

	// Evaluate low priority task in both repos
	resultA = engineA.Evaluate(lowPriorityTask, inFlight, inFlightTasks)
	resultB = engineB.Evaluate(lowPriorityTask, inFlight, inFlightTasks)

	// Repo A should allow (priority 1 <= max 2)
	if resultA.Skip {
		t.Error("expected repo A to allow low priority task")
	}
	// Repo B should skip (priority 1 < min 2)
	if !resultB.Skip {
		t.Error("expected repo B to skip low priority task")
	}
}

// TestRulesIsolation_CustomRulesEvaluatedIndependently verifies that custom rules
// in one repository don't affect task evaluation in another repository.
func TestRulesIsolation_CustomRulesEvaluatedIndependently(t *testing.T) {
	_, engineA, engineB := setupTwoReposWithRules(t)

	// Add a custom rule to repo A that denies bugs
	err := engineA.AddRuleWithValidation(config.CustomRule{
		Name:      "deny-bugs",
		Condition: "type == bug",
		Action:    "deny",
		Reason:    "bugs not allowed in repo A",
	})
	if err != nil {
		t.Fatalf("failed to add rule to engine A: %v", err)
	}

	// Add a custom rule to repo B that allows bugs (continues evaluation)
	err = engineB.AddRuleWithValidation(config.CustomRule{
		Name:      "allow-bugs",
		Condition: "type == bug",
		Action:    "allow",
		Reason:    "bugs allowed in repo B",
	})
	if err != nil {
		t.Fatalf("failed to add rule to engine B: %v", err)
	}

	// Create a bug task
	bugTask := &beads.Task{
		ID:       "bug-task",
		Title:    "A bug",
		Priority: 2,
		Type:     "bug",
		Status:   "open",
	}

	inFlight := make(map[string]bool)
	inFlightTasks := make(map[string]*beads.Task)

	// Evaluate bug in both repos
	resultA := engineA.Evaluate(bugTask, inFlight, inFlightTasks)
	resultB := engineB.Evaluate(bugTask, inFlight, inFlightTasks)

	// Repo A should deny bugs
	if !resultA.Skip {
		t.Error("expected repo A to deny bug task")
	}
	if resultA.SkipReason != "bugs not allowed in repo A" {
		t.Errorf("expected skip reason 'bugs not allowed in repo A', got %q", resultA.SkipReason)
	}

	// Repo B should allow bugs (allow action continues to next rule, which accepts)
	if resultB.Skip {
		t.Error("expected repo B to allow bug task")
	}
}

// TestRulesIsolation_ConcurrentAccess verifies that concurrent modifications
// to different repositories' rules engines are isolated.
func TestRulesIsolation_ConcurrentAccess(t *testing.T) {
	_, engineA, engineB := setupTwoReposWithRules(t)

	const numOperations = 100
	var wg sync.WaitGroup

	// Concurrently add rules to both engines
	for i := 0; i < numOperations; i++ {
		wg.Add(2)

		// Add rule to engine A
		go func(idx int) {
			defer wg.Done()
			rule := config.CustomRule{
				Name:      fmt.Sprintf("repo-a-rule-%d", idx),
				Condition: fmt.Sprintf("priority == %d", idx%5),
				Action:    "skip",
			}
			_ = engineA.AddRuleWithValidation(rule)
		}(i)

		// Add rule to engine B
		go func(idx int) {
			defer wg.Done()
			rule := config.CustomRule{
				Name:      fmt.Sprintf("repo-b-rule-%d", idx),
				Condition: fmt.Sprintf("type == type-%d", idx%5),
				Action:    "allow",
			}
			_ = engineB.AddRuleWithValidation(rule)
		}(i)
	}

	wg.Wait()

	// Verify rules are properly isolated
	snapshotA := engineA.GetSnapshot()
	snapshotB := engineB.GetSnapshot()

	// All repo A rules should start with "repo-a-rule-"
	for _, rule := range snapshotA.RuntimeRules {
		if len(rule.Name) < 12 || rule.Name[:12] != "repo-a-rule-" {
			t.Errorf("unexpected rule in repo A: %s", rule.Name)
		}
	}

	// All repo B rules should start with "repo-b-rule-"
	for _, rule := range snapshotB.RuntimeRules {
		if len(rule.Name) < 12 || rule.Name[:12] != "repo-b-rule-" {
			t.Errorf("unexpected rule in repo B: %s", rule.Name)
		}
	}

	// Verify each engine has the expected number of rules
	if len(snapshotA.RuntimeRules) != numOperations {
		t.Errorf("expected %d rules in repo A, got %d", numOperations, len(snapshotA.RuntimeRules))
	}
	if len(snapshotB.RuntimeRules) != numOperations {
		t.Errorf("expected %d rules in repo B, got %d", numOperations, len(snapshotB.RuntimeRules))
	}
}

// TestRulesIsolation_RuleRemovalNotShared verifies that removing a rule
// from one repository doesn't affect another repository.
func TestRulesIsolation_RuleRemovalNotShared(t *testing.T) {
	_, engineA, engineB := setupTwoReposWithRules(t)

	// Add the same-named rule to both engines
	ruleA := config.CustomRule{
		Name:      "shared-name-rule",
		Condition: "priority > 1",
		Action:    "skip",
		Reason:    "repo A version",
	}
	ruleB := config.CustomRule{
		Name:      "shared-name-rule",
		Condition: "type == bug",
		Action:    "allow",
		Reason:    "repo B version",
	}

	err := engineA.AddRuleWithValidation(ruleA)
	if err != nil {
		t.Fatalf("failed to add rule to engine A: %v", err)
	}
	err = engineB.AddRuleWithValidation(ruleB)
	if err != nil {
		t.Fatalf("failed to add rule to engine B: %v", err)
	}

	// Verify both rules exist
	if engineA.GetRule("shared-name-rule") == nil {
		t.Error("expected rule to exist in engine A")
	}
	if engineB.GetRule("shared-name-rule") == nil {
		t.Error("expected rule to exist in engine B")
	}

	// Remove rule from engine A
	removed := engineA.RemoveRule("shared-name-rule")
	if !removed {
		t.Error("expected rule to be removed from engine A")
	}

	// Verify rule is gone from engine A
	if engineA.GetRule("shared-name-rule") != nil {
		t.Error("rule should be removed from engine A")
	}

	// Verify rule still exists in engine B
	ruleInB := engineB.GetRule("shared-name-rule")
	if ruleInB == nil {
		t.Error("rule should still exist in engine B")
	}
	if ruleInB.Reason != "repo B version" {
		t.Errorf("rule in engine B should have its own reason, got %q", ruleInB.Reason)
	}
}

// TestRulesIsolation_ClearRuntimeRulesNotShared verifies that clearing
// runtime rules from one repository doesn't affect another repository.
func TestRulesIsolation_ClearRuntimeRulesNotShared(t *testing.T) {
	_, engineA, engineB := setupTwoReposWithRules(t)

	// Add multiple rules to both engines
	for i := 0; i < 5; i++ {
		_ = engineA.AddRuleWithValidation(config.CustomRule{
			Name:      fmt.Sprintf("rule-a-%d", i),
			Condition: "priority > 0",
			Action:    "skip",
		})
		_ = engineB.AddRuleWithValidation(config.CustomRule{
			Name:      fmt.Sprintf("rule-b-%d", i),
			Condition: "priority > 0",
			Action:    "allow",
		})
	}

	// Verify both engines have 5 rules
	if len(engineA.GetRuntimeRules()) != 5 {
		t.Errorf("expected 5 rules in engine A, got %d", len(engineA.GetRuntimeRules()))
	}
	if len(engineB.GetRuntimeRules()) != 5 {
		t.Errorf("expected 5 rules in engine B, got %d", len(engineB.GetRuntimeRules()))
	}

	// Clear rules from engine A
	engineA.ClearRuntimeRules()

	// Verify engine A has no rules
	if len(engineA.GetRuntimeRules()) != 0 {
		t.Errorf("expected 0 rules in engine A after clear, got %d", len(engineA.GetRuntimeRules()))
	}

	// Verify engine B still has all its rules
	rulesB := engineB.GetRuntimeRules()
	if len(rulesB) != 5 {
		t.Errorf("expected 5 rules in engine B after clearing A, got %d", len(rulesB))
	}

	// Verify the rules in B are the correct ones
	for i := 0; i < 5; i++ {
		expectedName := fmt.Sprintf("rule-b-%d", i)
		found := false
		for _, r := range rulesB {
			if r.Name == expectedName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected rule %q in engine B", expectedName)
		}
	}
}

// TestRulesIsolation_HTTPHandlerIsolation verifies that the HTTP handlers
// correctly route requests to the appropriate repository's rules engine.
func TestRulesIsolation_HTTPHandlerIsolation(t *testing.T) {
	daemon, engineA, engineB := setupTwoReposWithRules(t)
	handler := NewRulesHandler(daemon)

	// Add different rules to each engine directly
	_ = engineA.AddRuleWithValidation(config.CustomRule{
		Name:      "handler-test-a",
		Condition: "priority == 0",
		Action:    "skip",
	})
	_ = engineB.AddRuleWithValidation(config.CustomRule{
		Name:      "handler-test-b",
		Condition: "priority == 4",
		Action:    "allow",
	})

	// Test that we get different engines for different repo paths
	retrievedEngineA, errMsg := handler.getEngine(createRequestWithRepoPath("/tmp/test-repo-a"))
	if errMsg != "" {
		t.Fatalf("failed to get engine for repo A: %s", errMsg)
	}
	if retrievedEngineA != engineA {
		t.Error("handler returned wrong engine for repo A")
	}

	retrievedEngineB, errMsg := handler.getEngine(createRequestWithRepoPath("/tmp/test-repo-b"))
	if errMsg != "" {
		t.Fatalf("failed to get engine for repo B: %s", errMsg)
	}
	if retrievedEngineB != engineB {
		t.Error("handler returned wrong engine for repo B")
	}

	// Verify the rules are correctly isolated through handler retrieval
	ruleA := retrievedEngineA.GetRule("handler-test-a")
	ruleB := retrievedEngineB.GetRule("handler-test-b")

	if ruleA == nil {
		t.Error("expected handler-test-a in engine A")
	}
	if ruleB == nil {
		t.Error("expected handler-test-b in engine B")
	}

	// Verify cross-contamination didn't happen
	if retrievedEngineA.GetRule("handler-test-b") != nil {
		t.Error("handler-test-b should not exist in engine A")
	}
	if retrievedEngineB.GetRule("handler-test-a") != nil {
		t.Error("handler-test-a should not exist in engine B")
	}
}

// TestRulesIsolation_UnknownRepoReturnsNil verifies that requesting rules
// for an unknown repository returns nil, not another repo's engine.
func TestRulesIsolation_UnknownRepoReturnsNil(t *testing.T) {
	daemon, _, _ := setupTwoReposWithRules(t)

	// Request rules for a repo that doesn't exist
	engine := daemon.orchManager.GetRulesEngineForRepo("/tmp/nonexistent-repo")
	if engine != nil {
		t.Error("expected nil engine for unknown repo, got non-nil")
	}

	// Request rules for a run ID that doesn't exist
	engine = daemon.orchManager.GetRulesEngineForRun("nonexistent-run-id")
	if engine != nil {
		t.Error("expected nil engine for unknown run ID, got non-nil")
	}
}

// TestRulesIsolation_MultipleRunsSameRepoSequential verifies that when one
// run ends and another starts for the same repo, rules don't leak between them.
func TestRulesIsolation_MultipleRunsSameRepoSequential(t *testing.T) {
	eventBus := events.NewEventBus()
	state := NewRuntimeState()
	manager := NewOrchestratorManager(eventBus, state)

	repoPath := "/tmp/sequential-repo"

	// First run
	runID1 := "run-1"
	rules1 := config.DefaultRulesSettings()
	engine1 := rules.NewEngine(&rules1)

	orchConfig1 := &orchestrator.Config{
		WorkDir:     repoPath,
		Concurrency: 1,
		MaxPriority: -1,
	}
	orch1, _ := orchestrator.New(orchConfig1)
	if orch1 != nil {
		orch1.SetRulesEngine(engine1)
	}

	runState1 := &RunState{
		ID:        runID1,
		RepoPath:  repoPath,
		Status:    RunStatusRunning,
		StartTime: time.Now(),
		orch:      orch1,
	}
	manager.runs.Store(runID1, runState1)
	manager.runsByRepo.Store(repoPath, runID1)

	// Add rules to first run
	_ = engine1.AddRuleWithValidation(config.CustomRule{
		Name:      "run1-rule",
		Condition: "priority > 0",
		Action:    "skip",
	})

	// Verify rule exists
	engineRetrieved := manager.GetRulesEngineForRepo(repoPath)
	if engineRetrieved.GetRule("run1-rule") == nil {
		t.Error("expected run1-rule to exist in first run")
	}

	// "End" first run (remove from tracking)
	manager.runs.Delete(runID1)
	manager.runsByRepo.Delete(repoPath)

	// Second run with fresh engine
	runID2 := "run-2"
	rules2 := config.DefaultRulesSettings()
	engine2 := rules.NewEngine(&rules2)

	orchConfig2 := &orchestrator.Config{
		WorkDir:     repoPath,
		Concurrency: 1,
		MaxPriority: -1,
	}
	orch2, _ := orchestrator.New(orchConfig2)
	if orch2 != nil {
		orch2.SetRulesEngine(engine2)
	}

	runState2 := &RunState{
		ID:        runID2,
		RepoPath:  repoPath,
		Status:    RunStatusRunning,
		StartTime: time.Now(),
		orch:      orch2,
	}
	manager.runs.Store(runID2, runState2)
	manager.runsByRepo.Store(repoPath, runID2)

	// Verify second run has clean engine (no rules from first run)
	engineRetrieved = manager.GetRulesEngineForRepo(repoPath)
	if engineRetrieved == nil {
		t.Fatal("expected engine for second run")
	}
	if engineRetrieved.GetRule("run1-rule") != nil {
		t.Error("run1-rule should not exist in second run's engine")
	}

	// Add a different rule to second run
	_ = engine2.AddRuleWithValidation(config.CustomRule{
		Name:      "run2-rule",
		Condition: "type == feature",
		Action:    "allow",
	})

	// Verify second run has its own rule
	if engineRetrieved.GetRule("run2-rule") == nil {
		t.Error("expected run2-rule to exist in second run")
	}

	// Verify first engine still has its rule (even though run is "ended")
	if engine1.GetRule("run1-rule") == nil {
		t.Error("first engine should still have its rule (not cleared)")
	}
}

// Helper function to create a request with repo_path query parameter
func createRequestWithRepoPath(repoPath string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/rules?repo_path="+repoPath, nil)
	return req
}
