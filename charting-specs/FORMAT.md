# Diagram Response Format

Every diagram endpoint (typically a workflow's output script) returns a single-element array wrapping the visualization payload:

```javascript
return [{ json: { type, label, description, lastupdate, data, ... } }];
```

The dashboard reads `type` to select a renderer and passes `data` (plus `options` when present) to Chart.js or a custom renderer.

---

## Common Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `type` | string | yes | Renderer key (see types below) |
| `label` | string | yes | Card title. May contain inline HTML (e.g. help-icon links) |
| `description` | string | yes | Tooltip / subtitle text. HTML allowed |
| `lastupdate` | number | yes | Unix timestamp in **milliseconds** (`new Date(...).getTime()`) |
| `data` | object | yes | Visualization payload (structure varies by type) |
| `options` | object | no | Chart.js options override (used by doughnut, bar, line, scatter) |
| `helpUrl` | string | no | Link to external documentation |
| `summary` | object | no | Extra metadata for display outside the chart (used by doughnut) |
| `warning` | string | no | Warning banner text (used by line) |

---

## Visualization Types

### `stats`

Key metric cards showing numeric values with optional goals.

```javascript
data: {
  metrics: [
    {
      label: "Metric Name",       // display label
      value: 42,                  // number or string
      goal: 10,                   // optional — number or "auto"
      informational: false,       // optional — if true, no goal coloring
      explain: "<em>vs prior</em>" // optional — comparison HTML
    }
  ]
}
```

### `doughnut`

Pie/doughnut chart (Chart.js).

```javascript
data: {
  labels: ["Merged", "Closed"],
  datasets: [{
    data: [85, 15],
    backgroundColor: ["#3E8635", "#A30000"],
    borderColor: ["#1E4F18", "#7D1007"],
    borderWidth: 2,
    hoverOffset: 4
  }],
  extra: { ... }  // optional, see Extra Data below
}
options: {
  cutout: "60%",
  plugins: {
    legend: { position: "bottom" },
    tooltip: { callbacks: { label: function } },
    datalabels: { ... }
  }
}
```

### `line`

Line chart / time series (Chart.js).

```javascript
data: {
  labels: ["2024-01", "2024-02"],
  datasets: [{
    label: "Cycle Time",
    data: [3.5, 4.2],
    showLine: true,
    pointRadius: 3,
    backgroundColor: "rgba(54,162,235,0.2)",
    borderColor: "rgb(54,162,235)",
    borderWidth: 2,
    borderDash: [5, 5],       // optional — dashed line
    fill: false,              // or "-1" to fill between datasets
    tension: 0.3,             // curve smoothness
    order: 1                  // optional — draw order
  }]
}
options: {
  scales: {
    x: { title: { display: true, text: "Month" } },
    y: {
      beginAtZero: true,
      type: "logarithmic",    // optional
      title: { display: true, text: "Days" },
      ticks: { callback: function, stepSize: 1 }
    }
  },
  plugins: {
    legend: { labels: { usePointStyle: true } },
    tooltip: { callbacks: { label: function } }
  }
}
```

### `bar`

Horizontal or vertical bar chart (Chart.js).

```javascript
data: {
  labels: ["Fast", "Average", "Slow"],
  datasets: [{
    label: "PR count",
    data: [12, 45, 8],
    backgroundColor: ["rgba(34,197,94,0.8)", ...],
    borderColor: ["rgba(34,197,94,1)", ...],
    borderWidth: 1,
    borderRadius: 4,          // optional
    barThickness: 20          // optional
  }],
  extra: { ... }              // optional, see Extra Data below
}
options: {
  indexAxis: "y",              // "y" = horizontal bars, "x" = vertical (default)
  scales: {
    x: { beginAtZero: true, title: { display: true, text: "Count" } },
    y: { grid: { display: false } }
  },
  plugins: {
    legend: { display: true, position: "top" }
  }
}
```

### `scatter`

Scatter plot (Chart.js).

```javascript
data: {
  datasets: [{
    label: "PRs",
    data: [{ x: 150, y: 3.2 }, { x: 20, y: 0.5 }],
    backgroundColor: "rgba(54,162,235,0.6)",
    pointRadius: 5,
    pointHoverRadius: 8
  }]
}
options: {
  scales: {
    x: { type: "linear", position: "bottom", beginAtZero: true, title: { text: "Lines Changed" } },
    y: { type: "linear", beginAtZero: true, title: { text: "Days" } }
  }
}
```

### `table`

Data table with sortable columns.

```javascript
data: {
  columns: ["Repository", "PR Count", "Median Cycle Time (hrs)"],
  rows: [
    {
      "Repository": "org/repo-name",
      "PR Count": 42,
      "Median Cycle Time (hrs)": { display: "<b>3.5</b>", sort: 3.5 }
    }
  ],
  defaultSort: "Median Cycle Time (hrs)", // optional — column to sort by
  fullRepoNames: true                     // optional — show full org/repo names
}
```

Column names starting with `_` are hidden (metadata only). Cell values can be plain strings/numbers or objects with `display` (rendered HTML) and `sort` (sort key).

### `sankey`

Sankey flow diagram (chartjs-chart-sankey plugin).

```javascript
data: {
  datasets: [{
    label: "PR Lifecycle Flow",
    data: [
      { from: "opened", to: "reviewed", flow: 65.2, count: 196 },
      { from: "opened", to: "merged",   flow: 4.0,  count: 12 }
    ],
    color: { opened: "#0066CC", reviewed: "#6753AC", ... },
    colorMode: "gradient",
    labels: { opened: "Opened", reviewed: "Reviewed", ... },
    priority: { opened: 0, reviewed: 1, approved: 2, merged: 3, abandoned: 4 },
    column: { opened: 0, reviewed: 1, approved: 2, merged: 3, abandoned: 3 },
    size: "max"
  }],
  extra: { ... },                // optional, see Extra Data below
  medianReviewInterval: 4.5,     // optional
  transfers: { ... },            // optional — raw transfer counts
  percentages: { ... }           // optional — transfer percentages
}
```

Each entry in `data` represents a flow band. `flow` is the display value (percentage of total), `count` is the raw number. `column` fixes nodes to specific x-axis columns. `priority` controls vertical ordering within a column.

Flows smaller than `minFlow` percent of the total (default `2`) are drawn at that size so small branches stay visible instead of collapsing into hairlines; tooltips still show the real `flow`. The total is the summed flow leaving the source nodes (nodes no flow points to). Set `minFlow: 0` to draw strictly to scale.

### `knots`

Cycle time breakdown shown as connected stage cards.

```javascript
data: {
  stages: [
    { label: "Pickup", value: 12.5, icon: "hourglass" },
    { label: "Review", value: 8.3 },
    { label: "Integration", value: 2.1 }
  ]
}
```

### `stagedoughnut`

Stage-based doughnut for status breakdown (e.g. Jira issue stages).

```javascript
data: {
  stages: [
    { label: "In Progress", category: "IN_PROGRESS", value: 24.5 },
    { label: "Review",      category: "IN_PROGRESS", value: 8.2 },
    { label: "Done",        category: "DONE",        value: 0.5 }
  ],
  resolvedCount: 142
}
```

`category` is one of `"TODO"`, `"IN_PROGRESS"`, or `"DONE"`. `value` is the median hours spent in each stage.

---

## Extra Data (Drill-Down Lists)

The `extra` structure enables interactive drill-down lists from visualizations. It is placed inside `data.extra`.

```javascript
extra: {
  config: [ /* array of config entries */ ],
  data: { /* field → items mapping */ }
}
```

### Config Entry

Each entry in `config` describes one drill-down link or interactive element:

```javascript
{
  location: "appendToAnalytics" | "inDiagram",
  type: "openList",
  field: "slow_prs",              // key into extra.data
  template: "Show Slow PRs",     // button label or popup title
  labels: [ ... ],               // column definitions (appendToAnalytics)
  elements: { ... }              // element-to-data mapping (inDiagram)
}
```

### Location: `appendToAnalytics`

Renders a clickable button in the AI analytics tooltip. Clicking opens a popup table.

Required fields: `field`, `template`, `labels`.

```javascript
{
  location: "appendToAnalytics",
  type: "openList",
  field: "closed_without_merge",
  template: "Show Closed w/o Merge PRs",
  labels: [
    { label: "PR ID", field: "id" },
    { label: "PR", field: "title", link: "url" },
    { label: "Z-Score", field: "z_score" }
  ]
}
```

`labels` defines the table columns. Each `field` value is read from items in `extra.data[field]`.
The optional `link` names another key in the same item that holds an `http(s)` URL; the cell text
then links to that URL and opens in a new tab. The URL key doesn't need its own column. Cells whose
value is itself an `http(s)` URL are always linked.

### Location: `inDiagram`

Maps interactive diagram elements to data lists. Clicking a diagram element (e.g. a Sankey flow band) opens a popup table. Column labels are auto-generated from data item keys.

Required fields: `elements`.

```javascript
{
  location: "inDiagram",
  type: "openList",
  elements: {
    "opened->reviewed": { field: "opened_to_reviewed", template: "Opened → Reviewed PRs" },
    "opened->merged":   { field: "opened_to_merged",   template: "Opened → Merged PRs" }
  }
}
```

Each key in `elements` identifies a diagram element (for Sankey: `"from->to"` matching the flow data). `field` points to the data array in `extra.data`, `template` is the popup title.

### Data

The `data` object maps field names to arrays of items:

```javascript
data: {
  closed_without_merge: [
    { id: "1234", url: "https://github.com/org/repo/pull/1234" },
    { id: "5678", url: "https://github.com/org/repo/pull/5678" }
  ],
  slow_prs: [
    { id: "42", url: "https://...", z_score: 2.31, days: 14.5 }
  ]
}
```

Items can have any fields. When `labels` is provided in the config entry, only the specified fields are shown as columns. When `labels` is omitted (as in `inDiagram`), columns are auto-generated from the first item's keys.
