// Chart driver for /bots/:name/report. Fetches each panel from the
// ace-hosted /api/metrics/range proxy (never talks to prometheus
// directly — that would need CORS and expose the URL) and renders
// with Chart.js line charts.
//
// The panel list is intentionally hard-coded here to match the Go
// handler's promPanels allowlist; adding a new panel means editing
// both files, which is the right amount of friction to keep the
// dashboard focused.

(function () {
  const cfg = window.aceBotReport;
  if (!cfg) return;

  // Result → color map. Keyed by the "result" label value so pass is
  // always green regardless of the order prometheus returned series in
  // (an alphabetic sort would put "fail" before "pass" and swap the
  // colors on any panel that had both).
  const resultColors = {
    pass:    "#28a745", // green
    fail:    "#dc3545", // red
    error:   "#6f42c1", // purple
    stopped: "#6c757d", // grey
    mixed:   "#fd7e14", // orange — some pass, some fail
    empty:   "#adb5bd", // muted — run finished with 0 calls
  };
  // Fallback palette for series that don't carry a "result" label
  // (latency / MOS / RTT panels usually have one series with no label).
  const fallbackColors = ["#0d6efd", "#20c997", "#e83e8c", "#ffc107"];

  const panels = [
    { id: "runs_rate",  panel: "runs_rate",  stacked: true,  yLabel: "runs / interval" },
    { id: "calls_rate", panel: "calls_rate", stacked: true,  yLabel: "calls / interval" },
    { id: "invite_p95", panel: "invite_p95", stacked: false, yLabel: "ms" },
    { id: "mos_rx",     panel: "mos_rx",     stacked: false, yLabel: "MOS", yMax: 5, yMin: 1 },
    { id: "rtt_avg",    panel: "rtt_avg",    stacked: false, yLabel: "ms" },
  ];

  const charts = {};

  function seriesLabel(metric, fallback) {
    // For runs_rate/calls_rate the "result" label is the meaningful
    // dimension; for latency panels there's usually just one series
    // and we fall back to the panel name.
    return metric.result || metric.code_class || fallback;
  }

  function render(panelCfg, resp) {
    const canvas = document.getElementById(panelCfg.id);
    if (!canvas) return;
    const ctx = canvas.getContext("2d");

    // Collect timestamps across all series so gaps in one series line
    // up on the x-axis with values in another (Chart.js needs a shared
    // label array for stacked mode to make sense).
    const tsSet = new Set();
    resp.result.forEach((s) => s.values.forEach((v) => tsSet.add(v[0])));
    const timestamps = Array.from(tsSet).sort((a, b) => a - b);
    const labels = timestamps.map((t) => new Date(t * 1000));

    const datasets = resp.result.map((s, i) => {
      const map = new Map(s.values.map((v) => [v[0], parseFloat(v[1])]));
      // Color-by-result-label so pass=green, fail=red regardless of the
      // series order prometheus returns. Series without a result label
      // fall through to the neutral palette.
      const color =
        (s.metric.result && resultColors[s.metric.result]) ||
        fallbackColors[i % fallbackColors.length];
      return {
        label: seriesLabel(s.metric, panelCfg.panel),
        data: timestamps.map((t) => (map.has(t) ? map.get(t) : null)),
        borderColor: color,
        backgroundColor: color + "55",
        fill: panelCfg.stacked,
        tension: 0.2,
        pointRadius: 0,
        spanGaps: false,
      };
    });

    if (charts[panelCfg.id]) charts[panelCfg.id].destroy();
    charts[panelCfg.id] = new Chart(ctx, {
      type: "line",
      data: { labels, datasets },
      options: {
        responsive: true,
        animation: false,
        interaction: { intersect: false, mode: "index" },
        plugins: {
          legend: { position: "bottom", labels: { boxWidth: 12 } },
          tooltip: { callbacks: { label: (ctx) => `${ctx.dataset.label}: ${ctx.parsed.y}` } },
        },
        scales: {
          x: {
            type: "time",
            time: { tooltipFormat: "yyyy-MM-dd HH:mm" },
            grid: { display: false },
          },
          y: {
            stacked: panelCfg.stacked,
            beginAtZero: true,
            min: panelCfg.yMin,
            max: panelCfg.yMax,
            title: { display: true, text: panelCfg.yLabel, color: "#6c757d" },
          },
        },
      },
    });
  }

  function loadAll() {
    const rangeMin = document.getElementById("rangeSel").value;
    const loading = document.getElementById("loading");
    loading.classList.remove("d-none");
    const startedByEnc = encodeURIComponent(cfg.startedBy);
    // Fire all requests in parallel. A slow panel doesn't block others,
    // and Promise.allSettled means one panel's error (e.g. bad label
    // filter surviving the escape) doesn't hide the rest.
    const inflight = panels.map((p) =>
      fetch(`/api/metrics/range?panel=${p.panel}&started_by=${startedByEnc}&range_min=${rangeMin}`)
        .then((r) => (r.ok ? r.json() : Promise.reject(r.statusText)))
        .then((resp) => render(p, resp))
        .catch((err) => {
          const canvas = document.getElementById(p.id);
          if (canvas) {
            const parent = canvas.parentElement;
            parent.innerHTML = `<div class="text-danger small">Failed to load ${p.panel}: ${err}</div>`;
          }
        })
    );
    Promise.allSettled(inflight).finally(() => loading.classList.add("d-none"));
  }

  // Chart.js needs an adapter for the time scale. date-fns is the
  // upstream-recommended pair and adds ~10KB; it registers itself on
  // Chart when loaded.
  const dateAdapter = document.createElement("script");
  dateAdapter.src = "https://cdn.jsdelivr.net/npm/chartjs-adapter-date-fns@3.0.0/dist/chartjs-adapter-date-fns.bundle.min.js";
  dateAdapter.onload = () => {
    loadAll();
    document.getElementById("rangeSel").addEventListener("change", loadAll);
  };
  document.head.appendChild(dateAdapter);
})();
