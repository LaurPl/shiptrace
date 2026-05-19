// SessionDrawer is a slide-in overlay that renders one session's full
// event stream — header, tool calls, replan signals, ship events — in
// time order. Triggered by the `?session_id=X` query param so deep
// links survive a reload.
//
// Lives outside the 5-view constraint: not a route, not a top-level
// nav surface, just an overlay on top of whatever view spawned it.

import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  SessionDetailResponse,
  SessionNotFoundError,
  api,
} from "../api";

function fmtTimeOfDay(unix: number) {
  const d = new Date(unix * 1000);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  return `${hh}:${mm}:${ss}`;
}

function fmtDuration(seconds: number) {
  if (seconds <= 0) return "—";
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  return `${(seconds / 3600).toFixed(1)}h`;
}

// TimelineEntry is the row shape we render: union of the three event
// types, normalized to a common timestamp/kind/detail triple.
interface TimelineEntry {
  ts: number;
  category: "tool" | "replan" | "ship";
  primary: string;
  secondary?: string;
}

function buildTimeline(detail: SessionDetailResponse): TimelineEntry[] {
  const entries: TimelineEntry[] = [];
  for (const t of detail.tool_events) {
    entries.push({
      ts: t.ts,
      category: "tool",
      primary: t.tool,
      secondary:
        t.files_touched && t.files_touched.length > 0
          ? t.files_touched.join(", ")
          : undefined,
    });
  }
  for (const s of detail.replan_signals) {
    entries.push({
      ts: s.ts,
      category: "replan",
      primary: s.kind,
      secondary: `weight ${s.weight.toFixed(2)}`,
    });
  }
  for (const sh of detail.ship_events) {
    entries.push({
      ts: sh.ts,
      category: "ship",
      primary: sh.kind,
      secondary: sh.ref || sh.attribution_method,
    });
  }
  entries.sort((a, b) => a.ts - b.ts);
  return entries;
}

export default function SessionDrawer() {
  const [params, setParams] = useSearchParams();
  const sessionId = params.get("session_id");
  const [detail, setDetail] = useState<SessionDetailResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const close = useCallback(() => {
    const next = new URLSearchParams(params);
    next.delete("session_id");
    setParams(next, { replace: true });
  }, [params, setParams]);

  // Fetch on session_id change. Reset state when the drawer closes.
  useEffect(() => {
    if (!sessionId) {
      setDetail(null);
      setError(null);
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    setDetail(null);
    api
      .session(sessionId)
      .then((d) => {
        if (!cancelled) {
          setDetail(d);
          setLoading(false);
        }
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof SessionNotFoundError) {
          setError("session not found — it may have rolled out of storage.");
        } else {
          setError(err instanceof Error ? err.message : String(err));
        }
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId]);

  // Close on Esc.
  useEffect(() => {
    if (!sessionId) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [sessionId, close]);

  if (!sessionId) return null;

  const copyId = () => {
    void navigator.clipboard.writeText(sessionId).catch(() => {});
  };

  const timeline = detail ? buildTimeline(detail) : [];
  const durationSeconds =
    detail?.session.end_ts && detail.session.end_ts > 0
      ? detail.session.end_ts - detail.session.start_ts
      : 0;

  return (
    <div
      className="session-drawer-backdrop"
      onClick={close}
      role="presentation"
    >
      <aside
        className="session-drawer"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-label={`Session ${sessionId} detail`}
      >
        <header className="session-drawer-head">
          <div className="session-drawer-title">
            <h2>{detail?.session.label || sessionId}</h2>
            <button
              type="button"
              className="inline-toggle"
              onClick={copyId}
              title="Copy session id to clipboard"
            >
              copy id
            </button>
          </div>
          <button
            type="button"
            className="session-drawer-close"
            onClick={close}
            aria-label="Close drawer"
          >
            ×
          </button>
        </header>
        {loading && <div className="loading">loading session…</div>}
        {error && <div className="error">{error}</div>}
        {detail && (
          <>
            <dl className="session-drawer-meta">
              <dt>id</dt>
              <dd className="session-drawer-mono">{detail.session.id}</dd>
              {detail.session.project && (
                <>
                  <dt>project</dt>
                  <dd>{detail.session.project}</dd>
                </>
              )}
              <dt>provider</dt>
              <dd>{detail.session.provider}</dd>
              {detail.session.agent && (
                <>
                  <dt>agent</dt>
                  <dd>{detail.session.agent}</dd>
                </>
              )}
              {detail.session.skill && (
                <>
                  <dt>skill</dt>
                  <dd>{detail.session.skill}</dd>
                </>
              )}
              {detail.session.model && (
                <>
                  <dt>model</dt>
                  <dd>{detail.session.model}</dd>
                </>
              )}
              <dt>started</dt>
              <dd>{fmtTimeOfDay(detail.session.start_ts)}</dd>
              {detail.session.end_ts ? (
                <>
                  <dt>ended</dt>
                  <dd>
                    {fmtTimeOfDay(detail.session.end_ts)} ·{" "}
                    {fmtDuration(durationSeconds)}
                  </dd>
                </>
              ) : (
                <>
                  <dt>state</dt>
                  <dd>in progress</dd>
                </>
              )}
            </dl>
            <h3 className="session-drawer-h3">timeline</h3>
            {timeline.length === 0 ? (
              <div className="empty">no events recorded for this session.</div>
            ) : (
              <ol className="session-drawer-timeline">
                {timeline.map((entry, i) => (
                  <li
                    key={`${entry.ts}-${i}`}
                    className={`timeline-entry timeline-entry-${entry.category}`}
                  >
                    <span className="timeline-entry-ts">
                      {fmtTimeOfDay(entry.ts)}
                    </span>
                    <span className="timeline-entry-category">
                      {entry.category}
                    </span>
                    <span className="timeline-entry-primary">
                      {entry.primary}
                    </span>
                    {entry.secondary && (
                      <span className="timeline-entry-secondary">
                        {entry.secondary}
                      </span>
                    )}
                  </li>
                ))}
              </ol>
            )}
          </>
        )}
      </aside>
    </div>
  );
}
