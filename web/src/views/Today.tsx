import { useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { api, TodaySession } from "../api";
import { LoaderBoundary, useLoader } from "../components/Loader";
import { LegendStrip } from "../components/LegendStrip";

const HOURS_IN_WINDOW = 24;
const TICK_HOURS = 4;

// isEmptySession is the same predicate the row meta uses to tag a session
// "empty": no work recorded between start and stop. We use it to filter
// rows and to colour the pip neutrally instead of red (red is for real
// unshipped work).
function isEmptySession(s: TodaySession): boolean {
  const inProgress = !s.end_ts || s.end_ts <= 0;
  if (inProgress) return false;
  const dur = s.end_ts! - s.start_ts;
  return dur === 0 && s.prompt_count === 0 && s.tool_call_count === 0;
}

function fmtDuration(s: number) {
  if (s <= 0) return "—";
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.round(s / 60)}m`;
  return `${(s / 3600).toFixed(1)}h`;
}

function fmtTimeOfDay(unixSeconds: number) {
  const d = new Date(unixSeconds * 1000);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  return `${hh}:${mm}`;
}

// rowTitle gives the human-readable label on the left. Falls back through
// project basename → explicit label → start time so the row always has a
// recognizable handle, never a raw session ID.
function rowTitle(s: TodaySession) {
  if (s.project) {
    const parts = s.project.split("/").filter(Boolean);
    if (parts.length > 0) return parts[parts.length - 1];
  }
  if (s.label) return s.label;
  return `session · ${fmtTimeOfDay(s.start_ts)}`;
}

function rowMeta(s: TodaySession) {
  const dur =
    s.end_ts && s.end_ts > s.start_ts ? s.end_ts - s.start_ts : 0;
  const inProgress = !s.end_ts || s.end_ts <= 0;
  const bits: string[] = [s.provider];
  if (s.prompt_count > 0)
    bits.push(`${s.prompt_count} prompt${s.prompt_count === 1 ? "" : "s"}`);
  if (s.tool_call_count > 0)
    bits.push(`${s.tool_call_count} tool${s.tool_call_count === 1 ? "" : "s"}`);
  if (dur > 0) bits.push(fmtDuration(dur));
  if (s.replan_score > 0) bits.push(`replan ${s.replan_score.toFixed(2)}`);
  if (
    !inProgress &&
    dur === 0 &&
    s.prompt_count === 0 &&
    s.tool_call_count === 0
  ) {
    bits.push("empty");
  }
  return bits.join(" · ");
}

function barTooltip(s: TodaySession) {
  const dur =
    s.end_ts && s.end_ts > s.start_ts ? s.end_ts - s.start_ts : 0;
  const inProgress = !s.end_ts || s.end_ts <= 0;
  const parts = [
    s.id,
    `started ${fmtTimeOfDay(s.start_ts)}`,
    inProgress ? "in progress" : dur > 0 ? fmtDuration(dur) : "instant",
    `${s.ship_count} ship${s.ship_count === 1 ? "" : "s"}`,
  ];
  return parts.join(" · ");
}

function sessionBar(
  s: TodaySession,
  windowStart: number,
  windowEnd: number,
) {
  const span = windowEnd - windowStart || 1;
  const left = Math.max(
    0,
    Math.min(100, ((s.start_ts - windowStart) / span) * 100),
  );
  const inProgress = !s.end_ts || s.end_ts <= 0;
  const end = inProgress ? windowEnd : s.end_ts!;
  const dur = end - s.start_ts;

  // A bounded session with start == end has no horizontal extent. Render
  // it as a pip (dot) so the viewer can see something fired without it
  // disappearing into a half-pixel sliver.
  const isPip = !inProgress && dur <= 0;

  let stateCls = "";
  if (inProgress) stateCls = "in-progress";
  else if (s.ship_count > 0) stateCls = "shipped";
  else if (isEmptySession(s)) stateCls = "empty";
  else stateCls = "unshipped";

  if (isPip) {
    return (
      <div className="bar-track">
        <div
          className={`bar-pip ${stateCls}`}
          style={{ left: `${left}%` }}
          title={barTooltip(s)}
        />
      </div>
    );
  }

  const width = Math.max(0.5, (dur / span) * 100);
  return (
    <div className="bar-track">
      <div
        className={`bar ${stateCls}`}
        style={{ left: `${left}%`, width: `${width}%` }}
        title={barTooltip(s)}
      >
        {s.ship_count > 0 ? `${s.ship_count}✓` : ""}
      </div>
    </div>
  );
}

function buildTicks(windowStart: number, windowEnd: number) {
  // Anchor ticks to local-time TICK_HOURS boundaries (00, 04, 08, …) so
  // labels read at a glance instead of drifting with windowStart's
  // sub-hour offset.
  const start = new Date(windowStart * 1000);
  start.setMinutes(0, 0, 0);
  const h = start.getHours();
  const skip = (TICK_HOURS - (h % TICK_HOURS)) % TICK_HOURS;
  start.setHours(h + skip);
  if (Math.floor(start.getTime() / 1000) < windowStart) {
    start.setHours(start.getHours() + TICK_HOURS);
  }
  const out: { atUnix: number; label: string }[] = [];
  for (
    let t = Math.floor(start.getTime() / 1000);
    t < windowEnd;
    t += TICK_HOURS * 3600
  ) {
    out.push({ atUnix: t, label: fmtTimeOfDay(t) });
  }
  return out;
}

function TimeAxis({
  windowStart,
  windowEnd,
}: {
  windowStart: number;
  windowEnd: number;
}) {
  const span = windowEnd - windowStart || 1;
  const ticks = buildTicks(windowStart, windowEnd);
  return (
    <div className="time-axis-row">
      <div className="time-axis-spacer" />
      <div className="time-axis">
        {ticks.map((tk) => {
          const leftPct = ((tk.atUnix - windowStart) / span) * 100;
          return (
            <div
              key={tk.atUnix}
              className="time-tick"
              style={{ left: `${leftPct}%` }}
            >
              {tk.label}
            </div>
          );
        })}
        <div className="now-marker" title="now" />
      </div>
    </div>
  );
}

const LEGEND_ITEMS = [
  { label: "shipped", color: "var(--accent)" },
  { label: "unshipped", color: "var(--error)" },
  { label: "in progress", color: "var(--info)" },
  { label: "empty (no work recorded)", color: "var(--fg-dim)" },
];

export default function Today() {
  const state = useLoader(() => api.today(), []);
  const [params, setParams] = useSearchParams();
  // Default: empty sessions are hidden. The user opts in to see them via
  // `?empty=1` so install-boundary phantoms don't drown the real signal.
  const showEmpty = params.get("empty") === "1";

  // All hooks live at the top of the component so React sees the same call
  // count on every render. LoaderBoundary's children render-prop only runs
  // when status is "ok" — calling hooks inside it would make hook count
  // depend on loader state and violate the Rules of Hooks.
  const rawSessions = state.data?.sessions;
  const allSessions = useMemo(
    () =>
      rawSessions
        ? [...rawSessions].sort((a, b) => a.start_ts - b.start_ts)
        : [],
    [rawSessions],
  );

  const emptyCount = useMemo(
    () => allSessions.filter(isEmptySession).length,
    [allSessions],
  );
  const sessions = useMemo(
    () => (showEmpty ? allSessions : allSessions.filter((s) => !isEmptySession(s))),
    [allSessions, showEmpty],
  );

  const toggleEmpty = () => {
    const next = new URLSearchParams(params);
    if (showEmpty) next.delete("empty");
    else next.set("empty", "1");
    setParams(next, { replace: true });
  };

  const openSession = (id: string) => {
    const next = new URLSearchParams(params);
    next.set("session_id", id);
    setParams(next);
  };

  const windowEnd = Math.floor(Date.now() / 1000);
  const windowStart = windowEnd - HOURS_IN_WINDOW * 3600;
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
            {emptyCount > 0 && (
              <>
                {" · "}
                <button
                  type="button"
                  className="inline-toggle"
                  onClick={toggleEmpty}
                  title={
                    showEmpty
                      ? "Hide sessions that recorded no work"
                      : "Show sessions that recorded no work"
                  }
                >
                  {showEmpty
                    ? `hide ${emptyCount} empty`
                    : `show ${emptyCount} empty`}
                </button>
              </>
            )}
          </div>
          <LegendStrip items={LEGEND_ITEMS} />
          {sessions.map((s) => (
            <div
              className="timeline timeline-clickable"
              key={s.id}
              role="button"
              tabIndex={0}
              onClick={() => openSession(s.id)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  openSession(s.id);
                }
              }}
              title="Open session detail"
            >
              <div className="label">
                <div className="title">{rowTitle(s)}</div>
                <div className="meta" title={s.id}>
                  {rowMeta(s)}
                </div>
              </div>
              {sessionBar(s, windowStart, windowEnd)}
            </div>
          ))}
          <TimeAxis windowStart={windowStart} windowEnd={windowEnd} />
        </div>
      )}
    </LoaderBoundary>
  );
}
