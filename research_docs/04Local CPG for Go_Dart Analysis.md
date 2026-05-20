# **Engineering Report: Construction, Representation, and Programmatic Querying of Local Code Property Graphs for Go and Dart/Flutter Repositories**

The Code Property Graph (CPG) represents an advanced synthesis of static program analysis techniques, merging the Abstract Syntax Tree (AST), the Control Flow Graph (CFG), and the Program Dependence Graph (PDG) into a single, cohesive, directed, edge-labeled, attributed multigraph.1 Historically developed to discover complex security vulnerabilities in low-level systems code 1, the CPG allows structural and semantic properties of software to be queried simultaneously.2 Formally, a program is modeled as a graph ![][image1], where ![][image2] represents heterogeneous nodes of programming constructs (e.g., declarations, expressions, control blocks) 1, and ![][image3] represents typed edges modeling syntactic containment, execution ordering, and data dependency relationships.1  
Constructing a local CPG for mixed-language repositories containing both Go (backend) and Dart/Flutter (frontend) involves major technical challenges. These challenges include resolving implicit interface implementations in Go 6, handling asynchronous execution and mixins in Dart 8, and storing the resulting multi-layered graph in a lightweight format that remains highly performant for complex traversals.10 This report presents an engineering framework for building, storing, and programmatically querying a local CPG using open-source parsers, a optimized SQLite relational-JSON schema, and the Model Context Protocol (MCP).10

## **Technical Evaluation of Go and Dart AST Parsers**

Extracting semantic relations from codebases requires choosing between two main philosophies: compiler-native frontends that generate fully resolved intermediate representations (IR) 6, and language-agnostic incremental parsers like Tree-sitter that generate concrete syntax trees (CST).14 While Tree-sitter offers high parsing speed (![][image4] incremental updates) and robust parsing of incomplete or uncompilable source files 14, compiler-native engines provide complete, sound type resolution and reference tracking, which are critical for building reliable data-flow dependencies.7

### **Parsing Go Repositories**

To extract functions, call graphs, and program dependence relations in Go, developers typically choose between three core toolsets:

#### **Standard Compiler Toolchain (go/ssa, go/packages, go/callgraph)**

This is the most precise toolchain for analyzing Go code.6 The go/packages API loads the entire package structure, resolving Go modules and importing all dependencies.6 The source code is then lowered to a Static Single Assignment (SSA) form via go/ssa.6

* **Call Graph Resolution:** The go/callgraph package provides multiple analysis algorithms: Class Hierarchy Analysis (CHA), Rapid Type Analysis (RTA), and Points-To Analysis (PTA).7 PTA represents the most precise algorithm for resolving dynamic calls mediated through Go interfaces and function pointers, producing a sound over-approximation of the program's runtime behaviors.6  
* **Data Flow and Dependency Extraction:** Because the SSA form represents data mutations as discrete, immutable value definitions, extracting reaching definitions and definition-use chains is straightforward.18

#### **High-Level Visualizers (gocyto, go-callvis)**

The open-source library gocyto builds upon standard SSA packages (go/packages and PointerAnalysis) to construct clean call graphs.6 It formats package hierarchies, types, and attached methods into structured cytoscape nodes and edges.6 It is a significant improvement over go-callvis, which relies on deprecated package loaders, requires a system-level Graphviz dependency, and lacks native Go module support.6 gocyto is highly suitable for extracting structural call-graph topologies directly into JSON format.6

#### **Hybrid Tree-sitter Architectures (e.g., Codebase-Memory)**

Systems like *Codebase-Memory* use Tree-sitter to parse Go files, but they augment this syntactic structure with a specialized multi-pass pipeline and a local language server protocol (LSP) to resolve receiver types, pointer indirections, and package-qualified symbols.16 To map call sites to targets, *Codebase-Memory* uses a prioritized 6-strategy cascade:

1. **Import Map Resolution (0.95 confidence):** Splitting names into prefixes and matching them against file-level import directories.16  
2. **Import Suffix-Matching (0.85 confidence):** Matching unresolved suffixes against imported module paths.16  
3. **Same Module Scoping (0.90 confidence):** Prefixing local module contexts.16  
4. **Unique Reverse Index (0.75 confidence):** Global lookup for project-unique identifiers.16  
5. **Suffix Search (0.55 confidence):** Heuristic resolution based on the nearest package distance.16

### **Parsing Dart and Flutter Repositories**

Dart features unique language characteristics—such as class mixin applications 8, asynchronous Futures/Streams 23, extension methods 8, and hot-reloadable UI code—which make syntactic-only parsing difficult.8

#### **Native package:analyzer Engine**

This package is the official compiler frontend for Dart.8 It parses Dart source files via the Common Front-End (CFE) to produce serialized Kernel ASTs.9

* **AstNode and Element Split:** The engine separates syntactic constructs (AstNode, representing the written text layout) from the semantic metadata model (Element and Element2, representing resolved definitions).9  
* **The Element2 API Migration:** Modern versions of the analyzer (v8.0.0+) have migrated to the Element2 and Fragment models to support macro-augmentations and clean up legacy, unique element types.22 For example, ClassElement and ConstructorElement are replaced by composition-based ClassElement2 and ConstructorElement2 models.22  
* **Type and Call Resolution:** In a resolved AST, every SimpleIdentifier node pointing to a method call or variable lookup has a populated staticElement field.29 This field references the unique semantic definition in the element tree, providing direct access to the target method declaration, parameter types, or class inheritance paths.29

#### **Analyzer Extensions and Linters**

To write custom rules and export CPG elements, developers can build on the standard analyzer\_plugin framework 27 or use the modern analysis\_server\_plugin package introduced in Dart 3.10.32 Extending the core Plugin class and implementing visitor patterns (e.g., RecursiveAstVisitor) allows tools to intercept resolved compilation units, collect class declarations, and trace method invocations.9 The open-source custom\_lint builder wraps this complex setup.27 It runs analysis rules inside a managed framework and exposes helper classes (like CustomLintResolver and ChangeReporter) to traverse code blocks and generate AST diagnostics.30

#### **Syntactic Tree-sitter and ACER**

While AST-based call graph generators like ACER use Tree-sitter to parse multiple languages, they must implement custom, complex scope-resolution heuristics to handle Dart.14 Lacking a compiler type-solver, they rely on nested lexical walks to match receiver targets, which leads to lower precision and recall in complex Flutter codebases compared to native analyzer integrations.14  
The table below compares the primary parsing options for constructing a CPG:

| Tool / Library Name | Primary Target Language | Underpinning Engine | Core Resolution Paradigm | Storage & Export Options | Scale & Monorepo Capability |
| :---- | :---- | :---- | :---- | :---- | :---- |
| **Go callgraph (CLI)** 19 | Go | go/ssa & go/packages 6 | Static, CHA, RTA, and Points-To pointer analysis 7 | Raw digraph formats or dot/Graphviz outputs 19 | High (Optimized for full compilation context) 6 |
| **gocyto (Library)** 6 | Go | go/ssa 6 | Direct SSA-based pointer and CHA resolution 6 | Nested Cytoscape JSON & standalone HTML 6 | Moderate (Bound to complete package resolution) 6 |
| **package:analyzer** 8 | Dart / Flutter | Dart Common Front-End (CFE) 9 | Strongly-typed compiler-level Element2 resolution 15 | Programmatic traversal via Visitor APIs 9 | High (Powering the Dart Analysis Server) 26 |
| **custom\_lint\_builder** 30 | Dart / Flutter | package:analyzer 27 | Event-based intercept on compiler-resolved ASTs 30 | Programmatic AST errors, assists, and fixes 30 | High (Isolate-based modular execution) 33 |
| **Codebase-Memory** 16 | Multi-language (66+) 16 | Tree-sitter 16 | Priority 6-strategy syntactic and LSP-style heuristic cascade 16 | Statically-linked single-file SQLite DB 16 | High (Multi-threaded worker pool & incremental reindexing) 16 |

## **Relational-JSON Hybrid SQLite CPG Schema**

To store a Code Property Graph, using a traditional graph database can introduce significant operational overhead for local environments.10 Modern database research shows that a relational engine like SQLite can store and query property graphs efficiently.10 By storing structural adjacency data (the graph topology) in standard relational tables and variable attributes in JSON-typed columns, developers can achieve fast lookup performance and high schema flexibility.10

### **SQLite Database Schema Definition (DDL)**

The following schema defines a relational-JSON representation for a CPG. It includes strict check constraints, foreign key configurations, and relational indexes optimized for recursive traversals 10:

SQL  
\-- Enforce transactional integrity and parent-child cascades  
PRAGMA foreign\_keys \= ON;  
PRAGMA journal\_mode \= WAL;

\-- Table: nodes  
\-- Stores the unified programming elements representing syntax, control, and structure.  
CREATE TABLE nodes (  
    id TEXT PRIMARY KEY,  
    type TEXT NOT NULL,  
    name TEXT NOT NULL,  
    filepath TEXT NOT NULL,  
    start\_line INTEGER NOT NULL,  
    start\_column INTEGER NOT NULL,  
    end\_line INTEGER NOT NULL,  
    end\_column INTEGER NOT NULL,  
    code TEXT NOT NULL,  
    attributes TEXT DEFAULT '{}', \-- JSON containing dynamic properties (e.g., return types, visibility, modifiers)  
    CONSTRAINT chk\_node\_type CHECK (type IN (  
        'CLASS', 'INTERFACE', 'METHOD', 'VARIABLE', 'EXPRESSION', 'LITERAL', 'BLOCK', 'CALL', 'RETURN', 'METHOD\_PARAMETER'  
    )),  
    CONSTRAINT chk\_json\_attrs CHECK (json\_valid(attributes))  
) WITHOUT ROWID;

\-- Table: edges  
\-- Stores relationships representing AST hierarchy, execution flow, and data dependencies.  
CREATE TABLE edges (  
    id INTEGER PRIMARY KEY AUTOINCREMENT,  
    source\_id TEXT NOT NULL,  
    target\_id TEXT NOT NULL,  
    type TEXT NOT NULL,  
    attributes TEXT DEFAULT '{}', \-- JSON for edge-specific context (e.g., call parameter indices, variable names)  
    FOREIGN KEY (source\_id) REFERENCES nodes (id) ON DELETE CASCADE,  
    FOREIGN KEY (target\_id) REFERENCES nodes (id) ON DELETE CASCADE,  
    CONSTRAINT chk\_edge\_type CHECK (type IN (  
        'AST\_CHILD',     \-- Syntactic nesting  
        'CFG\_FLOW',      \-- Successive execution steps  
        'PDG\_CONTROL',   \-- Conditional branching guards  
        'PDG\_DATA',      \-- Reaching definitions / data dependency  
        'CALLS',         \-- Method invocation links  
        'OVERRIDES',     \-- Polymorphic interface/class overrides  
        'READS',         \-- Reads from a variable  
        'WRITES'         \-- Writes/definitions to a variable  
    )),  
    CONSTRAINT chk\_json\_edge\_attrs CHECK (json\_valid(attributes))  
);

\-- Optimization Indexes  
\-- These indexes are critical for multi-hop recursive queries and symbol lookups.  
CREATE INDEX idx\_nodes\_lookup ON nodes (name, type, filepath);  
CREATE INDEX idx\_edges\_source\_type ON edges (source\_id, type);  
CREATE INDEX idx\_edges\_target\_type ON edges (target\_id, type);  
CREATE UNIQUE INDEX idx\_edges\_dedup ON edges (source\_id, target\_id, type);

### **Mapping Go and Dart Language Constructs**

The table below outlines how programming elements from both Go and Dart are normalized into this unified relational schema:

| Target Language Construct | Go AST / SSA Element | Dart Analyzer AST / Element | CPG Node Type | Associated CPG Edge Types & Direction |
| :---- | :---- | :---- | :---- | :---- |
| **Class or Struct Declaration** | type Spec with structType 18 | ClassDeclaration / ClassElement2 8 | CLASS | AST\_CHILD (to Methods), INHERITS\_FROM (to parent Class) 4 |
| **Interface Definition** | interfaceType 18 | ClassDeclaration with isInterface or mixin constructs 8 | INTERFACE | AST\_CHILD (to abstract Methods) 4 |
| **Method / Function Declaration** | \*ssa.Function 18 | MethodDeclaration / MethodElement2 9 | METHOD | AST\_CHILD (from Class/Struct node), OVERRIDES (to parent Interface Method) 4 |
| **Local Variable Declaration** | \*ssa.Alloc 18 | VariableDeclaration / LocalVariableElement2 15 | VARIABLE | AST\_CHILD (from Block/Method node), WRITES (from assignment Statement) 4 |
| **Method Parameter** | \*ssa.Parameter 18 | FormalParameter / FormalParameterElement2 8 | METHOD\_PARAMETER | AST\_CHILD (from Method node) 4 |
| **Method Invocation Call Site** | \*ssa.Call 18 | MethodInvocation / SimpleIdentifier resolving to element 29 | CALL | CALLS (to target Method node), AST\_CHILD (from parent Block) 4 |
| **Variable Assignment (Write)** | \*ssa.Store 18 | AssignmentExpression writing to an Identifier 38 | EXPRESSION | WRITES (to target Variable node), READS (from source value expressions) 37 |
| **Variable Reference (Read)** | \*ssa.Value usage 18 | SimpleIdentifier reading an identifier 29 | EXPRESSION | READS (from target Variable node) 37 |

### **Extracting Control and Data Dependencies (PDG)**

To construct the Program Dependence Graph (PDG) within this SQLite database, the parsing pipeline must compute both control and data dependencies 2:

#### **Control Dependence (PDG\_CONTROL)**

Control dependence measures how execution flows are altered by conditional branches.21

* **Mechanism:** The parser computes the post-dominance tree of the basic blocks in the CFG.21 A node ![][image5] is control-dependent on a node ![][image6] if there is an execution path from ![][image6] to ![][image5] such that ![][image5] post-dominates all nodes along that path except ![][image6] itself.39  
* **Database Representation:** A PDG\_CONTROL edge is written from the conditional predicate expression node (e.g., if (x \< MAX)) to the statements inside the branch (e.g., y \= 2 \* x).3

#### **Data Dependence (PDG\_DATA)**

Data dependence (reaching definitions) tracks how data moves through variables.2

* **Mechanism:** In Go, the go/ssa form makes this extraction straightforward.18 Because every variable is defined exactly once, any statement that consumes an SSA register contains a direct data dependency edge from that register's definition.18 In Dart, the parser must compute these relations using classic reaching definitions analysis.37 For a variable ![][image7], a definition of ![][image7] at statement ![][image8] reaches a use at statement ![][image9] if there is an execution path in the CFG from ![][image8] to ![][image9] along which ![][image7] is not redefined.37  
* **Database Representation:** A PDG\_DATA edge is written from the statement that defines the variable to the statement that consumes it.2

## **Graph Traversal and Path Tracking via SQLite Recursive CTEs**

Relational databases can execute recursive graph traversals using recursive Common Table Expressions (CTEs).10 This allows multi-hop path tracking directly inside the SQLite query engine.10

### **Implementing BFS and DFS in SQLite**

By changing the relational join logic and queue sorting, recursive CTEs can support different search strategies:

* **Breadth-First Search (BFS):** To explore the graph level-by-level, the query stores a depth tracking column and uses UNION ALL to evaluate nodes sequentially based on their distance from the root.36  
* **Depth-First Search (DFS):** To follow paths to their terminal nodes before backtracking, the query maintains a path tracking array (or text trail) and traverses nodes based on the order they are added to the search path.41

### **Multi-Hop Programmatic Taint Tracking Query**

The following SQL query tracks data-flow propagation. It traces how data moves from a source variable (e.g., httpConfig) through an arbitrary number of assignments (PDG\_DATA edges) and function boundaries (CALLS edges) until it reaches a sensitive execution sink 2:

SQL  
WITH RECURSIVE taint\_tracker(  
    current\_node\_id,  
    current\_node\_name,  
    current\_node\_type,  
    filepath,  
    start\_line,  
    depth,  
    path\_trail,  
    cycle\_detected  
) AS (  
    \-- Anchor Member: Locate the untrusted entry point (the source variable)  
    SELECT   
        n.id AS current\_node\_id,  
        n.name AS current\_node\_name,  
        n.type AS current\_node\_type,  
        n.filepath AS filepath,  
        n.start\_line AS start\_line,  
        0 AS depth,  
        n.id || ' \[' || n.name || '\]' AS path\_trail,  
        0 AS cycle\_detected  
    FROM nodes n  
    WHERE n.type \= 'VARIABLE'   
      AND n.name \= 'httpConfig'

    UNION ALL

    \-- Recursive Member: Traverse outgoing data dependency and call edges  
    SELECT   
        target.id AS current\_node\_id,  
        target.name AS current\_node\_name,  
        target.type AS current\_node\_type,  
        target.filepath AS filepath,  
        target.start\_line AS start\_line,  
        tracker.depth \+ 1 AS depth,  
        tracker.path\_trail || ' \-\> ' || e.type || ' \-\> ' || target.id || ' \[' || target.name || '\]' AS path\_trail,  
        \-- Detect cycles by checking if the target ID is already in our path history  
        CASE   
            WHEN instr(tracker.path\_trail, target.id) \> 0 THEN 1   
            ELSE 0   
        END AS cycle\_detected  
    FROM taint\_tracker tracker  
    JOIN edges e ON tracker.current\_node\_id \= e.source\_id  
    JOIN nodes target ON e.target\_id \= target.id  
    WHERE   
        \-- Traverse only data-flow and invocation relationships  
        e.type IN ('PDG\_DATA', 'CALLS')  
        \-- Limit traversal depth to prevent path explosion in large codebases  
        AND tracker.depth \< 12  
        \-- Stop executing the current branch if a cycle is detected  
        AND tracker.cycle\_detected \= 0  
)  
SELECT   
    depth,  
    current\_node\_type AS terminal\_type,  
    current\_node\_name AS terminal\_name,  
    filepath,  
    start\_line,  
    path\_trail  
FROM taint\_tracker  
WHERE cycle\_detected \= 0  
ORDER BY depth ASC;

### **Explaining the Query Components**

1. **The Anchor Member:** Seeds the recursion queue by selecting the root nodes (depth 0\) that match the source criteria.10  
2. **The Recursive Member:** Joins the active queue with the edges table on source\_id.10 By restricting the join condition to e.type IN ('PDG\_DATA', 'CALLS'), the query filters out unrelated structural nodes (like syntax nesting edges) and focuses only on data propagation.2  
3. **Cycle Guard:** Because real-world call and data-flow graphs are highly cyclic, traversals can trigger infinite loops if they encounter recursion.41 The query uses instr(tracker.path\_trail, target.id) to check if the target node is already in the active path trail.10 If it is, the query flags a cycle and stops traversing that branch.10  
4. **Depth Guard:** The clause tracker.depth \< 12 ensures that the query halts even when analyzing very deep dependency chains, protecting database resources.10

### **Sample Output Trace**

When analyzing a typical program, the query output traces how the configuration variable flows through intermediate functions down to a network connection call:

| Depth | Terminal Type | Terminal Name | Filepath | Start Line | Path Trail |
| :---- | :---- | :---- | :---- | :---- | :---- |
| **0** | VARIABLE | httpConfig | config.go | 14 | n01 \[httpConfig\] |
| **1** | CALL | initServer | config.go | 28 | n01 \[httpConfig\] \-\> CALLS \-\> n45 |
| **2** | METHOD\_PARAMETER | cfg | server.go | 102 | ... \-\> n45 \-\> PDG\_DATA \-\> n52 \[cfg\] |
| **3** | EXPRESSION | cfg.BindAddress | server.go | 115 | ... \-\> n52 \[cfg\] \-\> PDG\_DATA \-\> n60 |
| **4** | CALL | net.Listen | network.go | 44 | ... \-\> n60 \-\> CALLS \-\> n99 \[net.Listen\] |

## **Programmatic CPG Querying via Model Context Protocol**

The Model Context Protocol (MCP) is an open-standard, bidirectional integration framework designed to connect Large Language Models (LLMs) with local tools, databases, and services.13 Rather than granting an AI agent direct access to raw source files or requiring it to construct complex SQL queries from scratch, an MCP server can expose domain-specific tools that wrap the underlying CPG database.12

\+--------------------------------------------------------------+  
|                        MCP Host (IDE)                        |  
|                                                              |  
|   \+------------------+             \+--------------------+    |  
|   |  AI Agent (LLM)  |             |     CPG Client     |    |  
|   \+------------------+             \+--------------------+    |  
|            |                                 |               |  
|            | Request: "Trace 'httpConfig'"   |               |  
|            |--------------------------------\>|               |  
\+------------|---------------------------------|---------------+  
             |                                 |  
             |                                 | Standard I/O (JSON-RPC 2.0)  
             |                                 v  
\+------------|-------------------------------------------------+  
|            |               CPG MCP Server                    |  
|            |                                                 |  
|            | 1\. Intercept \`tools/call\`                       |  
|            | 2\. Run Recursive SQLite CTE Query               |  
|            | 3\. Parse Relational Rows into JSON Array        |  
|            |                                                 |  
|            \+---------------------------------+               |  
|                                              |               |  
|                                              v               |  
|                                     \+-----------------+      |  
|                                     | Local SQLite DB |      |  
|                                     \+-----------------+      |  
\+--------------------------------------------------------------+

To prevent stream corruption in STDIO-based MCP servers, all developer logs, diagnostics, and errors must be routed strictly to standard error (stderr).45 The standard output (stdout) is reserved exclusively for raw, serialized JSON-RPC 2.0 payloads.13

### **MCP Tool Schema Definitions**

The CPG MCP server exposes three main tools to the AI agent 43:

JSON  
{  
  "tools":,  
        "additionalProperties": false  
      }  
    },  
    {  
      "name": "get\_symbol\_definition",  
      "description": "Retrieves the exact code declaration, AST children, and dynamic metadata for a specific class, interface, or method identifier.",  
      "inputSchema": {  
        "type": "object",  
        "properties": {  
          "symbol\_name": {  
            "type": "string",  
            "description": "The exact identifier name of the symbol (e.g., 'AuthService')."  
          },  
          "symbol\_type": {  
            "type": "string",  
            "enum":,  
            "description": "The matching semantic node classification."  
          }  
        },  
        "required": \["symbol\_name", "symbol\_type"\],  
        "additionalProperties": false  
      }  
    },  
    {  
      "name": "find\_overlapping\_calls",  
      "description": "Searches the call graph to locate overlapping invocation targets matching a pattern, enabling discovery of polymorphic dispatch sites.",  
      "inputSchema": {  
        "type": "object",  
        "properties": {  
          "method\_name": {  
            "type": "string",  
            "description": "The identifier of the invoked method (e.g., 'Verify')."  
          }  
        },  
        "required": \["method\_name"\],  
        "additionalProperties": false  
      }  
    }  
  \]  
}

### **Protocol-Level JSON-RPC Transaction Payloads**

When an AI agent invokes these tools, the host and server exchange structured JSON-RPC messages 42:

#### **Request Payload (Agent invoking trace\_variable\_flow)**

JSON  
{  
  "jsonrpc": "2.0",  
  "id": "req\_8871263",  
  "method": "tools/call",  
  "params": {  
    "name": "trace\_variable\_flow",  
    "arguments": {  
      "variable\_name": "httpConfig",  
      "max\_depth": 5  
    }  
  }  
}

#### **Successful Response Payload (Returning structured graph traversals)**

JSON  
{  
  "jsonrpc": "2.0",  
  "id": "req\_8871263",  
  "result": {  
    "content":"  
        }  
      }  
    \],  
    "isError": false  
  }  
}

#### **Error Handling and Execution Failures**

If the database engine fails or validation checks fail, the server returns structured error details.43 The protocol separates protocol-level failures (e.g., calling an unknown tool) from application-level execution errors 44:

* **Protocol Error (Incorrect JSON-RPC structure):**  
  JSON  
  {  
    "jsonrpc": "2.0",  
    "id": "req\_8871263",  
    "error": {  
      "code": \-32602,  
      "message": "Invalid parameter structure for trace\_variable\_flow: 'max\_depth' must be an integer."  
    }  
  }

* **Tool Execution Error (Valid protocol, failed backend execution):**  
  JSON  
  {  
    "jsonrpc": "2.0",  
    "id": "req\_8871263",  
    "result": {  
      "content":,  
      "isError": true  
    }  
  }

### **Tracing Variables: Programmatic Traversals vs. Text Grep**

Using a structured CPG via an MCP interface is highly effective compared to traditional, line-by-line textual search (grep) 2:

* **Contextual Accuracy:** Text-based search engines cannot differentiate between distinct scopes.37 A search for "config" or "address" in a large monorepo returns hundreds of matches across comments, structure definitions, log files, and unrelated local scopes.35 In contrast, the CPG distinguishes nodes using unique IDs linked to their file paths, namespace boundaries, and specific lexical scopes, returning only matches in the targeted execution path.16  
* **Handling Variable Renames and Shadowing:** When data is passed through function boundaries, parameter names often change (e.g., from httpConfig to cfg or a).14 Text search cannot resolve these transitions automatically.14 The CPG traverses these boundaries using CALLS and PDG\_DATA edges, tracking how values flow across different identifiers without losing context.14  
* **Asynchronous execution and callbacks:** In Dart/Flutter repositories, values are frequently wrapped in async closures or passed to UI widgets.23 Grep searches fail on these asynchronous structures because they rely on linear execution order.23 The CPG connects execution points directly via control-dependence and data-dependency edges, allowing agents to trace the data path even when statements are separated by complex asynchronous boundaries.2

#### **Works cited**

1. Code Property Graph | Joern Documentation, accessed May 20, 2026, [https://docs.joern.io/code-property-graph/](https://docs.joern.io/code-property-graph/)  
2. Code Property Graph \- Apiiro, accessed May 20, 2026, [https://apiiro.com/glossary/code-property-graph/](https://apiiro.com/glossary/code-property-graph/)  
3. Code property graph \- Wikipedia, accessed May 20, 2026, [https://en.wikipedia.org/wiki/Code\_property\_graph](https://en.wikipedia.org/wiki/Code_property_graph)  
4. Code Property Graph Specification 1.1 \- Joern, accessed May 20, 2026, [https://cpg.joern.io/](https://cpg.joern.io/)  
5. Code property graphs for analysis \- Fluid Attacks, accessed May 20, 2026, [https://fluidattacks.com/blog/code-property-graphs-for-analysis](https://fluidattacks.com/blog/code-property-graphs-for-analysis)  
6. protolambda/gocyto: Callgraph analysis and visualization for Go \- GitHub, accessed May 20, 2026, [https://github.com/protolambda/gocyto](https://github.com/protolambda/gocyto)  
7. callgraph package \- golang.org/x/tools/go/callgraph \- Go Packages, accessed May 20, 2026, [https://pkg.go.dev/golang.org/x/tools/go/callgraph](https://pkg.go.dev/golang.org/x/tools/go/callgraph)  
8. analyzer changelog | Dart package \- Pub.dev, accessed May 20, 2026, [https://pub.dev/packages/analyzer/changelog](https://pub.dev/packages/analyzer/changelog)  
9. The Anatomy of Dart Code Analysis: Understanding Key Entities | by Sergey Aliyev | Medium, accessed May 20, 2026, [https://medium.com/@lordjadawin/the-anatomy-of-dart-code-analysis-understanding-key-entities-ba75cf20d8ba](https://medium.com/@lordjadawin/the-anatomy-of-dart-code-analysis-understanding-key-entities-ba75cf20d8ba)  
10. SQLite as a Graph Database: Recursive CTEs, Semantic Search ..., accessed May 20, 2026, [https://dev.to/rohansx/sqlite-as-a-graph-database-recursive-ctes-semantic-search-and-why-we-ditched-neo4j-1ai](https://dev.to/rohansx/sqlite-as-a-graph-database-recursive-ctes-semantic-search-and-why-we-ditched-neo4j-1ai)  
11. SQLGraph: An Efficient Relational-Based Property Graph Store \- Google Research, accessed May 20, 2026, [https://research.google.com/pubs/archive/43287.pdf](https://research.google.com/pubs/archive/43287.pdf)  
12. SQLite MCP Server \- LobeHub, accessed May 20, 2026, [https://lobehub.com/mcp/rvarun11-sqlite-mcp](https://lobehub.com/mcp/rvarun11-sqlite-mcp)  
13. What is Model Context Protocol (MCP)? A guide | Google Cloud, accessed May 20, 2026, [https://cloud.google.com/discover/what-is-model-context-protocol](https://cloud.google.com/discover/what-is-model-context-protocol)  
14. An AST-based Call Graph Generator Framework \- ACER \- arXiv, accessed May 20, 2026, [https://arxiv.org/pdf/2308.15669](https://arxiv.org/pdf/2308.15669)  
15. element library \- Dart API \- Pub.dev, accessed May 20, 2026, [https://pub.dev/documentation/analyzer/latest/dart\_element\_element](https://pub.dev/documentation/analyzer/latest/dart_element_element)  
16. Codebase-Memory: Tree-Sitter-Based Knowledge Graphs for LLM Code Exploration via MCP \- arXiv, accessed May 20, 2026, [https://arxiv.org/html/2603.27277v1](https://arxiv.org/html/2603.27277v1)  
17. Incremental Parsing Using Tree-sitter \- Strumenta \- Federico Tomassetti, accessed May 20, 2026, [https://tomassetti.me/incremental-parsing-using-tree-sitter/](https://tomassetti.me/incremental-parsing-using-tree-sitter/)  
18. ssa package \- golang.org/x/tools/go/ssa \- Go Packages, accessed May 20, 2026, [https://pkg.go.dev/golang.org/x/tools/go/ssa](https://pkg.go.dev/golang.org/x/tools/go/ssa)  
19. go \- Creating call graph \- Stack Overflow, accessed May 20, 2026, [https://stackoverflow.com/questions/31362332/creating-call-graph](https://stackoverflow.com/questions/31362332/creating-call-graph)  
20. Want a tool to do static analysis of Go code \- Google Groups, accessed May 20, 2026, [https://groups.google.com/g/golang-nuts/c/P6734K9VzNc](https://groups.google.com/g/golang-nuts/c/P6734K9VzNc)  
21. Efficiently computing static single assignment form and the control dependence graph \- UT Austin Computer Science, accessed May 20, 2026, [https://www.cs.utexas.edu/\~pingali/CS380C/2010/papers/ssaCytron.pdf](https://www.cs.utexas.edu/~pingali/CS380C/2010/papers/ssaCytron.pdf)  
22. Diff \- 8020e36f291f9eb90139c3af946f108c771c782f^\! \- sdk \- dart Git repositories \- Git at Google, accessed May 20, 2026, [https://dart.googlesource.com/sdk/+/8020e36f291f9eb90139c3af946f108c771c782f%5E%21/](https://dart.googlesource.com/sdk/+/8020e36f291f9eb90139c3af946f108c771c782f%5E%21/)  
23. package:analyzer When the parameter type is Function type , type.element is getting 'null', accessed May 20, 2026, [https://stackoverflow.com/questions/61651285/packageanalyzer-when-the-parameter-type-is-function-type-type-element-is-gett](https://stackoverflow.com/questions/61651285/packageanalyzer-when-the-parameter-type-is-function-type-type-element-is-gett)  
24. saropa\_lints 12.0.0 changelog | Dart package \- Pub.dev, accessed May 20, 2026, [https://pub.dev/packages/saropa\_lints/versions/12.0.0/changelog](https://pub.dev/packages/saropa_lints/versions/12.0.0/changelog)  
25. pkg/analyzer/CHANGELOG.md, accessed May 20, 2026, [https://dart.googlesource.com/sdk//+/d6a763ebd16c52091b3c6220d320abc900d75442/pkg/analyzer/CHANGELOG.md](https://dart.googlesource.com/sdk//+/d6a763ebd16c52091b3c6220d320abc900d75442/pkg/analyzer/CHANGELOG.md)  
26. Customizing static analysis \- Dart programming language, accessed May 20, 2026, [https://dart.dev/tools/analysis](https://dart.dev/tools/analysis)  
27. Compatibility issue with analyzer\_plugin 0.13.0 and analyzer 7.4.5 \#60899 \- GitHub, accessed May 20, 2026, [https://github.com/dart-lang/sdk/issues/60899](https://github.com/dart-lang/sdk/issues/60899)  
28. build changelog | Dart package \- Pub.dev, accessed May 20, 2026, [https://pub.dev/packages/build/changelog](https://pub.dev/packages/build/changelog)  
29. How can I replace all matches of variables programatically? \- Google Groups, accessed May 20, 2026, [https://groups.google.com/a/dartlang.org/g/analyzer-discuss/c/7-B75W1MG5k](https://groups.google.com/a/dartlang.org/g/analyzer-discuss/c/7-B75W1MG5k)  
30. Create your own lint rules with custom lint \- Charles's Blog, accessed May 20, 2026, [https://charlescyt.github.io/create-your-own-lint-rules-with-custom-lint](https://charlescyt.github.io/create-your-own-lint-rules-with-custom-lint)  
31. aikalant/dart-custom-analyzer-plugin \- GitHub, accessed May 20, 2026, [https://github.com/aikalant/dart-custom-analyzer-plugin](https://github.com/aikalant/dart-custom-analyzer-plugin)  
32. Analyzer plugins \- Dart programming language, accessed May 20, 2026, [https://dart.dev/tools/analyzer-plugins](https://dart.dev/tools/analyzer-plugins)  
33. Creating Your First Dart Analyzer Plugin \- Very Good Ventures, accessed May 20, 2026, [https://verygood.ventures/blog/creating-your-first-dart-analyzer-plugin-with-the-new-plugin-system/](https://verygood.ventures/blog/creating-your-first-dart-analyzer-plugin-with-the-new-plugin-system/)  
34. Is JavaScript Call Graph Extraction Solved Yet? A Comparative Study of Static and Dynamic Tools \- IEEE Xplore, accessed May 20, 2026, [https://ieeexplore.ieee.org/iel7/6287639/10005208/10066273.pdf](https://ieeexplore.ieee.org/iel7/6287639/10005208/10066273.pdf)  
35. I built a local knowledge graph for codebases \- query your code with natural language : r/rust \- Reddit, accessed May 20, 2026, [https://www.reddit.com/r/rust/comments/1s5fsh2/i\_built\_a\_local\_knowledge\_graph\_for\_codebases/](https://www.reddit.com/r/rust/comments/1s5fsh2/i_built_a_local_knowledge_graph_for_codebases/)  
36. The WITH Clause \- SQLite, accessed May 20, 2026, [https://sqlite.org/lang\_with.html](https://sqlite.org/lang_with.html)  
37. Data Flow Graph Construction \- abstract syntax tree \- Stack Overflow, accessed May 20, 2026, [https://stackoverflow.com/questions/15087195/data-flow-graph-construction](https://stackoverflow.com/questions/15087195/data-flow-graph-construction)  
38. A Minimal Static Call Graph for Python Programs \- Rahul Gopinath, accessed May 20, 2026, [https://rahul.gopinath.org/post/2022/02/16/python-callgraph/](https://rahul.gopinath.org/post/2022/02/16/python-callgraph/)  
39. The Program Dependence Graph and Its Use in Optimization \- Electrical Engineering and Computer Science, accessed May 20, 2026, [https://web.eecs.umich.edu/\~mahlke/courses/583f23/reading/ferrante\_toplas\_87.pdf](https://web.eecs.umich.edu/~mahlke/courses/583f23/reading/ferrante_toplas_87.pdf)  
40. Data Flow Analysis for Go | The GoLand Blog, accessed May 20, 2026, [https://blog.jetbrains.com/go/2024/03/26/data-flow-analysis-for-go/](https://blog.jetbrains.com/go/2024/03/26/data-flow-analysis-for-go/)  
41. Recursive CTEs for Graph Traversal: Implementing BFS and DFS in SQL | by M. Ali Khan, accessed May 20, 2026, [https://medium.com/@muhammadalikhan0003/recursive-ctes-for-graph-traversal-implementing-bfs-and-dfs-in-sql-8ad240e392b4](https://medium.com/@muhammadalikhan0003/recursive-ctes-for-graph-traversal-implementing-bfs-and-dfs-in-sql-8ad240e392b4)  
42. Model Context Protocol (MCP): A comprehensive introduction for developers \- Stytch, accessed May 20, 2026, [https://stytch.com/blog/model-context-protocol-introduction/](https://stytch.com/blog/model-context-protocol-introduction/)  
43. Tools \- What is the Model Context Protocol (MCP)?, accessed May 20, 2026, [https://modelcontextprotocol.io/specification/2025-11-25/server/tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)  
44. Tools \- Model Context Protocol, accessed May 20, 2026, [https://modelcontextprotocol.io/specification/draft/server/tools](https://modelcontextprotocol.io/specification/draft/server/tools)  
45. Build an MCP server \- Model Context Protocol, accessed May 20, 2026, [https://modelcontextprotocol.io/docs/develop/build-server](https://modelcontextprotocol.io/docs/develop/build-server)  
46. How the Model Context Protocol (MCP) Works \- Lucidworks, accessed May 20, 2026, [https://lucidworks.com/blog/how-the-model-context-protocol-works-a-technical-deep-dive](https://lucidworks.com/blog/how-the-model-context-protocol-works-a-technical-deep-dive)  
47. Get Control flow graph from Abstract Syntax Tree \- Stack Overflow, accessed May 20, 2026, [https://stackoverflow.com/questions/92537/get-control-flow-graph-from-abstract-syntax-tree](https://stackoverflow.com/questions/92537/get-control-flow-graph-from-abstract-syntax-tree)

[image1]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAGYAAAAaCAYAAABFPynYAAADH0lEQVR4Xu2ZzctNQRzHfyEvoSgLEguhLKRsvEReQ15SLJTNpZCIFHlJEklSokRJREoWyj9AT2SBhYUFoUSKhRKyIK+/bzPTnedrZs6955577+lxPvXtOef7mzkzZ86cmd89j0hFRcX/wRI2SsoaNopgrGqX6oJqgufP9o67wVbVfjZLylrVZTbzck31R/VStUI1SXVe9V41y8a6xTjVO/I+iOmT0zcx/fS5a2NOp3uHGwIT0r9GSittHfBItd47zwUu+kM1lAPKATHxJxzoIGh/MJsWxH6y6XFftZTNHHwR01YMxEYEvNzgprIugHhb1s0GwIz9zqaHm60hhqnespmTVDsg1MfXqrNsNsInMY315wCR6lC7wZuc2ltSA5Z6k5oB44M2HpN/yTv+5R07sLTF+hZlqphKbzgQoOmLFwjaHsKmR+zBbBGzDBfBHjFtrCL/lXeMDT9EqG9J8IRRaTgHSgT6lnVjLglgfrPRAp/FtDHHaoOYt7HmlYmBepyUJInNtLxcFJPZhXRVdUVMConXH2WR+WWxQLL7eEtMmSme91w1yjtvFTdWx1VnVD32vBFQbiObKVIPZrVqkWqear5qsWTvQ+0ANxTro8MtM5vs+UTVvXq4ZQaIuf4D8rlfk+ncgXIn2EyBClgGQmxXHZb6w9un6terRGeoyb8DwMwUUwZvJcgq3yxIPHDNZeT3eMfnVKO9cx/UPcVmCjfoKRB/wWaEo6qTTYg30hBu0FNgwqAMNmJ8reABbJWvkt2HVByxnWymeCqm0iAOWHaIia/jQAcZKembdrhJhvS/UTDDa2wGyJrAD8V8LoqBuk1/znKN8jI13ot1G/RhIJtE7D5SuDqpvRMPD2X49wuYJvXfgSmy4lHuSL2TH+3fIzZ20xXqIujPbjYJlDnGZgYHxawaoYxpupgftki53dj4go90Gd/nbtg6IfA1PPeDKTt7xazz7WC5agabBfJMzJbQZ8GsQ9paNPi900767NviQKaFf0cUyVzVITYL5LYUnyGWEqTYm9lsgTFsFMhC1XU2+zI1NkrKNjYqKioqKiq6yV+AKtfl5s/uSwAAAABJRU5ErkJggg==>

[image2]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABEAAAAZCAYAAADXPsWXAAAAo0lEQVR4XmNgGAXYACMQfwDi/0j4LYoKCPjLgJAHsbGC+QwQBQ5o4sgAJI8XJDBAFFWjicPARiA2RhdEB8oMEEO2oUsAARcQP0MXxAVAhnxEFwSCX+gC+AAs4JBBMhDXoInhBdgMQecTBOiGXANiUSQ+UeA7A8IQUEAfQZIjGsxjgBjiB8T30OSIBgkMmF4iGSgyQAxIR5cgFZxGFxgFo4AaAADynCc6DrNJsgAAAABJRU5ErkJggg==>

[image3]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAbCAYAAAB1NA+iAAAA00lEQVR4Xu2SPw5BQRCHJ4JEKDQo3UAUwh0UonUkFS2FA6gluncBpRs4AoUIwW8yu8mYXesVyvclX7LzZ+e9zS5RgWUGdzmNUoEd+HK2YQPWYQuO4MnVknDD0yYVyQEDkoaFLSiSA/h83NBUuTLcqPis1gH+/Joj7JrcV/wAay6GJM1zleu5XC72JM18dZ4SXKt4BWsq/uBC4df4bYxVfFPrgF/n5ceU2aSnSrL5YAsKrvOVRlmSNExsAUxJatHf38IHydO1V8dy/g6vsO/2FBT8lzd/2juBKlrgXQAAAABJRU5ErkJggg==>

[image4]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACsAAAAaCAYAAAAue6XIAAABwklEQVR4Xu2WTytEURjGH/9ZEAvKAgufQWQjf8IHUCxkslD2SvIRlJIsfAcfwYaNZCU2oiyQBRaUQv6+r3MuM49773tGQ9H86qnxO89953TnzjFAkb/DIAuDZkkpy+/QJlmVrEjqaC2OackcywBeWeTDEtyACf93q+RCcv/R+EqL5JxlFvVI3lSl5IWlhX4cOnCTFzxPSB6q11WTa5Sc+LUoSWxJFlmmocOOWWbRB9fpJ98teSDHWJstQ/p6Dmewy9GdXyP/CPtZtTar6PoAS6YHrrhBnmmA612TV1dDjgnZ7IFkhyWjd0YH8TPHjMP1drNcrXcWIZudh90JGqQcwvX0iIro9c4i5D3GYHSaEDZIietNxrg44q5lOmF0om/hHS8QI3A9PtYy3luEbLYDdidoUFKnC/GeSbo+m1HYHdwgvRQd7BW8gM8TwiJks3r8WZ13tLTHUriEOy3S0Gv1X2YaIZvdR+5Jk8oV3MBtuGdYX+tDb6G9GZYePZP1N8Opj77mczpC5wyxLDSzkluWeVIC+84XDH2jcpZ5sC5ZZvlTDEuOWAaivzmeWf40C5IplgH8+kYjMiwM2iVVLIsU+Q+8AcPof4U5yGDQAAAAAElFTkSuQmCC>

[image5]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABEAAAAZCAYAAADXPsWXAAAAnElEQVR4XmNgGAXYwDIg/gHE/5HwTRQVDAyMSHIg/AVVGgEOMEAU1KCJw8AMIN6MLogOhBgQNqEDOyA+iy6IC2AzBGT4ZzQxvGARA8SQiUhi6IYSBCwMqK75B8TMCGniAcyQ90AsiyZHNOhngBgShi5BCnjNQEY4oAOQAR/QBUkB+gwQQ0rQJYgBMUB8hAERqCCXHEBWMApGwaAAAJrRKHZiVir5AAAAAElFTkSuQmCC>

[image6]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABMAAAAaCAYAAABVX2cEAAAAwklEQVR4XmNgGAWUgmgg/gnE/5HwGyT5X2hyt5HkcAI3Bojip2ji3ED8D4i50MQJApjt6GJkgZUMEM3NUD6IzYyQJg0wMiBc9w2IBVClSQe/GSCG2aNLkANOMkAMu4cuQSqYBcTlDNgjgiSQDcRLoGxYRIAMJhm4APFpJD5yRJAE1IH4BbogAyLlS6BLYANsQDyfAaIBxEYHxQwQuWfoEujgFhB/AOK3QPwRiL+iSjO8g4qD5EHsz0BciaJiFIyCoQoAWsU1i36JWuMAAAAASUVORK5CYII=>

[image7]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAsAAAAZCAYAAADnstS2AAAAfUlEQVR4XmNgGAX0BtVAHIsmNh+NDwa/ofR/ILaDspOh/AYoHwzmAjEPlA2StEeS+8uAprgWSk9ggChGBtOBWBpNDAxACp+hib1E44OBEANEsRySGDcQlyDx4YCFAdMJb9H4KOAPEF8AYn4gPgnEKqjSmMAAiCPQBUcBfQEAZzUVcMAb5poAAAAASUVORK5CYII=>

[image8]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABIAAAAZCAYAAAA8CX6UAAAAsUlEQVR4XmNgGAWkgi4g/gjE/6H4OxC/QxO7DldNBIBpwgZ+MuCWwwAghYfQBaGAhwEi34AmjgEiGCAKHdElkAA+F8PBNQbCiogyiBhFxKgBKziALogE3BggavDGHix8HNDEkcFtBogaMXQJZHCTgbCTQfJ/0QXRAUgRKJ3gAiD5J+iC6ECFAaKwGV0CCOQYIHLr0CWQQSAQn2RAxMQdID4OxWehYqBsYgrTMApGwaAHAJc1Nr9vaqJpAAAAAElFTkSuQmCC>

[image9]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAaCAYAAAC+aNwHAAAAsklEQVR4XmNgGAUw8ByI/yPh30B8F0m+Ek3+B5IcHPAzQCSPoEsgAZA8TtDOAFFgiy6BBN6iCyCDXwz4bUgG4hx0QWQA8x8u8BCIGdEFYYCLAaJ5B7oEEsBnOEMTA0SBOboEEniBLoAMfjLgtyEeiDPRBZEBIf/fRxdAByDNN9AFkQDIhXgByIB76IJQADJYCl0QHbQyYPfCJSB2RRfEBWoYIIb8gdKghMWJomIUjAJaAQB1qS0EQS26SAAAAABJRU5ErkJggg==>