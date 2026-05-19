import { useEffect, useState } from "react";
import { NavLink, Navigate, Route, Routes } from "react-router-dom";
import { api, VersionResponse } from "./api";
import Today from "./views/Today";
import Distribution from "./views/Distribution";
import ReplanHeatmap from "./views/ReplanHeatmap";
import AgentSkillROI from "./views/AgentSkillROI";
import ProviderMix from "./views/ProviderMix";
import GlobalBanner from "./components/GlobalBanner";

function Nav() {
  const tabs = [
    { to: "/today", label: "today" },
    { to: "/distribution", label: "distribution" },
    { to: "/replan", label: "replan" },
    { to: "/agent-skill", label: "agent / skill" },
    { to: "/provider", label: "provider mix" },
  ];
  return (
    <nav className="tabs">
      {tabs.map((t) => (
        <NavLink
          key={t.to}
          to={t.to}
          className={({ isActive }) => (isActive ? "active" : "")}
        >
          {t.label}
        </NavLink>
      ))}
    </nav>
  );
}

function StatusFooter() {
  const [v, setV] = useState<VersionResponse | null>(null);
  useEffect(() => {
    api.version().then(setV).catch(() => setV(null));
  }, []);
  return (
    <footer className="status">
      <span>{v ? `${v.name} · api v${v.api_version}` : "shiptrace"}</span>
      <span>
        {v
          ? `up since ${new Date(v.startup).toLocaleString()}`
          : "—"}
      </span>
    </footer>
  );
}

export default function App() {
  return (
    <div className="shell">
      <header className="top">
        <h1>
          <span className="glyph">▲</span> shiptrace
        </h1>
        <span className="meta">
          local dashboard · localhost:7777
        </span>
      </header>
      <GlobalBanner />
      <Nav />
      <Routes>
        <Route path="/" element={<Navigate to="/today" replace />} />
        <Route path="/today" element={<Today />} />
        <Route path="/distribution" element={<Distribution />} />
        <Route path="/replan" element={<ReplanHeatmap />} />
        <Route path="/agent-skill" element={<AgentSkillROI />} />
        <Route path="/provider" element={<ProviderMix />} />
      </Routes>
      <StatusFooter />
    </div>
  );
}
