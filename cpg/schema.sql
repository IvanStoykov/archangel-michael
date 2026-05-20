-- Code Property Graph (CPG) Database Schema for Go and Dart Analysis

CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    name TEXT NOT NULL,
    filepath TEXT NOT NULL,
    start_line INTEGER,
    end_line INTEGER,
    metadata TEXT DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS edges (
    source TEXT NOT NULL,
    target TEXT NOT NULL,
    type TEXT NOT NULL,
    metadata TEXT DEFAULT '{}',
    PRIMARY KEY (source, target, type),
    FOREIGN KEY (source) REFERENCES nodes(id) ON DELETE CASCADE,
    FOREIGN KEY (target) REFERENCES nodes(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_nodes_filepath ON nodes(filepath);
CREATE INDEX IF NOT EXISTS idx_nodes_type_name ON nodes(type, name);
CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source);
CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target);
