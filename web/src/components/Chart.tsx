// BarComparisonChart wraps recharts' BarChart with the styling and
// rendering defaults the dashboard needs. Three behaviours that
// callers consistently want and that recharts doesn't give by default:
//
//   1. minPointSize={2} on every Bar — recharts otherwise renders
//      single-row or low-value bars as a sub-pixel sliver, which is
//      indistinguishable from "no data" against a dark background.
//   2. An explicit value-axis domain padded above the data max, so a
//      single-row chart with sessions=23 against a default-scaled
//      0–24 axis still shows a tall bar (recharts auto-scaling
//      otherwise leaves margins that crush the bar to ~10% of height).
//   3. Consistent tooltip / cursor / grid styling pulled from a
//      single place rather than repeated inline in every view.
//
// The wrapper is intentionally narrow: one chart shape (bar
// comparison), two layouts (vertical = bars run horizontally,
// horizontal = bars rise vertically). Adding more is fine when a
// caller needs it.

import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

export interface ChartSeries {
  // Field on each data row that this series reads.
  key: string;
  // Label shown in tooltip / legend.
  label: string;
  // Bar fill, hex string. CSS vars don't resolve inside SVG fill.
  color: string;
}

export interface BarComparisonChartProps<T> {
  data: T[];
  // Field on each row that holds the category name (string).
  categoryKey: keyof T & string;
  series: ChartSeries[];
  // "vertical" = categories on Y axis, bars run left-to-right (default).
  // "horizontal" = categories on X axis, bars rise vertically.
  layout?: "vertical" | "horizontal";
  // Width reserved for the category axis labels (vertical layout).
  categoryWidth?: number;
  // Optional height override; otherwise computed from row count.
  height?: number;
}

// Colour tokens kept in one place so a future palette change doesn't
// require touching every chart. Match the CSS variables in styles.css.
const GRID = "#2a2a2a";
const AXIS = "#888";
const TOOLTIP_BG = "#141414";

export function BarComparisonChart<T>({
  data,
  categoryKey,
  series,
  layout = "vertical",
  categoryWidth = 130,
  height,
}: BarComparisonChartProps<T>) {
  // Pad the value domain ~10% above the observed max so a single-row
  // chart with sessions=23 still occupies most of the canvas. The
  // floor of 1 keeps an all-zero dataset from collapsing to NaN.
  const maxValue = Math.max(
    1,
    ...data.flatMap((row) =>
      series.map((s) => {
        const v = (row as Record<string, unknown>)[s.key];
        return typeof v === "number" ? v : 0;
      }),
    ),
  );
  const domain: [number, number] = [0, Math.ceil(maxValue * 1.1)];

  const computedHeight = height ?? Math.max(220, data.length * 36);

  const categoryAxis =
    layout === "vertical" ? (
      <YAxis
        type="category"
        dataKey={categoryKey}
        stroke={AXIS}
        width={categoryWidth}
        tickLine={false}
        fontSize={11}
      />
    ) : (
      <XAxis
        type="category"
        dataKey={categoryKey}
        stroke={AXIS}
        tickLine={false}
        fontSize={11}
      />
    );

  const valueAxis =
    layout === "vertical" ? (
      <XAxis
        type="number"
        stroke={AXIS}
        tickLine={false}
        fontSize={11}
        domain={domain}
        allowDecimals={false}
      />
    ) : (
      <YAxis
        type="number"
        stroke={AXIS}
        tickLine={false}
        fontSize={11}
        domain={domain}
        allowDecimals={false}
      />
    );

  return (
    <ResponsiveContainer width="100%" height={computedHeight}>
      <BarChart
        data={data}
        layout={layout}
        margin={{ top: 8, right: 32, left: 8, bottom: 8 }}
      >
        <CartesianGrid stroke={GRID} strokeDasharray="3 3" />
        {valueAxis}
        {categoryAxis}
        <Tooltip
          cursor={{ fill: "#1c1c1c" }}
          contentStyle={{
            background: TOOLTIP_BG,
            border: `1px solid ${GRID}`,
            fontFamily: "monospace",
            fontSize: 12,
          }}
        />
        {series.map((s) => (
          <Bar
            key={s.key}
            dataKey={s.key}
            fill={s.color}
            name={s.label}
            minPointSize={2}
          />
        ))}
      </BarChart>
    </ResponsiveContainer>
  );
}

// Palette tokens callers can reference without duplicating the hex.
// Kept in sync with --accent / --accent-dim in styles.css.
export const ChartPalette = {
  accent: "#6ab04c",
  accentDim: "#4a7d35",
};
