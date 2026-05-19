import { api } from "../api";
import { LoaderBoundary, useLoader } from "../components/Loader";
import { BarComparisonChart, ChartPalette } from "../components/Chart";
import { HelpHint } from "../components/HelpHint";

const SHIP_DEFINITION =
  "a ship is a recorded shipping signal (manual `shiptrace ship`, git commit, deploy hook). sessions-to-ship is the ratio of work sessions to shipped artifacts in the window.";

export default function ProviderMix() {
  const state = useLoader(() => api.providerMix(30), []);
  return (
    <LoaderBoundary state={state} empty={(d) => d.providers.length === 0}>
      {(data) => (
        <>
          <div className="card">
            <h2>
              provider mix <HelpHint text={SHIP_DEFINITION} />
            </h2>
            <div className="subtitle">
              last {data.window_days} day{data.window_days === 1 ? "" : "s"}
            </div>
            {data.providers.length === 1 ? (
              <ProviderSummary
                name={data.providers[0].name}
                sessions={data.providers[0].sessions}
                ships={data.providers[0].ships}
              />
            ) : (
              <BarComparisonChart
                data={data.providers}
                categoryKey="name"
                layout="horizontal"
                height={260}
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

// ProviderSummary replaces the bar chart when there's only one provider.
// A single-row "mix" is just a count — the chart contributes nothing.
function ProviderSummary({
  name,
  sessions,
  ships,
}: {
  name: string;
  sessions: number;
  ships: number;
}) {
  return (
    <div className="provider-summary">
      <div className="provider-summary-row">
        <span className="provider-summary-label">{name}</span>
        <span className="provider-summary-stat">
          <span className="provider-summary-num">{sessions}</span>
          <span className="provider-summary-unit">
            session{sessions === 1 ? "" : "s"}
          </span>
        </span>
        <span className="provider-summary-stat">
          <span className="provider-summary-num">{ships}</span>
          <span className="provider-summary-unit">
            ship{ships === 1 ? "" : "s"}
          </span>
        </span>
      </div>
      <div className="provider-summary-note">
        only one provider in this window — chart is suppressed until at least
        two are present.
      </div>
    </div>
  );
}
