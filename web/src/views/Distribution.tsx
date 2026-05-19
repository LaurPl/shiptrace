import { api } from "../api";
import { LoaderBoundary, useLoader } from "../components/Loader";
import { BarComparisonChart, ChartPalette } from "../components/Chart";

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
        const noShips = rows.every((p) => p.ships === 0);
        return (
          <>
            <div className="card">
              <h2>sessions-to-ship by project</h2>
              <div className="subtitle">
                last {data.window_days} day{data.window_days === 1 ? "" : "s"} ·
                lower = tighter shipping discipline
              </div>
              {noShips ? (
                <div className="empty">
                  no ships in window — run <code>shiptrace ship</code> after a
                  commit to start measuring sessions-to-ship.
                </div>
              ) : (
                <BarComparisonChart
                  data={rows}
                  categoryKey="name"
                  series={[
                    {
                      key: "sessions",
                      label: "sessions",
                      color: ChartPalette.accentDim,
                    },
                    {
                      key: "ships",
                      label: "ships",
                      color: ChartPalette.accent,
                    },
                  ]}
                />
              )}
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
