// LegendStrip renders a row of color chips with labels. Used to legend
// the four session states in Today and (with the `gradient` prop) the
// replan score scale in ReplanHeatmap.

export interface LegendItem {
  label: string;
  // CSS color value, var-friendly. e.g. "var(--accent)", "#888".
  color: string;
}

export function LegendStrip({ items }: { items: LegendItem[] }) {
  return (
    <div className="legend-strip" role="list">
      {items.map((it) => (
        <span className="legend-chip" key={it.label} role="listitem">
          <span
            className="legend-chip-color"
            style={{ background: it.color }}
            aria-hidden="true"
          />
          <span>{it.label}</span>
        </span>
      ))}
    </div>
  );
}

// GradientLegend renders a horizontal gradient bar with min/mid/max labels.
// Used by ReplanHeatmap to anchor the color scale numerically.
export function GradientLegend({
  from,
  to,
  min,
  mid,
  max,
}: {
  from: string;
  to: string;
  min: string;
  mid: string;
  max: string;
}) {
  return (
    <div className="gradient-legend">
      <div
        className="gradient-bar"
        style={{ background: `linear-gradient(to right, ${from}, ${to})` }}
        aria-hidden="true"
      />
      <div className="gradient-labels">
        <span>{min}</span>
        <span>{mid}</span>
        <span>{max}</span>
      </div>
    </div>
  );
}
