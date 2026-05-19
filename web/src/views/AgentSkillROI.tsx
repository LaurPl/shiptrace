import { AgentSkillRow, api } from "../api";
import { LoaderBoundary, useLoader } from "../components/Loader";
import { BarComparisonChart, ChartPalette } from "../components/Chart";

// isUnattributed returns true when the only row in the slice is the
// `(none)` bucket — meaning no session has yet been tagged with an
// agent or skill. Rendering a one-row chart in that case is misleading;
// we show an empty state with a pointer to the docs instead.
function isUnattributed(rows: AgentSkillRow[]): boolean {
  return rows.length === 0 || (rows.length === 1 && rows[0].name === "(none)");
}

function Section({
  title,
  rows,
}: {
  title: string;
  rows: AgentSkillRow[];
}) {
  if (isUnattributed(rows)) {
    return (
      <div className="card">
        <h2>{title}</h2>
        <div className="empty">
          no agent/skill attribution recorded yet — sessions appear here once
          the recorder captures an agent or skill tag.
        </div>
      </div>
    );
  }
  return (
    <div className="card">
      <h2>{title}</h2>
      <BarComparisonChart
        data={rows}
        categoryKey="name"
        categoryWidth={150}
        height={Math.max(220, rows.length * 36)}
        series={[
          {
            key: "sessions",
            label: "sessions",
            color: ChartPalette.accentDim,
          },
          { key: "ships", label: "ships", color: ChartPalette.accent },
        ]}
      />
    </div>
  );
}

export default function AgentSkillROI() {
  const state = useLoader(() => api.agentSkill(30), []);
  return (
    <LoaderBoundary
      state={state}
      empty={(d) => d.by_agent.length === 0 && d.by_skill.length === 0}
    >
      {(data) => (
        <>
          <Section title="sessions-to-ship by agent" rows={data.by_agent} />
          <Section title="sessions-to-ship by skill" rows={data.by_skill} />
        </>
      )}
    </LoaderBoundary>
  );
}
