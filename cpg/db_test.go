package cpg

import (
	"os"
	"testing"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "cpg_test_*.db")
	if err != nil {
		t.Fatalf("failed to create temp db file: %v", err)
	}
	tmpFile.Close()

	db, err := Open(tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("failed to open cpg database: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}
	return db, cleanup
}

func TestInsertAndGetNode(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	node := Node{
		ID:        "main.go::main",
		Type:      "FUNCTION",
		Name:      "main",
		FilePath:  "main.go",
		StartLine: 10,
		EndLine:   50,
		Metadata:  map[string]interface{}{"package": "main"},
	}

	if err := db.InsertNode(node); err != nil {
		t.Fatalf("failed to insert node: %v", err)
	}

	got, err := db.GetNode("main.go::main")
	if err != nil {
		t.Fatalf("failed to get node: %v", err)
	}

	if got.Name != "main" {
		t.Errorf("expected name 'main', got %q", got.Name)
	}
	if got.Type != "FUNCTION" {
		t.Errorf("expected type 'FUNCTION', got %q", got.Type)
	}
	if got.StartLine != 10 || got.EndLine != 50 {
		t.Errorf("expected lines 10-50, got %d-%d", got.StartLine, got.EndLine)
	}
}

func TestInsertEdgeAndGetCallers(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a call chain: main -> handler -> processData -> validateInput
	nodes := []Node{
		{ID: "main.go::main", Type: "FUNCTION", Name: "main", FilePath: "main.go", StartLine: 1, EndLine: 10},
		{ID: "server.go::handler", Type: "FUNCTION", Name: "handler", FilePath: "server.go", StartLine: 1, EndLine: 20},
		{ID: "data.go::processData", Type: "FUNCTION", Name: "processData", FilePath: "data.go", StartLine: 1, EndLine: 30},
		{ID: "validate.go::validateInput", Type: "FUNCTION", Name: "validateInput", FilePath: "validate.go", StartLine: 1, EndLine: 15},
	}
	for _, n := range nodes {
		if err := db.InsertNode(n); err != nil {
			t.Fatalf("failed to insert node %s: %v", n.ID, err)
		}
	}

	edges := []Edge{
		{Source: "main.go::main", Target: "server.go::handler", Type: "CALLS"},
		{Source: "server.go::handler", Target: "data.go::processData", Type: "CALLS"},
		{Source: "data.go::processData", Target: "validate.go::validateInput", Type: "CALLS"},
	}
	for _, e := range edges {
		if err := db.InsertEdge(e); err != nil {
			t.Fatalf("failed to insert edge %s -> %s: %v", e.Source, e.Target, err)
		}
	}

	// Trace callers of validateInput back to main
	trace, err := db.GetCallers("validate.go::validateInput", 12)
	if err != nil {
		t.Fatalf("GetCallers failed: %v", err)
	}

	if len(trace) < 3 {
		t.Fatalf("expected at least 3 trace steps (validateInput -> processData -> handler -> main), got %d", len(trace))
	}

	// Verify origin step
	if trace[0].NodeName != "validateInput" {
		t.Errorf("expected origin node 'validateInput', got %q", trace[0].NodeName)
	}

	// Verify the trace reaches main
	foundMain := false
	for _, step := range trace {
		if step.NodeName == "main" {
			foundMain = true
			break
		}
	}
	if !foundMain {
		t.Errorf("expected trace to reach 'main', but it did not")
	}
}

func TestTraceDataFlow(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	nodes := []Node{
		{ID: "config.go::apiKey", Type: "VARIABLE", Name: "apiKey", FilePath: "config.go", StartLine: 5, EndLine: 5},
		{ID: "http.go::buildRequest", Type: "FUNCTION", Name: "buildRequest", FilePath: "http.go", StartLine: 10, EndLine: 30},
		{ID: "net.go::dial", Type: "FUNCTION", Name: "dial", FilePath: "net.go", StartLine: 1, EndLine: 20},
	}
	for _, n := range nodes {
		db.InsertNode(n)
	}

	edges := []Edge{
		{Source: "config.go::apiKey", Target: "http.go::buildRequest", Type: "PDG_DATA"},
		{Source: "http.go::buildRequest", Target: "net.go::dial", Type: "PDG_DATA"},
	}
	for _, e := range edges {
		db.InsertEdge(e)
	}

	trace, err := db.TraceDataFlow("config.go::apiKey", 10)
	if err != nil {
		t.Fatalf("TraceDataFlow failed: %v", err)
	}

	if len(trace) != 3 {
		t.Fatalf("expected 3 trace steps, got %d", len(trace))
	}

	if trace[0].NodeName != "apiKey" {
		t.Errorf("expected origin 'apiKey', got %q", trace[0].NodeName)
	}
	if trace[2].NodeName != "dial" {
		t.Errorf("expected final sink 'dial', got %q", trace[2].NodeName)
	}
}

func TestStats(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.InsertNode(Node{ID: "a", Type: "FUNCTION", Name: "a", FilePath: "a.go", StartLine: 1, EndLine: 5})
	db.InsertNode(Node{ID: "b", Type: "FUNCTION", Name: "b", FilePath: "b.go", StartLine: 1, EndLine: 5})
	db.InsertEdge(Edge{Source: "a", Target: "b", Type: "CALLS"})

	nc, ec, err := db.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if nc != 2 {
		t.Errorf("expected 2 nodes, got %d", nc)
	}
	if ec != 1 {
		t.Errorf("expected 1 edge, got %d", ec)
	}
}

func TestSearch(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.InsertNode(Node{ID: "config.go::LoadConfig", Type: "FUNCTION", Name: "LoadConfig", FilePath: "config.go", StartLine: 10, EndLine: 40})
	db.InsertNode(Node{ID: "config.go::SaveConfig", Type: "FUNCTION", Name: "SaveConfig", FilePath: "config.go", StartLine: 50, EndLine: 80})
	db.InsertNode(Node{ID: "main.go::main", Type: "FUNCTION", Name: "main", FilePath: "main.go", StartLine: 1, EndLine: 10})

	results, err := db.Search("Config")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results for 'Config', got %d", len(results))
	}
}
