CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  label TEXT,
  provider TEXT NOT NULL,
  provider_session_id TEXT,
  project TEXT,
  start_ts INTEGER NOT NULL,
  end_ts INTEGER,
  model TEXT,
  agent TEXT,
  skill TEXT,
  prompt_count INTEGER DEFAULT 0,
  tool_call_count INTEGER DEFAULT 0,
  tokens_in INTEGER DEFAULT 0,
  tokens_out INTEGER DEFAULT 0,
  replan_score REAL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tool_events (
  session_id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  tool TEXT NOT NULL,
  tool_input_hash TEXT,
  files_touched TEXT,
  FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE TABLE IF NOT EXISTS ship_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT,
  ts INTEGER NOT NULL,
  kind TEXT NOT NULL,
  ref TEXT,
  magnitude TEXT,
  metadata TEXT,
  attribution_method TEXT
);

CREATE INDEX IF NOT EXISTS idx_tool_events_session ON tool_events(session_id, ts);
CREATE INDEX IF NOT EXISTS idx_ship_events_session ON ship_events(session_id, ts);
CREATE INDEX IF NOT EXISTS idx_sessions_project_time ON sessions(project, start_ts);
CREATE INDEX IF NOT EXISTS idx_sessions_provider_lookup ON sessions(provider, provider_session_id);
