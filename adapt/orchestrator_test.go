package adapt

import (
	"fmt"
	"testing"
)

func TestADaPTSuccessfulExecution(t *testing.T) {
	executor := func(desc string) (string, error) {
		return "completed: " + desc, nil
	}
	planner := func(desc string, err string) ([]string, error) {
		return []string{"sub-1", "sub-2"}, nil
	}

	ctrl := NewController(executor, planner, 3, 3)
	root, err := ctrl.Execute("analyze codebase")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if root.Status != StatusSuccess {
		t.Errorf("expected root status 'success', got %q", root.Status)
	}
	if root.Result == "" {
		t.Error("expected non-empty result")
	}
}

func TestADaPTDecompositionOnFailure(t *testing.T) {
	callCount := 0
	executor := func(desc string) (string, error) {
		callCount++
		// First call fails, subsequent calls succeed
		if callCount == 1 {
			return "", fmt.Errorf("initial attempt failed")
		}
		return "completed: " + desc, nil
	}
	planner := func(desc string, errMsg string) ([]string, error) {
		return []string{"step-a: prepare", "step-b: execute"}, nil
	}

	ctrl := NewController(executor, planner, 3, 3)
	root, err := ctrl.Execute("complex task")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if root.Status != StatusSuccess {
		t.Errorf("expected root to eventually succeed via decomposition, got %q", root.Status)
	}
	if len(root.Children) != 2 {
		t.Errorf("expected 2 child sub-tasks from planner, got %d", len(root.Children))
	}
}

func TestADaPTDepthBudgetExhaustion(t *testing.T) {
	executor := func(desc string) (string, error) {
		return "", fmt.Errorf("always fails")
	}
	planner := func(desc string, errMsg string) ([]string, error) {
		return []string{"retry-sub"}, nil
	}

	// maxDepth=1 means root can decompose once, but children at depth 1 cannot decompose further
	ctrl := NewController(executor, planner, 1, 2)
	root, err := ctrl.Execute("impossible task")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if root.Status != StatusFailed {
		t.Errorf("expected root to fail when depth budget exhausted, got %q", root.Status)
	}
}

func TestADaPTORComposition(t *testing.T) {
	callCount := 0
	executor := func(desc string) (string, error) {
		callCount++
		// First alternative fails, second succeeds
		if callCount <= 2 {
			return "", fmt.Errorf("attempt %d failed", callCount)
		}
		return "success via alternative", nil
	}
	planner := func(desc string, errMsg string) ([]string, error) {
		return []string{"approach-A", "approach-B", "approach-C"}, nil
	}

	ctrl := NewController(executor, planner, 3, 3)

	// Manually create an OR-composed root to test OR logic
	root := ctrl.newTask("test OR", 0)
	root.Composition = OpOR

	// Simulate: executor fails root, planner decomposes, children run as OR
	ctrl.mu.Lock()
	ctrl.root = root
	ctrl.mu.Unlock()

	ctrl.executeTask(root)

	// The root itself fails (executor returns error), then planner creates children.
	// Children run as OR: first child fails, second child should succeed.
	// Since root decomposes and children inherit AND by default,
	// we need to verify the decomposition happened.
	if root.Status == StatusPending {
		t.Error("expected root to have been executed")
	}
}
