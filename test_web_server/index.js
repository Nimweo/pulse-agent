// index.js
import express from "express";
import zlib from "node:zlib";

const app = express();
const MAX_POINTS = 600; // About 10 minutes at 1 Hz.

// Ring buffer. uPlot expects column-oriented data: [[time], [cpu], [mem%]].
const store = { t: [], cpu: [], mem: [] };
const clients = new Set(); // Active SSE connections.

function push(s) {
  store.t.push(s.time / 1000); // uPlot expects seconds rather than milliseconds.
  store.cpu.push(s.cpu);
  store.mem.push(s.mem_total ? (s.mem_used / s.mem_total) * 100 : 0);
  if (store.t.length > MAX_POINTS) {
    store.t.shift();
    store.cpu.shift();
    store.mem.shift();
  }
}

// Receive the raw request body and decompress gzip payloads.
app.use((req, res, next) => {
  if (req.method !== "POST") return next();
  const chunks = [];
  req.on("data", (c) => chunks.push(c));
  req.on("end", () => {
    req.rawBody = Buffer.concat(chunks);
    next();
  });
  req.on("error", next);
});

app.use((req, res, next) => {
  if (!req.rawBody?.length) return next();
  const done = (buf) => {
    try {
      req.body = JSON.parse(buf.toString("utf8"));
      next();
    } catch {
      res.status(400).json({ error: "invalid JSON" });
    }
  };
  if (req.headers["content-encoding"] === "gzip") {
    zlib.gunzip(req.rawBody, (err, buf) =>
      err ? res.status(400).json({ error: "invalid gzip payload" }) : done(buf)
    );
  } else {
    done(req.rawBody);
  }
});

// Agent startup health check.
app.get("/health", (_req, res) => res.status(200).json({ status: "ok" }));

// Metrics ingestion endpoint.
app.post("/ingest", (req, res) => {
  const { h, hostname, core = [] } = req.body ?? {};
  const host = h ?? hostname ?? "?";

  core.forEach(push);

  console.log(
    `[${new Date().toISOString()}] ${host} — core: ${core.length}, ` +
      `raw: ${req.rawBody.length} B, buffer: ${store.t.length}`
  );

  // Broadcast new samples to connected browsers.
  const msg = `data: ${JSON.stringify(core)}\n\n`;
  for (const c of clients) c.write(msg);

  res.status(202).json({ ok: true });
});

// Live SSE stream.
app.get("/stream", (req, res) => {
  res.writeHead(200, {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive",
  });
  res.write("\n"); // Open the stream immediately.
  clients.add(res);
  req.on("close", () => clients.delete(res));
});

// Initial state so the chart is populated after a page refresh.
app.get("/data", (_req, res) => res.json(store));

// Dashboard page.
app.get("/", (_req, res) => {
  res.type("html").send(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Pulse — live dashboard</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/uplot@1.6.32/dist/uPlot.min.css">
<style>
  body { background:#18181b; color:#e4e4e7; font:14px/1.5 system-ui, sans-serif; margin:0; padding:24px; }
  h1 { font-size:16px; font-weight:600; margin:0 0 4px; }
  .meta { color:#a1a1aa; font-size:12px; margin-bottom:20px; font-family:ui-monospace, monospace; }
  .u-legend { color:#e4e4e7 !important; }
  .uplot { margin-bottom:24px; }
</style>
</head>
<body>
  <h1>Nimweo Pulse — live dashboard</h1>
  <div class="meta" id="meta">waiting for data…</div>
  <div id="chart"></div>

<script src="https://cdn.jsdelivr.net/npm/uplot@1.6.32/dist/uPlot.iife.min.js"></script>
<script>
const MAX = ${MAX_POINTS};
let data = [[], [], []];
let plot;

const opts = {
  width: Math.min(window.innerWidth - 48, 1100),
  height: 320,
  title: "CPU / RAM (%)",
  scales: { y: { range: [0, 100] } },
  axes: [
    { stroke: "#a1a1aa", grid: { stroke: "#27272a" }, ticks: { stroke: "#27272a" } },
    { stroke: "#a1a1aa", grid: { stroke: "#27272a" }, ticks: { stroke: "#27272a" } },
  ],
  series: [
    {},
    { label: "CPU %", stroke: "#f97316", width: 2, value: (u, v) => v?.toFixed(1) ?? "--" },
    { label: "RAM %", stroke: "#38bdf8", width: 2, value: (u, v) => v?.toFixed(1) ?? "--" },
  ],
};

function trim() {
  while (data[0].length > MAX) data.forEach((s) => s.shift());
}

function addSample(s) {
  data[0].push(s.time / 1000);
  data[1].push(s.cpu);
  data[2].push(s.mem_total ? (s.mem_used / s.mem_total) * 100 : 0);
}

async function boot() {
  const store = await (await fetch("/data")).json();
  data = [store.t, store.cpu, store.mem];
  plot = new uPlot(opts, data, document.getElementById("chart"));

  const es = new EventSource("/stream");
  es.onmessage = (e) => {
    const core = JSON.parse(e.data);
    core.forEach(addSample);
    trim();
    plot.setData(data);
    document.getElementById("meta").textContent =
      \`points: \${data[0].length} · latest: \${new Date().toLocaleTimeString("en-US")}\`;
  };
  es.onerror = () => {
    document.getElementById("meta").textContent = "disconnected — refresh the page";
  };
}
boot();
</script>
</body>
</html>`);
});

app.listen(3000, () =>
  console.log(
    "health: GET http://localhost:3000/health\n" +
      "ingest: POST http://localhost:3000/ingest\n" +
      "dashboard: http://localhost:3000"
  )
);
