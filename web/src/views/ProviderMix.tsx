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

export default function ProviderMix() {
  const state = useLoader(() => api.providerMix(30), []);
  return (
    <LoaderBoundary state={state} empty={(d) => d.providers.length === 0}>
      {(data) => (
        <>
          <div className="card">
            <h2>provider mix</h2>
            <div className="subtitle">
              last {data.window_days} day{data.window_days === 1 ? "" : "s"}
            </div>
            <ResponsiveContainer width="100%" height={260}>
              <BarChart
                data={data.providers}
                margin={{ top: 8, right: 32, left: 8, bottom: 8 }}
              >
                <CartesianGrid stroke="#2a2a2a" strokeDasharray="3 3" />
                <XAxis dataKey="name" stroke="#888" fontSize={11} tickLine={false} />
                <YAxis stroke="#888" fontSize={11} tickLine={false} />
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

          <div className="card">
            <h2>raw numbers</h2>
            <table className="data">
              <thead>
                <tr>
                  <th>provider</th>
                  <th className="num">sessions</th>
                  <th className="num">ships</th>
                  <th className="num">sess/ship</th>
                </tr>
              </thead>
              <tbody>
                {data.providers.map((p) => (
                  <tr key={p.name}>
                    <td>{p.name}</td>
                    <td className="num">{p.sessions}</td>
                    <td className="num">{p.ships}</td>
                    <td className="num">
                      {p.sessions_per_ship > 0
                        ? p.sessions_per_ship.toFixed(2)
                        : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </LoaderBoundary>
  );
}
