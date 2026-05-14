import { api, ReplanCell } from "../api";
import { LoaderBoundary, useLoader } from "../components/Loader";

const HOURS = Array.from({ length: 24 }, (_, i) => i);

// scoreColor returns a CSS background value for a (mean_score, session_count)
// cell. Scores tint warm (red) past 0.5, cool (blue) below; opacity scales
// with session_count so a single high-score session doesn't dominate.
function scoreColor(score: number, count: number): string {
  if (count === 0) return "transparent";
  const opacity = Math.min(1, 0.25 + count * 0.15);
  if (score >= 0.5) {
    const warmth = Math.min(1, (score - 0.5) * 2);
    const r = 90 + Math.round(warmth * 165);
    const g = 50;
    const b = 50;
    return `rgba(${r}, ${g}, ${b}, ${opacity})`;
  }
  const cool = 1 - score * 2;
  const r = 20;
  const g = 80 + Math.round(cool * 60);
  const b = 130 + Math.round(cool * 30);
  return `rgba(${r}, ${g}, ${b}, ${opacity})`;
}

function cellFor(
  cells: ReplanCell[],
  project: string,
  hour: number,
): ReplanCell | undefined {
  return cells.find((c) => c.project === project && c.hour === hour);
}

export default function ReplanHeatmap() {
  const state = useLoader(() => api.replanHeatmap(30), []);
  return (
    <LoaderBoundary state={state} empty={(d) => d.cells.length === 0}>
      {(data) => {
        const projects = data.projects.length > 0 ? data.projects : ["(unassigned)"];
        return (
          <div className="card">
            <h2>replan heatmap — project × hour</h2>
            <div className="subtitle">
              last {data.window_days} day{data.window_days === 1 ? "" : "s"} ·
              warm = high replan score · opacity = session volume
            </div>
            <div className="heatmap">
              <div />
              {HOURS.map((h) => (
                <div key={h} className="hour-header">
                  {h % 3 === 0 ? h : ""}
                </div>
              ))}
              {projects.map((proj) => (
                <FragmentRow key={proj} project={proj} cells={data.cells} />
              ))}
            </div>
          </div>
        );
      }}
    </LoaderBoundary>
  );
}

function FragmentRow({
  project,
  cells,
}: {
  project: string;
  cells: ReplanCell[];
}) {
  return (
    <>
      <div className="project-cell" title={project}>
        {project}
      </div>
      {HOURS.map((h) => {
        const c = cellFor(cells, project, h);
        const score = c?.mean_score ?? 0;
        const count = c?.session_count ?? 0;
        return (
          <div
            key={h}
            className="cell"
            style={{ background: scoreColor(score, count) }}
            title={
              c
                ? `${project} @ ${h}:00 — ${count} session${count === 1 ? "" : "s"}, replan ${score.toFixed(2)}`
                : `${project} @ ${h}:00 — no sessions`
            }
          />
        );
      })}
    </>
  );
}
