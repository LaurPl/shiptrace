import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { AgentSkillRow, api } from "../api";
import { LoaderBoundary, useLoader } from "../components/Loader";

const ACCENT = "#6ab04c";
const ACCENT_DIM = "#4a7d35";

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
      <ResponsiveContainer width="100%" height={Math.max(220, rows.length * 36)}>
        <BarChart
          data={rows}
          layout="vertical"
          margin={{ top: 8, right: 32, left: 8, bottom: 8 }}
        >
          <CartesianGrid stroke="#2a2a2a" strokeDasharray="3 3" />
          <XAxis type="number" stroke="#888" fontSize={11} tickLine={false} />
          <YAxis
            type="category"
            dataKey="name"
            stroke="#888"
            width={150}
            tickLine={false}
            fontSize={11}
          />
          <Tooltip
            cursor={{ fill: "#1c1c1c" }}
            contentStyle={{
              background: "#141414",
              border: "1px solid #2a2a2a",
              fontFamily: "monospace",
              fontSize: 12,
            }}
          />
          <Bar dataKey="sessions" fill={ACCENT_DIM} name="sessions" />
          <Bar dataKey="ships" fill={ACCENT} name="ships" />
        </BarChart>
      </ResponsiveContainer>
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
