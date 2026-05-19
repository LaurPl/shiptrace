import { useEffect, useState } from "react";
import { api } from "../api";

// GlobalBanner renders a one-line nudge above the nav when the store
// has no ship events yet. Without ships, every "sessions-to-ship" ratio
// in the dashboard is "—" — the banner explains why before the user
// concludes their data is broken.
export default function GlobalBanner() {
  const [show, setShow] = useState(false);
  useEffect(() => {
    api
      .health()
      .then((h) => setShow(!h.has_ships))
      .catch(() => setShow(false));
  }, []);
  if (!show) return null;
  return (
    <div className="global-banner" role="status">
      No ships recorded yet. Run <code>shiptrace ship</code> after your next
      commit. Until then, sessions-to-ship ratios show <code>—</code>.
    </div>
  );
}
