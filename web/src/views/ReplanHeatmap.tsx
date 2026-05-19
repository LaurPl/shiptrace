import { api, ReplanCell } from "../api";
import { LoaderBoundary, useLoader } from "../components/Loader";
import { GradientLegend } from "../components/LegendStrip";

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

// noVariance returns true when every populated cell carries the same
// score — the colour gradient then conveys nothing. We surface a banner
// instead of letting the user squint at a uniformly tinted grid.
function noVariance(cells: ReplanCell[]): boolean {
  const populated = cells.filter((c) => c.session_count > 0);
  if (populated.length === 0) return true;
  const first = populated[0].mean_score;
  return populated.every((c) => c.mean_score === first);
}

export default function ReplanHeatmap() {
  const state = useLoader(() => api.replanHeatmap(30), []);
  return (
    <LoaderBoundary state={state} empty={(d) => d.cells.length === 0}>
      {(data) => {
        const projects = data.projects.length > 0 ? data.projects : ["(unassigned)"];
        const flat = noVariance(data.cells);
        return (
          <div className="card">
            <h2>replan heatmap — project × hour</h2>
            <div className="subtitle">
              last {data.window_days} day{data.window_days === 1 ? "" : "s"} ·
              warm = high replan score · opacity = session volume
            </div>
            {flat && (
              <div className="variance-banner" role="status">
                Not enough variance to render a gradient — every populated cell
                has the same replan score. The grid below still shows when
                sessions ran; the colour is informational only.
              </div>
            )}
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
            <GradientLegend
              from="rgba(20, 130, 160, 0.85)"
              to="rgba(255, 50, 50, 0.85)"
              min="0.0"
              mid="0.5"
              max="1.0"
            />
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
