// Tiny typed wrapper around the JSON endpoints. Each function maps to one
// handler in internal/server; the response shapes mirror those Go structs
// 1:1.

export interface TodaySession {
  id: string;
  label?: string;
  project?: string;
  provider: string;
  agent?: string;
  skill?: string;
  model?: string;
  start_ts: number;
  end_ts?: number;
  prompt_count: number;
  tool_call_count: number;
  replan_score: number;
  ship_count: number;
}

export interface TodayResponse {
  as_of: string;
  sessions: TodaySession[];
}

export interface DistributionProject {
  name: string;
  sessions: number;
  ships: number;
  sessions_per_ship: number;
  mean_replan_score: number;
}

export interface DistributionResponse {
  window_days: number;
  projects: DistributionProject[];
}

export interface ReplanCell {
  project: string;
  hour: number;
  session_count: number;
  mean_score: number;
}

export interface ReplanHeatmapResponse {
  window_days: number;
  cells: ReplanCell[];
  projects: string[];
}

export interface AgentSkillRow {
  name: string;
  sessions: number;
  ships: number;
  sessions_per_ship: number;
}

export interface AgentSkillResponse {
  window_days: number;
  by_agent: AgentSkillRow[];
  by_skill: AgentSkillRow[];
}

export interface ProviderRow {
  name: string;
  sessions: number;
  ships: number;
  sessions_per_ship: number;
}

export interface ProviderMixResponse {
  window_days: number;
  providers: ProviderRow[];
}

export interface VersionResponse {
  name: string;
  startup: string;
  uptime_secs: number;
  api_version: number;
  schema_state: string;
}

export interface SessionInfo {
  id: string;
  label?: string;
  provider: string;
  provider_session_id?: string;
  project?: string;
  start_ts: number;
  end_ts?: number;
  model?: string;
  agent?: string;
  skill?: string;
}

export interface SessionToolEvent {
  ts: number;
  tool: string;
  tool_input_hash?: string;
  files_touched?: string[];
}

export interface SessionReplanSignal {
  ts: number;
  kind: string;
  weight: number;
  // metadata is forwarded as parsed JSON; treat as opaque per-kind shape.
  metadata?: Record<string, unknown>;
}

export interface SessionShipEvent {
  ts: number;
  kind: string;
  ref?: string;
  attribution_method?: string;
  metadata?: Record<string, unknown>;
}

export interface SessionDetailResponse {
  session: SessionInfo;
  tool_events: SessionToolEvent[];
  replan_signals: SessionReplanSignal[];
  ship_events: SessionShipEvent[];
}

async function getJSON<T>(path: string): Promise<T> {
  const r = await fetch(path);
  if (!r.ok) {
    throw new Error(`${path} → HTTP ${r.status}`);
  }
  return (await r.json()) as T;
}

// SessionNotFoundError is thrown by api.session when the backend returns
// 404 for an unknown session id. Components catch this distinct from
// network/transport errors so they can render "session not found" vs
// "failed to load."
export class SessionNotFoundError extends Error {
  constructor(public id: string) {
    super(`session ${id} not found`);
    this.name = "SessionNotFoundError";
  }
}

export const api = {
  today: () => getJSON<TodayResponse>("/api/today"),
  distribution: (days = 30) =>
    getJSON<DistributionResponse>(`/api/distribution?days=${days}`),
  replanHeatmap: (days = 30) =>
    getJSON<ReplanHeatmapResponse>(`/api/replan-heatmap?days=${days}`),
  agentSkill: (days = 30) =>
    getJSON<AgentSkillResponse>(`/api/agent-skill-roi?days=${days}`),
  providerMix: (days = 30) =>
    getJSON<ProviderMixResponse>(`/api/provider-mix?days=${days}`),
  session: async (id: string): Promise<SessionDetailResponse> => {
    const r = await fetch(`/api/session/${encodeURIComponent(id)}`);
    if (r.status === 404) {
      throw new SessionNotFoundError(id);
    }
    if (!r.ok) {
      throw new Error(`/api/session/${id} → HTTP ${r.status}`);
    }
    return (await r.json()) as SessionDetailResponse;
  },
  version: () => getJSON<VersionResponse>("/api/version"),
};
