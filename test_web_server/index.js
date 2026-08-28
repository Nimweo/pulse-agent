import express from "express";
import { pathToFileURL } from "node:url";
import zlib from "node:zlib";

const MAX_POINTS = 600;
const MAX_BATCHES = 50;
const MAX_SEEN_BATCHES = 1_000;
const MAX_BODY_BYTES = 10 * 1024 * 1024;
const SUPPORTED_SCHEMA_VERSION = 1;

const app = express();
const clients = new Set();
const seenBatchIDs = new Set();
const payloads = [];
const store = {
  t: [],
  cpu: [],
  mem: [],
  latest_metrics: {},
  latest_payload: null,
  stats: {
    batches: 0,
    core_samples: 0,
    points: 0,
    last_received_at: null,
  },
};

function pushCoreSample(sample) {
  store.t.push(sample.time / 1000);
  store.cpu.push(sample.cpu);
  store.mem.push(sample.mem_total ? (sample.mem_used / sample.mem_total) * 100 : 0);
  while (store.t.length > MAX_POINTS) {
    store.t.shift();
    store.cpu.shift();
    store.mem.shift();
  }
}

function rememberBatchID(batchID) {
  seenBatchIDs.add(batchID);
  if (seenBatchIDs.size > MAX_SEEN_BATCHES) {
    seenBatchIDs.delete(seenBatchIDs.values().next().value);
  }
}

function storePayload(payload) {
  payload.core.forEach(pushCoreSample);
  for (const point of payload.points) {
    store.latest_metrics[`${point.metric}:${point.device}`] = {
      time: point.time,
      value: point.value,
    };
  }

  store.latest_payload = payload;
  store.stats.batches++;
  store.stats.core_samples += payload.core.length;
  store.stats.points += payload.points.length;
  store.stats.last_received_at = new Date().toISOString();

  payloads.push({ received_at: store.stats.last_received_at, payload });
  if (payloads.length > MAX_BATCHES) payloads.shift();
}

function validatePayload(payload) {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    return "request body must be a JSON object";
  }
  if (payload.schema_version !== SUPPORTED_SCHEMA_VERSION) {
    return `schema_version must equal ${SUPPORTED_SCHEMA_VERSION}`;
  }
  if (typeof payload.batch_id !== "string" || payload.batch_id.length === 0) {
    return "batch_id must be a non-empty string";
  }
  if (!Number.isFinite(payload.sent_at)) {
    return "sent_at must be a number";
  }
  if (typeof payload.agent_version !== "string" || payload.agent_version.length === 0) {
    return "agent_version must be a non-empty string";
  }
  if (typeof payload.hostname !== "string" || payload.hostname.length === 0) {
    return "hostname must be a non-empty string";
  }
  if (!Array.isArray(payload.core)) {
    return "core must be an array";
  }
  if (!Array.isArray(payload.points)) {
    return "points must be an array";
  }
  if (payload.system !== undefined && (payload.system === null || typeof payload.system !== "object")) {
    return "system must be an object when provided";
  }

  for (const sample of payload.core) {
    if (!Number.isFinite(sample?.time) || !Number.isFinite(sample?.cpu)) {
      return "each core sample must contain numeric time and cpu values";
    }
  }
  for (const point of payload.points) {
    if (
      !Number.isFinite(point?.time) ||
      typeof point?.metric !== "string" ||
      typeof point?.device !== "string" ||
      !Number.isFinite(point?.value)
    ) {
      return "each point must contain time, metric, device, and value";
    }
  }

  return null;
}

// Preserve the raw request so both plain JSON and gzip payloads can be tested.
app.use((req, res, next) => {
  if (req.method !== "POST") return next();

  const chunks = [];
  let size = 0;
  let tooLarge = false;
  req.on("data", (chunk) => {
    size += chunk.length;
    if (size > MAX_BODY_BYTES) {
      tooLarge = true;
      return;
    }
    chunks.push(chunk);
  });
  req.on("end", () => {
    if (tooLarge) {
      res.status(413).json({ error: "request body is too large" });
      return;
    }
    req.rawBody = Buffer.concat(chunks);
    req.rawBodySize = size;
    next();
  });
  req.on("error", next);
});

app.use((req, res, next) => {
  if (!req.rawBody?.length) return next();

  const parseJSON = (body) => {
    try {
      req.body = JSON.parse(body.toString("utf8"));
      next();
    } catch {
      res.status(400).json({ error: "request body contains invalid JSON" });
    }
  };

  if (req.headers["content-encoding"] === "gzip") {
    zlib.gunzip(req.rawBody, { maxOutputLength: MAX_BODY_BYTES }, (err, body) => {
      if (err) {
        res.status(400).json({ error: "request body contains invalid gzip data" });
        return;
      }
      parseJSON(body);
    });
    return;
  }

  if (req.headers["content-encoding"]) {
    res.status(415).json({ error: "unsupported content encoding" });
    return;
  }
  parseJSON(req.rawBody);
});

app.get(["/health", "/api/health"], (_req, res) => {
  res.status(200).json({ status: "ok" });
});

app.post(["/ingest", "/api/ingest"], (req, res) => {
  const validationError = validatePayload(req.body);
  if (validationError) {
    res.status(422).json({ error: validationError });
    return;
  }

  if (seenBatchIDs.has(req.body.batch_id)) {
    res.status(200).json({ accepted: true, duplicate: true });
    return;
  }

  rememberBatchID(req.body.batch_id);
  storePayload(req.body);

  console.log(
    `[${store.stats.last_received_at}] ${req.body.hostname} ` +
      `batch=${req.body.batch_id} core=${req.body.core.length} ` +
      `points=${req.body.points.length} body=${req.rawBodySize}B`
  );

  const message = `data: ${JSON.stringify(req.body)}\n\n`;
  for (const client of clients) client.write(message);

  res.status(202).json({
    accepted: true,
    duplicate: false,
    batch_id: req.body.batch_id,
    core_samples: req.body.core.length,
    points: req.body.points.length,
  });
});

app.get("/stream", (req, res) => {
  res.writeHead(200, {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive",
  });
  res.write("\n");
  clients.add(res);
  req.on("close", () => clients.delete(res));
});

app.get(["/data", "/api/data"], (_req, res) => res.json(store));
app.get(["/payloads", "/api/payloads"], (_req, res) => res.json(payloads));

app.get("/", (_req, res) => {
  res.type("html").send(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Pulse — live dashboard</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/uplot@1.6.32/dist/uPlot.min.css">
<style>
  body { background:#18181b; color:#e4e4e7; font:14px/1.5 system-ui,sans-serif; margin:0; padding:24px; }
  h1 { font-size:16px; font-weight:600; margin:0 0 4px; }
  h2 { font-size:14px; margin:28px 0 8px; }
  .meta { color:#a1a1aa; font-size:12px; margin-bottom:20px; font-family:ui-monospace,monospace; }
  .u-legend { color:#e4e4e7 !important; }
  pre { background:#09090b; border:1px solid #27272a; border-radius:6px; max-height:360px; overflow:auto; padding:16px; }
</style>
</head>
<body>
  <h1>Nimweo Pulse — live dashboard</h1>
  <div class="meta" id="meta">waiting for data…</div>
  <div id="chart"></div>
  <h2>Latest payload</h2>
  <pre id="payload">No payload received.</pre>

<script src="https://cdn.jsdelivr.net/npm/uplot@1.6.32/dist/uPlot.iife.min.js"></script>
<script>
const MAX = ${MAX_POINTS};
let data = [[], [], []];
let plot;
let stats = { batches: 0, core_samples: 0, points: 0 };

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

function addSample(sample) {
  data[0].push(sample.time / 1000);
  data[1].push(sample.cpu);
  data[2].push(sample.mem_total ? (sample.mem_used / sample.mem_total) * 100 : 0);
}

function renderPayload(payload) {
  document.getElementById("payload").textContent = payload
    ? JSON.stringify(payload, null, 2)
    : "No payload received.";
}

function renderStats() {
  document.getElementById("meta").textContent =
    "batches: " + stats.batches +
    " · core: " + stats.core_samples +
    " · points: " + stats.points;
}

function acceptPayload(payload) {
  payload.core.forEach(addSample);
  while (data[0].length > MAX) data.forEach((series) => series.shift());
  stats.batches++;
  stats.core_samples += payload.core.length;
  stats.points += payload.points.length;
  plot.setData(data);
  renderStats();
  renderPayload(payload);
}

async function boot() {
  const initial = await (await fetch("/data")).json();
  data = [initial.t, initial.cpu, initial.mem];
  stats = initial.stats;
  plot = new uPlot(opts, data, document.getElementById("chart"));
  renderStats();
  renderPayload(initial.latest_payload);

  const events = new EventSource("/stream");
  events.onmessage = (event) => acceptPayload(JSON.parse(event.data));
  events.onerror = () => {
    document.getElementById("meta").textContent = "disconnected — refresh the page";
  };
}
boot();
</script>
</body>
</html>`);
});

export { app };

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const port = Number.parseInt(process.env.PORT ?? "3000", 10);
  app.listen(port, () =>
    console.log(
      `health: GET http://localhost:${port}/api/health\n` +
        `ingest: POST http://localhost:${port}/api/ingest\n` +
        `payloads: GET http://localhost:${port}/api/payloads\n` +
        `dashboard: http://localhost:${port}`
    )
  );
}
