package cpg

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaDDL string

// Node represents a structural element in the Code Property Graph.
type Node struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`      // FUNCTION, VARIABLE, CLASS, INTERFACE, IF, LOOP, etc.
	Name      string                 `json:"name"`
	FilePath  string                 `json:"filepath"`
	StartLine int                    `json:"start_line"`
	EndLine   int                    `json:"end_line"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Edge represents a relationship between two nodes in the CPG.
type Edge struct {
	Source   string                 `json:"source"`
	Target   string                 `json:"target"`
	Type     string                 `json:"type"` // AST_CHILD, CFG_FLOW, PDG_DATA, PDG_CONTROL, CALLS, READS, WRITES
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// TraceStep represents one hop in a recursive path traversal.
type TraceStep struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
	NodeType string `json:"node_type"`
	EdgeType string `json:"edge_type"`
	Depth    int    `json:"depth"`
}

// DB wraps the SQLite connection for CPG operations.
type DB struct {
	conn *sql.DB
}

// Open initializes a new CPG database at the given path and applies the schema DDL.
func Open(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("cpg: failed to open database %q: %w", dbPath, err)
	}

	// Enable WAL mode and foreign keys for performance and integrity
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := conn.Exec(p); err != nil {
			conn.Close()
			return nil, fmt.Errorf("cpg: failed to set pragma %q: %w", p, err)
		}
	}

	// Apply schema DDL
	if _, err := conn.Exec(schemaDDL); err != nil {
		conn.Close()
		return nil, fmt.Errorf("cpg: failed to apply schema: %w", err)
	}

	log.Info().Str("path", dbPath).Msg("CPG database initialized.")
	return &DB{conn: conn}, nil
}

// Close shuts down the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// InsertNode stores a structural node into the CPG.
func (db *DB) InsertNode(n Node) error {
	meta, err := json.Marshal(n.Metadata)
	if err != nil {
		meta = []byte("{}")
	}
	_, err = db.conn.Exec(
		`INSERT OR REPLACE INTO nodes (id, type, name, filepath, start_line, end_line, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Type, n.Name, n.FilePath, n.StartLine, n.EndLine, string(meta),
	)
	return err
}

// InsertEdge stores a relationship between two nodes.
func (db *DB) InsertEdge(e Edge) error {
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		meta = []byte("{}")
	}
	_, err = db.conn.Exec(
		`INSERT OR REPLACE INTO edges (source, target, type, metadata) VALUES (?, ?, ?, ?)`,
		e.Source, e.Target, e.Type, string(meta),
	)
	return err
}

// GetNode retrieves a single node by ID.
func (db *DB) GetNode(id string) (Node, error) {
	var n Node
	var metaStr string
	err := db.conn.QueryRow(
		`SELECT id, type, name, filepath, start_line, end_line, metadata FROM nodes WHERE id = ?`, id,
	).Scan(&n.ID, &n.Type, &n.Name, &n.FilePath, &n.StartLine, &n.EndLine, &metaStr)
	if err != nil {
		return n, err
	}
	json.Unmarshal([]byte(metaStr), &n.Metadata)
	return n, nil
}

// GetNodesByFile returns all nodes belonging to a specific file path.
func (db *DB) GetNodesByFile(filepath string) ([]Node, error) {
	rows, err := db.conn.Query(
		`SELECT id, type, name, filepath, start_line, end_line, metadata FROM nodes WHERE filepath = ? ORDER BY start_line`,
		filepath,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var metaStr string
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.FilePath, &n.StartLine, &n.EndLine, &metaStr); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(metaStr), &n.Metadata)
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// GetCallers walks the CALLS edge type upstream to find all callers of a given function node.
// Uses a recursive CTE with cycle detection via path_trail and a depth limit.
func (db *DB) GetCallers(functionID string, maxDepth int) ([]TraceStep, error) {
	query := `
		WITH RECURSIVE caller_trace(node_id, node_name, node_type, edge_type, depth, path_trail) AS (
			SELECT n.id, n.name, n.type, 'ORIGIN', 0, '|' || n.id || '|'
			FROM nodes n WHERE n.id = ?
			UNION ALL
			SELECT n.id, n.name, n.type, e.type, ct.depth + 1, ct.path_trail || n.id || '|'
			FROM caller_trace ct
			JOIN edges e ON e.target = ct.node_id AND e.type = 'CALLS'
			JOIN nodes n ON n.id = e.source
			WHERE ct.depth < ?
			  AND ct.path_trail NOT LIKE '%|' || n.id || '|%'
		)
		SELECT node_id, node_name, node_type, edge_type, depth FROM caller_trace ORDER BY depth ASC
	`
	return db.executeTrace(query, functionID, maxDepth)
}

// TraceDataFlow propagates data dependency edges (PDG_DATA, READS, WRITES) downstream
// from a given variable node to locate all consumers and sinks.
func (db *DB) TraceDataFlow(variableID string, maxDepth int) ([]TraceStep, error) {
	query := `
		WITH RECURSIVE flow_trace(node_id, node_name, node_type, edge_type, depth, path_trail) AS (
			SELECT n.id, n.name, n.type, 'ORIGIN', 0, '|' || n.id || '|'
			FROM nodes n WHERE n.id = ?
			UNION ALL
			SELECT n.id, n.name, n.type, e.type, ft.depth + 1, ft.path_trail || n.id || '|'
			FROM flow_trace ft
			JOIN edges e ON e.source = ft.node_id AND e.type IN ('PDG_DATA', 'READS', 'WRITES')
			JOIN nodes n ON n.id = e.target
			WHERE ft.depth < ?
			  AND ft.path_trail NOT LIKE '%|' || n.id || '|%'
		)
		SELECT node_id, node_name, node_type, edge_type, depth FROM flow_trace ORDER BY depth ASC
	`
	return db.executeTrace(query, variableID, maxDepth)
}

// FindOverlappingCalls finds functions that share common callee targets,
// useful for detecting code duplication or integration conflicts.
func (db *DB) FindOverlappingCalls(functionID string) ([]Node, error) {
	query := `
		SELECT DISTINCT n.id, n.type, n.name, n.filepath, n.start_line, n.end_line, n.metadata
		FROM edges e1
		JOIN edges e2 ON e1.target = e2.target AND e1.source != e2.source
		JOIN nodes n ON n.id = e2.source
		WHERE e1.source = ? AND e1.type = 'CALLS' AND e2.type = 'CALLS'
	`
	rows, err := db.conn.Query(query, functionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var metaStr string
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.FilePath, &n.StartLine, &n.EndLine, &metaStr); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(metaStr), &n.Metadata)
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// Stats returns basic graph topology metrics.
func (db *DB) Stats() (nodeCount int, edgeCount int, err error) {
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&nodeCount)
	if err != nil {
		return
	}
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&edgeCount)
	return
}

// EdgeTypeSummary returns a map of edge type → count.
func (db *DB) EdgeTypeSummary() (map[string]int, error) {
	rows, err := db.conn.Query(`SELECT type, COUNT(*) FROM edges GROUP BY type ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summary := make(map[string]int)
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		summary[t] = c
	}
	return summary, nil
}

// Search performs a name-based search across all nodes.
func (db *DB) Search(pattern string) ([]Node, error) {
	query := `SELECT id, type, name, filepath, start_line, end_line, metadata FROM nodes WHERE name LIKE ? ORDER BY filepath, start_line`
	rows, err := db.conn.Query(query, "%"+strings.TrimSpace(pattern)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var metaStr string
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.FilePath, &n.StartLine, &n.EndLine, &metaStr); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(metaStr), &n.Metadata)
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// executeTrace is a helper that runs recursive CTE trace queries and collects results.
func (db *DB) executeTrace(query string, rootID string, maxDepth int) ([]TraceStep, error) {
	rows, err := db.conn.Query(query, rootID, maxDepth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []TraceStep
	for rows.Next() {
		var s TraceStep
		if err := rows.Scan(&s.NodeID, &s.NodeName, &s.NodeType, &s.EdgeType, &s.Depth); err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}
	return steps, nil
}
