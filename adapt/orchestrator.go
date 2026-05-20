package adapt

import (
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"
	StatusRunning   TaskStatus = "running"
	StatusSuccess   TaskStatus = "success"
	StatusFailed    TaskStatus = "failed"
	StatusCancelled TaskStatus = "cancelled"
)

// CompositionOp defines how child sub-tasks relate to each other.
type CompositionOp string

const (
	// OpAND means all children must succeed (Sequential Dependency).
	// A failure triggers recursive decomposition of the failed child.
	OpAND CompositionOp = "AND"

	// OpOR means any one child succeeding satisfies the parent (Conditional Exploration).
	// Children are tried in order; first success short-circuits.
	OpOR CompositionOp = "OR"
)

// Task represents a single node in the ADaPT recursive task tree.
type Task struct {
	ID          string        `json:"id"`
	Description string        `json:"description"`
	Status      TaskStatus    `json:"status"`
	Depth       int           `json:"depth"`
	MaxDepth    int           `json:"max_depth"`
	Composition CompositionOp `json:"composition"`
	Children    []*Task       `json:"children,omitempty"`
	Result      string        `json:"result,omitempty"`
	Error       string        `json:"error,omitempty"`
	StartedAt   time.Time     `json:"started_at,omitempty"`
	EndedAt     time.Time     `json:"ended_at,omitempty"`
	Attempts    int           `json:"attempts"`
	MaxAttempts int           `json:"max_attempts"`
}

// ExecutorFunc is the function signature that the Executor calls for leaf tasks.
// It receives the task description and returns (result string, error).
type ExecutorFunc func(description string) (string, error)

// PlannerFunc is the function signature that the Planner calls when a task fails.
// It receives the failed task description and the error, and returns a short-horizon
// sub-plan as a list of sub-task descriptions.
type PlannerFunc func(failedDescription string, failureError string) ([]string, error)

// Controller manages the ADaPT recursive task execution loop.
type Controller struct {
	mu          sync.Mutex
	root        *Task
	executor    ExecutorFunc
	planner     PlannerFunc
	maxDepth    int
	maxAttempts int
	taskCounter int
}

// NewController creates a new ADaPT controller.
func NewController(executor ExecutorFunc, planner PlannerFunc, maxDepth int, maxAttempts int) *Controller {
	return &Controller{
		executor:    executor,
		planner:     planner,
		maxDepth:    maxDepth,
		maxAttempts: maxAttempts,
	}
}

// Execute initiates the ADaPT loop on a root task description.
func (c *Controller) Execute(description string) (*Task, error) {
	c.mu.Lock()
	c.taskCounter = 0
	c.mu.Unlock()

	root := c.newTask(description, 0)

	c.mu.Lock()
	c.root = root
	c.mu.Unlock()

	log.Info().
		Str("task_id", root.ID).
		Str("description", description).
		Int("max_depth", c.maxDepth).
		Msg("[ADaPT] Starting root task execution.")

	c.executeTask(root)

	return root, nil
}

// Root returns the current root task tree for inspection.
func (c *Controller) Root() *Task {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.root
}

// executeTask is the recursive core of ADaPT.
func (c *Controller) executeTask(t *Task) {
	t.Status = StatusRunning
	t.StartedAt = time.Now()

	log.Info().
		Str("task_id", t.ID).
		Int("depth", t.Depth).
		Str("description", t.Description).
		Msg("[ADaPT Executor] Attempting task.")

	// --- Executor Phase ---
	result, err := c.executor(t.Description)

	if err == nil {
		// Task succeeded
		t.Status = StatusSuccess
		t.Result = result
		t.EndedAt = time.Now()
		log.Info().
			Str("task_id", t.ID).
			Dur("duration", t.EndedAt.Sub(t.StartedAt)).
			Msg("[ADaPT Executor] Task succeeded.")
		return
	}

	// --- Failure: Check depth budget ---
	t.Attempts++
	t.Error = err.Error()

	log.Warn().
		Str("task_id", t.ID).
		Int("depth", t.Depth).
		Int("attempt", t.Attempts).
		Err(err).
		Msg("[ADaPT Executor] Task failed. Evaluating decomposition.")

	if t.Depth >= t.MaxDepth {
		t.Status = StatusFailed
		t.EndedAt = time.Now()
		log.Error().
			Str("task_id", t.ID).
			Int("depth", t.Depth).
			Msg("[ADaPT] Maximum decomposition depth reached. Marking task as failed.")
		return
	}

	if t.Attempts > t.MaxAttempts {
		t.Status = StatusFailed
		t.EndedAt = time.Now()
		log.Error().
			Str("task_id", t.ID).
			Int("attempts", t.Attempts).
			Msg("[ADaPT] Maximum retry attempts exhausted. Marking task as failed.")
		return
	}

	// --- Planner Phase: Generate localized sub-plan ---
	log.Info().
		Str("task_id", t.ID).
		Msg("[ADaPT Planner] Requesting localized sub-plan from model.")

	subDescriptions, planErr := c.planner(t.Description, err.Error())
	if planErr != nil || len(subDescriptions) == 0 {
		t.Status = StatusFailed
		t.EndedAt = time.Now()
		log.Error().
			Str("task_id", t.ID).
			Err(planErr).
			Msg("[ADaPT Planner] Failed to generate sub-plan. Marking task as failed.")
		return
	}

	log.Info().
		Str("task_id", t.ID).
		Int("sub_tasks", len(subDescriptions)).
		Str("composition", string(t.Composition)).
		Msg("[ADaPT Planner] Sub-plan generated. Decomposing into children.")

	// Create child tasks
	t.Children = make([]*Task, len(subDescriptions))
	for i, desc := range subDescriptions {
		child := c.newTask(desc, t.Depth+1)
		child.Composition = OpAND // Default children to AND composition
		t.Children[i] = child
	}

	// --- Controller Phase: Execute children by composition operator ---
	switch t.Composition {
	case OpOR:
		c.executeOR(t)
	default: // OpAND or unset
		c.executeAND(t)
	}
}

// executeAND runs all children sequentially. If any fails, the parent fails.
func (c *Controller) executeAND(parent *Task) {
	for _, child := range parent.Children {
		c.executeTask(child)

		if child.Status == StatusFailed {
			parent.Status = StatusFailed
			parent.EndedAt = time.Now()
			parent.Error = fmt.Sprintf("AND child %s failed: %s", child.ID, child.Error)
			log.Warn().
				Str("parent_id", parent.ID).
				Str("failed_child", child.ID).
				Msg("[ADaPT Controller] AND composition: child failure propagated to parent.")
			return
		}
	}

	parent.Status = StatusSuccess
	parent.EndedAt = time.Now()

	// Collect results from children
	var results []string
	for _, child := range parent.Children {
		if child.Result != "" {
			results = append(results, child.Result)
		}
	}
	parent.Result = fmt.Sprintf("All %d sub-tasks completed successfully", len(parent.Children))

	log.Info().
		Str("task_id", parent.ID).
		Int("children_completed", len(parent.Children)).
		Msg("[ADaPT Controller] AND composition: all children succeeded.")
}

// executeOR runs children in order until one succeeds. First success short-circuits.
func (c *Controller) executeOR(parent *Task) {
	for _, child := range parent.Children {
		c.executeTask(child)

		if child.Status == StatusSuccess {
			parent.Status = StatusSuccess
			parent.Result = child.Result
			parent.EndedAt = time.Now()
			log.Info().
				Str("parent_id", parent.ID).
				Str("succeeded_child", child.ID).
				Msg("[ADaPT Controller] OR composition: first successful child satisfies parent.")
			return
		}

		log.Debug().
			Str("parent_id", parent.ID).
			Str("failed_child", child.ID).
			Msg("[ADaPT Controller] OR composition: child failed, trying next alternative.")
	}

	// All OR children failed
	parent.Status = StatusFailed
	parent.EndedAt = time.Now()
	parent.Error = fmt.Sprintf("OR composition: all %d alternatives failed", len(parent.Children))
	log.Error().
		Str("task_id", parent.ID).
		Msg("[ADaPT Controller] OR composition: all alternatives exhausted.")
}

// newTask creates a task with a unique ID.
func (c *Controller) newTask(description string, depth int) *Task {
	c.mu.Lock()
	c.taskCounter++
	id := fmt.Sprintf("task-%04d-d%d", c.taskCounter, depth)
	c.mu.Unlock()

	return &Task{
		ID:          id,
		Description: description,
		Status:      StatusPending,
		Depth:       depth,
		MaxDepth:    c.maxDepth,
		Composition: OpAND,
		MaxAttempts: c.maxAttempts,
	}
}

// PrintTree logs the full task tree for diagnostics.
func PrintTree(t *Task, indent string) {
	status := string(t.Status)
	log.Info().
		Str("task_id", t.ID).
		Str("status", status).
		Int("depth", t.Depth).
		Str("description", t.Description).
		Msgf("%s└── %s [%s] %s", indent, t.ID, status, t.Description)

	for _, child := range t.Children {
		PrintTree(child, indent+"    ")
	}
}
