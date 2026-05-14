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

function Section({
  title,
  rows,
}: {
  title: string;
  rows: AgentSkillRow[];
}) {
  if (rows.length === 0) {
    return (
      <div className="card">
        <h2>{title}</h2>
        <div className="empty">no data yet</div>
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
