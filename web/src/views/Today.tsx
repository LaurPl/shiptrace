import { useMemo } from "react";
import { api, TodaySession } from "../api";
import { LoaderBoundary, useLoader } from "../components/Loader";

function fmtDuration(s: number) {
  if (s <= 0) return "—";
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.round(s / 60)}m`;
  return `${(s / 3600).toFixed(1)}h`;
}

function sessionBar(s: TodaySession, windowStart: number, windowEnd: number) {
  const span = windowEnd - windowStart || 1;
  const left = Math.max(0, ((s.start_ts - windowStart) / span) * 100);
  const end = s.end_ts && s.end_ts > 0 ? s.end_ts : windowEnd;
  const width = Math.max(0.5, ((end - s.start_ts) / span) * 100);
  let cls = "bar";
  if (!s.end_ts || s.end_ts <= 0) cls += " in-progress";
  else if (s.ship_count > 0) cls += " shipped";
  else cls += " unshipped";
  const duration = end - s.start_ts;
  return (
    <div className="bar-track" key={s.id}>
      <div
        className={cls}
        style={{ left: `${left}%`, width: `${width}%` }}
        title={`${s.label || s.id} — ${fmtDuration(duration)}, ${s.ship_count} ship${s.ship_count === 1 ? "" : "s"}`}
      >
        {s.ship_count > 0 ? `${s.ship_count}✓` : ""}
      </div>
    </div>
  );
}

export default function Today() {
  const state = useLoader(() => api.today(), []);

  // All hooks live at the top of the component so React sees the same call
  // count on every render. LoaderBoundary's children render-prop only runs
  // when status is "ok" — calling hooks inside it would make hook count
  // depend on loader state and violate the Rules of Hooks.
  const rawSessions = state.data?.sessions;
  const sessions = useMemo(
    () =>
      rawSessions
        ? [...rawSessions].sort((a, b) => a.start_ts - b.start_ts)
        : [],
    [rawSessions],
  );

  const windowEnd = Math.floor(Date.now() / 1000);
  const windowStart = windowEnd - 24 * 3600;
  const shipped = sessions.filter((s) => s.ship_count > 0).length;
  const inProgress = sessions.filter(
    (s) => !s.end_ts || s.end_ts <= 0,
  ).length;
  const ratio =
    shipped > 0 ? (sessions.length / shipped).toFixed(2) : "—";

  return (
    <LoaderBoundary
      state={state}
      empty={(d) => d.sessions.length === 0}
    >
      {() => (
        <div className="card">
          <h2>Today — last 24 hours</h2>
          <div className="subtitle">
            {sessions.length} session{sessions.length === 1 ? "" : "s"} ·{" "}
            {shipped} shipped · {inProgress} in progress ·{" "}
            sessions-to-ship&nbsp;{ratio}
          </div>
          {sessions.map((s) => (
            <div className="timeline" key={s.id}>
              <div className="label">
                <div className="title">{s.label || s.id}</div>
                <div className="meta">
                  {s.project || "(no project)"} · {s.provider}
                  {s.replan_score > 0
                    ? ` · replan ${s.replan_score.toFixed(2)}`
                    : ""}
                </div>
              </div>
              {sessionBar(s, windowStart, windowEnd)}
            </div>
          ))}
        </div>
      )}
    </LoaderBoundary>
  );
}
