import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { api } from "../api";
import { LoaderBoundary, useLoader } from "../components/Loader";

const ACCENT = "#6ab04c";
const ACCENT_DIM = "#4a7d35";

export default function Distribution() {
  const state = useLoader(() => api.distribution(30), []);
  return (
    <LoaderBoundary
      state={state}
      empty={(d) => d.projects.length === 0}
    >
      {(data) => {
        // Hide projects with literally zero of both — they're noise.
        const rows = data.projects.filter(
          (p) => p.sessions > 0 || p.ships > 0,
        );
        return (
          <>
            <div className="card">
              <h2>sessions-to-ship by project</h2>
              <div className="subtitle">
                last {data.window_days} day{data.window_days === 1 ? "" : "s"} ·
                lower = tighter shipping discipline
              </div>
              <ResponsiveContainer width="100%" height={Math.max(220, rows.length * 36)}>
                <BarChart
                  data={rows}
                  layout="vertical"
                  margin={{ top: 8, right: 32, left: 8, bottom: 8 }}
                >
                  <CartesianGrid stroke="#2a2a2a" strokeDasharray="3 3" />
                  <XAxis
                    type="number"
                    stroke="#888"
                    tickLine={false}
                    fontSize={11}
                  />
                  <YAxis
                    type="category"
                    dataKey="name"
                    stroke="#888"
                    width={130}
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
                  <Bar
                    dataKey="sessions"
                    fill={ACCENT_DIM}
                    name="sessions"
                  />
                  <Bar dataKey="ships" fill={ACCENT} name="ships" />
                </BarChart>
              </ResponsiveContainer>
            </div>

            <div className="card">
              <h2>raw numbers</h2>
              <table className="data">
                <thead>
                  <tr>
                    <th>project</th>
                    <th className="num">sessions</th>
                    <th className="num">ships</th>
                    <th className="num">sess/ship</th>
                    <th className="num">avg replan</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((p) => (
                    <tr key={p.name}>
                      <td>{p.name}</td>
                      <td className="num">{p.sessions}</td>
                      <td className="num">{p.ships}</td>
                      <td className="num">
                        {p.sessions_per_ship > 0
                          ? p.sessions_per_ship.toFixed(2)
                          : "—"}
                      </td>
                      <td className="num">
                        {p.mean_replan_score.toFixed(2)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        );
      }}
    </LoaderBoundary>
  );
}
