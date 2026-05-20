package scout

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"archangel/cpg"

	"github.com/rs/zerolog/log"
)

// IndexResult captures the output of a scout mapping pass.
type IndexResult struct {
	FilesScanned   int      `json:"files_scanned"`
	NodesInserted  int      `json:"nodes_inserted"`
	EdgesInserted  int      `json:"edges_inserted"`
	TargetFiles    []string `json:"target_files,omitempty"`    // Narrowed file list for Pass 2
	Errors         []string `json:"errors,omitempty"`
}

// Indexer traverses a Go codebase and populates a CPG database.
type Indexer struct {
	db       *cpg.DB
	rootPath string
}

// NewIndexer creates a new scout indexer targeting a root directory.
func NewIndexer(db *cpg.DB, rootPath string) *Indexer {
	return &Indexer{
		db:       db,
		rootPath: rootPath,
	}
}

// Pass1MapWorkspace performs the Scout Mapping pass:
// walks the codebase, parses Go ASTs, and inserts function/type/variable
// nodes and call edges into the CPG database.
func (idx *Indexer) Pass1MapWorkspace() (*IndexResult, error) {
	result := &IndexResult{}
	fset := token.NewFileSet()

	err := filepath.Walk(idx.rootPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // skip inaccessible paths
		}

		// Skip hidden dirs, vendor, node_modules, .git
		if info.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || base == "vendor" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only parse .go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files during mapping pass
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		result.FilesScanned++
		relPath, _ := filepath.Rel(idx.rootPath, path)

		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", relPath, err))
			return nil
		}

		// Insert package-level declarations
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				n, e := idx.indexFunction(fset, relPath, file, d)
				result.NodesInserted += n
				result.EdgesInserted += e

			case *ast.GenDecl:
				n := idx.indexGenDecl(fset, relPath, d)
				result.NodesInserted += n
			}
		}

		return nil
	})

	if err != nil {
		return result, fmt.Errorf("scout: workspace walk failed: %w", err)
	}

	log.Info().
		Int("files_scanned", result.FilesScanned).
		Int("nodes_inserted", result.NodesInserted).
		Int("edges_inserted", result.EdgesInserted).
		Int("errors", len(result.Errors)).
		Msg("[Scout Pass 1] Workspace mapping completed.")

	return result, nil
}

// Pass2SelectTargets narrows the file set by querying the CPG for nodes
// matching a given search pattern (e.g., a variable name or function).
// Returns only the files that contain matches, protecting the auditor
// from context dilution.
func (idx *Indexer) Pass2SelectTargets(searchPattern string) ([]string, error) {
	nodes, err := idx.db.Search(searchPattern)
	if err != nil {
		return nil, fmt.Errorf("scout: search failed: %w", err)
	}

	seen := make(map[string]bool)
	var files []string
	for _, n := range nodes {
		if !seen[n.FilePath] {
			seen[n.FilePath] = true
			files = append(files, n.FilePath)
		}
	}

	log.Info().
		Str("pattern", searchPattern).
		Int("matching_nodes", len(nodes)).
		Int("target_files", len(files)).
		Msg("[Scout Pass 2] Target file selection completed.")

	return files, nil
}

// ReverseAuditTrace performs a backward caller walk from a flagged function
// to verify whether a suspected bug is reachable from public entry points.
// Returns the trace path and a boolean indicating reachability.
func (idx *Indexer) ReverseAuditTrace(functionID string, maxDepth int) ([]cpg.TraceStep, bool, error) {
	trace, err := idx.db.GetCallers(functionID, maxDepth)
	if err != nil {
		return nil, false, fmt.Errorf("scout: reverse trace failed: %w", err)
	}

	// A function is considered reachable if any caller in the chain is a
	// main() entry point, an HTTP handler, or an init() function
	reachable := false
	for _, step := range trace {
		name := strings.ToLower(step.NodeName)
		if name == "main" || name == "init" || strings.Contains(name, "handler") || strings.Contains(name, "serve") {
			reachable = true
			break
		}
	}

	log.Info().
		Str("function_id", functionID).
		Int("trace_depth", len(trace)).
		Bool("reachable", reachable).
		Msg("[Scout Reverse-Audit] Backward reachability trace completed.")

	return trace, reachable, nil
}

// --- Internal AST indexing helpers ---

// indexFunction extracts a function declaration node and its outgoing call edges.
func (idx *Indexer) indexFunction(fset *token.FileSet, relPath string, file *ast.File, fn *ast.FuncDecl) (nodesInserted, edgesInserted int) {
	pos := fset.Position(fn.Pos())
	endPos := fset.Position(fn.End())

	// Build a receiver-qualified name
	funcName := fn.Name.Name
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		if t, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
			if ident, ok := t.X.(*ast.Ident); ok {
				funcName = ident.Name + "." + funcName
			}
		} else if ident, ok := fn.Recv.List[0].Type.(*ast.Ident); ok {
			funcName = ident.Name + "." + funcName
		}
	}

	nodeID := fmt.Sprintf("%s::%s", relPath, funcName)
	node := cpg.Node{
		ID:        nodeID,
		Type:      "FUNCTION",
		Name:      funcName,
		FilePath:  relPath,
		StartLine: pos.Line,
		EndLine:   endPos.Line,
		Metadata:  map[string]interface{}{"package": file.Name.Name},
	}

	if err := idx.db.InsertNode(node); err != nil {
		log.Debug().Err(err).Str("node", nodeID).Msg("Failed to insert function node.")
	} else {
		nodesInserted++
	}

	// Walk the function body to find call expressions
	if fn.Body != nil {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			calleeName := extractCalleeName(call)
			if calleeName == "" {
				return true
			}

			// Build a best-effort callee ID (may not resolve cross-package yet)
			calleeID := fmt.Sprintf("%s::%s", relPath, calleeName)

			edge := cpg.Edge{
				Source: nodeID,
				Target: calleeID,
				Type:   "CALLS",
			}

			if err := idx.db.InsertEdge(edge); err != nil {
				log.Debug().Err(err).Str("edge", fmt.Sprintf("%s -> %s", nodeID, calleeID)).Msg("Failed to insert call edge.")
			} else {
				edgesInserted++
			}

			return true
		})
	}

	return
}

// indexGenDecl extracts type and variable declarations.
func (idx *Indexer) indexGenDecl(fset *token.FileSet, relPath string, gd *ast.GenDecl) (nodesInserted int) {
	for _, spec := range gd.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			pos := fset.Position(s.Pos())
			endPos := fset.Position(s.End())
			nodeType := "TYPE"
			if _, ok := s.Type.(*ast.InterfaceType); ok {
				nodeType = "INTERFACE"
			} else if _, ok := s.Type.(*ast.StructType); ok {
				nodeType = "STRUCT"
			}

			node := cpg.Node{
				ID:        fmt.Sprintf("%s::%s", relPath, s.Name.Name),
				Type:      nodeType,
				Name:      s.Name.Name,
				FilePath:  relPath,
				StartLine: pos.Line,
				EndLine:   endPos.Line,
			}
			if err := idx.db.InsertNode(node); err == nil {
				nodesInserted++
			}

		case *ast.ValueSpec:
			for _, name := range s.Names {
				if name.Name == "_" {
					continue
				}
				pos := fset.Position(name.Pos())
				node := cpg.Node{
					ID:        fmt.Sprintf("%s::%s", relPath, name.Name),
					Type:      "VARIABLE",
					Name:      name.Name,
					FilePath:  relPath,
					StartLine: pos.Line,
					EndLine:   pos.Line,
				}
				if err := idx.db.InsertNode(node); err == nil {
					nodesInserted++
				}
			}
		}
	}
	return
}

// extractCalleeName attempts to resolve the function name from a call expression.
func extractCalleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	}
	return ""
}
