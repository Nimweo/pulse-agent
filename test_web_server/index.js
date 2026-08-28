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
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Pulse Signal Room</title>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Crect width='32' height='32' fill='%230b141b'/%3E%3Cpath d='M8 8h8a6 6 0 0 1 0 12h-4v4H8V8Zm4 4v4h4a2 2 0 0 0 0-4h-4Z' fill='%2354c6bb'/%3E%3C/svg%3E">
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/uplot@1.6.32/dist/uPlot.min.css">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&family=IBM+Plex+Sans+Condensed:wght@400;500;600&display=swap" rel="stylesheet">
<style>
  :root {
    color-scheme: dark;
    --ink: #d9e7e5;
    --muted: #81949d;
    --void: #0b141b;
    --panel: #111f29;
    --panel-strong: #162833;
    --line: #263844;
    --cyan: #54c6bb;
    --amber: #f2b84b;
    --coral: #e46f61;
    --blue: #6fa8dc;
    --violet: #a58bd4;
  }
  * { box-sizing: border-box; }
  body {
    min-width: 320px;
    margin: 0;
    background:
      linear-gradient(rgba(84,198,187,.035) 1px, transparent 1px),
      linear-gradient(90deg, rgba(84,198,187,.035) 1px, transparent 1px),
      var(--void);
    background-size: 32px 32px;
    color: var(--ink);
    font: 15px/1.5 "IBM Plex Sans Condensed", sans-serif;
  }
  button, summary { font: inherit; }
  .shell { width: min(1520px, 100%); margin: 0 auto; padding: 28px; }
  .masthead { border: 1px solid var(--line); background: rgba(11,20,27,.9); }
  .masthead-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    min-height: 50px;
    padding: 10px 16px;
    border-bottom: 1px solid var(--line);
  }
  .brand { display: flex; align-items: center; gap: 12px; letter-spacing: .08em; text-transform: uppercase; }
  .brand-mark {
    display: grid;
    width: 29px;
    height: 29px;
    place-items: center;
    border: 1px solid var(--cyan);
    color: var(--cyan);
    font: 500 12px/1 "IBM Plex Mono", monospace;
  }
  .brand-name { font-size: 12px; font-weight: 600; }
  .connection { display: flex; align-items: center; gap: 9px; color: var(--muted); font: 500 11px/1 "IBM Plex Mono", monospace; text-transform: uppercase; }
  .connection-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--amber); box-shadow: 0 0 0 4px rgba(242,184,75,.12); }
  .connection[data-state="live"] .connection-dot { background: var(--cyan); box-shadow: 0 0 0 4px rgba(84,198,187,.12); animation: signal 1.8s ease-out infinite; }
  .connection[data-state="offline"] .connection-dot { background: var(--coral); box-shadow: 0 0 0 4px rgba(228,111,97,.12); animation: none; }
  .intro { display: grid; grid-template-columns: minmax(260px,.8fr) minmax(540px,1.6fr); gap: 40px; padding: 30px 32px 32px; }
  .eyebrow { margin: 0 0 8px; color: var(--cyan); font: 500 11px/1.2 "IBM Plex Mono", monospace; letter-spacing: .12em; text-transform: uppercase; }
  h1 { max-width: 480px; margin: 0; font-size: clamp(34px,4vw,58px); font-weight: 500; line-height: .95; letter-spacing: -.035em; }
  .intro-copy { max-width: 500px; margin: 18px 0 0; color: var(--muted); font-size: 16px; }
  .signal-rail { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); align-content: end; margin: 0; border-top: 1px solid var(--line); border-left: 1px solid var(--line); }
  .signal-cell { min-height: 78px; padding: 13px 15px; border-right: 1px solid var(--line); border-bottom: 1px solid var(--line); }
  .signal-cell dt { margin-bottom: 8px; color: var(--muted); font: 500 10px/1 "IBM Plex Mono", monospace; letter-spacing: .1em; text-transform: uppercase; }
  .signal-cell dd { overflow: hidden; margin: 0; color: var(--ink); font: 500 14px/1.25 "IBM Plex Mono", monospace; text-overflow: ellipsis; white-space: nowrap; }
  .signal-cell dd[data-empty="true"] { color: #566a73; }
  .platform-strip { display: grid; grid-template-columns: 1.2fr 1.8fr 1.25fr 1fr 1.1fr 1.2fr; margin: 0; border-top: 1px solid var(--line); }
  .platform-cell { min-width: 0; padding: 13px 16px 15px; border-right: 1px solid var(--line); }
  .platform-cell:last-child { border-right: 0; }
  .platform-cell dt { margin-bottom: 8px; color: var(--muted); font: 500 9px/1 "IBM Plex Mono", monospace; letter-spacing: .11em; text-transform: uppercase; }
  .platform-cell dd { overflow: hidden; margin: 0; color: var(--ink); font: 500 12px/1.35 "IBM Plex Mono", monospace; text-overflow: ellipsis; white-space: nowrap; }
  .platform-cell dd[data-empty="true"] { color: #566a73; }
  .section-heading { display: flex; align-items: end; justify-content: space-between; gap: 20px; margin: 34px 0 12px; }
  .section-heading h2 { margin: 0; font-size: 21px; font-weight: 500; letter-spacing: -.01em; }
  .section-heading p { margin: 0; color: var(--muted); font: 400 11px/1.4 "IBM Plex Mono", monospace; }
  .instrument-grid { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 12px; }
  .instrument { min-width: 0; border: 1px solid var(--line); background: rgba(17,31,41,.94); }
  .instrument-wide { grid-column: 1 / -1; }
  .instrument-gpu { box-shadow: inset 3px 0 0 rgba(165,139,212,.72); }
  .instrument-gpu .instrument-kicker { color: var(--violet); }
  .instrument-head { display: flex; align-items: start; justify-content: space-between; min-height: 64px; gap: 16px; padding: 13px 15px; border-bottom: 1px solid var(--line); }
  .instrument-kicker { margin: 0 0 4px; color: var(--muted); font: 500 9px/1 "IBM Plex Mono", monospace; letter-spacing: .12em; text-transform: uppercase; }
  .instrument h3 { margin: 0; font-size: 17px; font-weight: 500; }
  .instrument-note { max-width: 260px; margin: 2px 0 0; color: var(--muted); font: 400 10px/1.35 "IBM Plex Mono", monospace; text-align: right; }
  .plot-frame { position: relative; min-height: 282px; padding: 10px 8px 2px; overflow: hidden; }
  .instrument-wide .plot-frame { min-height: 322px; }
  .plot { width: 100%; min-height: 260px; }
  .instrument-wide .plot { min-height: 300px; }
  .empty {
    position: absolute;
    inset: 10px 8px 2px;
    z-index: 2;
    display: grid;
    place-items: center;
    border: 1px dashed #304650;
    color: var(--muted);
    background: rgba(11,20,27,.7);
    font: 400 11px/1.5 "IBM Plex Mono", monospace;
    text-align: center;
  }
  .empty[hidden] { display: none; }
  .uplot { font-family: "IBM Plex Mono", monospace !important; }
  .u-legend { color: var(--ink) !important; font-size: 11px !important; }
  .u-legend .u-series > * { padding: 3px 5px !important; }
  .u-value { color: var(--muted) !important; }
  .inspection { margin-top: 12px; border: 1px solid var(--line); background: rgba(11,20,27,.92); }
  .inspection summary { cursor: pointer; padding: 14px 16px; color: var(--ink); font-weight: 500; list-style-position: inside; }
  .inspection summary:focus-visible { outline: 2px solid var(--cyan); outline-offset: 3px; }
  pre { max-height: 480px; overflow: auto; margin: 0; padding: 18px; border-top: 1px solid var(--line); color: #afc5c7; background: #081015; font: 12px/1.55 "IBM Plex Mono", monospace; }
  .footer { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 10px 24px; padding: 18px 2px 0; color: var(--muted); font: 400 10px/1.5 "IBM Plex Mono", monospace; }
  .footer code { color: var(--cyan); }
  @keyframes signal { 70%,100% { box-shadow: 0 0 0 9px rgba(84,198,187,0); } }
  @media (max-width: 900px) {
    .shell { padding: 16px; }
    .intro { grid-template-columns: 1fr; gap: 26px; padding: 25px 20px; }
    .instrument-grid { grid-template-columns: 1fr; }
    .instrument-wide { grid-column: auto; }
    .instrument-wide .plot-frame { min-height: 282px; }
    .instrument-wide .plot { min-height: 260px; }
    .platform-strip { grid-template-columns: repeat(2,minmax(0,1fr)); }
    .platform-cell { border-bottom: 1px solid var(--line); }
  }
  @media (max-width: 560px) {
    .shell { padding: 0; }
    .masthead, .instrument, .inspection { border-left: 0; border-right: 0; }
    .masthead-top { padding: 10px 14px; }
    .intro { padding: 22px 14px; }
    .signal-rail { grid-template-columns: 1fr; }
    .platform-strip { grid-template-columns: 1fr; }
    .platform-cell { border-right: 0; }
    .section-heading { padding: 0 14px; align-items: start; flex-direction: column; }
    .instrument-grid { gap: 8px; }
    .instrument-head { flex-direction: column; }
    .instrument-note { text-align: left; }
    .footer { padding: 18px 14px; }
  }
  @media (prefers-reduced-motion: reduce) {
    .connection[data-state="live"] .connection-dot { animation: none; }
  }
</style>
</head>
<body>
<main class="shell">
  <header class="masthead">
    <div class="masthead-top">
      <div class="brand"><span class="brand-mark">P/</span><span class="brand-name">Nimweo Pulse</span></div>
      <div class="connection" id="connection" data-state="waiting" aria-live="polite">
        <span class="connection-dot" aria-hidden="true"></span><span id="connection-label">Waiting for stream</span>
      </div>
    </div>
    <div class="intro">
      <div>
        <p class="eyebrow">Telemetry receiver</p>
        <h1>Live machine telemetry</h1>
        <p class="intro-copy">A direct view of every metric batch accepted by the local test endpoint.</p>
      </div>
      <dl class="signal-rail">
        <div class="signal-cell"><dt>Computer</dt><dd id="host" data-empty="true">No signal</dd></div>
        <div class="signal-cell"><dt>Agent</dt><dd id="agent-version" data-empty="true">No signal</dd></div>
        <div class="signal-cell"><dt>Latest batch</dt><dd id="batch-id" data-empty="true">No signal</dd></div>
        <div class="signal-cell"><dt>Accepted</dt><dd id="batch-count">0 batches</dd></div>
      </dl>
    </div>
    <dl class="platform-strip" aria-label="Platform details">
      <div class="platform-cell"><dt>Operating system</dt><dd id="operating-system" data-empty="true">No signal</dd></div>
      <div class="platform-cell"><dt>Processor</dt><dd id="processor-model" data-empty="true">No signal</dd></div>
      <div class="platform-cell"><dt>Installed memory</dt><dd id="installed-memory" data-empty="true">No signal</dd></div>
      <div class="platform-cell"><dt>CPU topology</dt><dd id="cpu-topology" data-empty="true">No signal</dd></div>
      <div class="platform-cell"><dt>Graphics</dt><dd id="graphics-device" data-empty="true">No signal</dd></div>
      <div class="platform-cell"><dt>Kernel</dt><dd id="kernel-details" data-empty="true">No signal</dd></div>
    </dl>
  </header>

  <div class="section-heading">
    <h2>Machine signals</h2>
    <p id="sample-summary">No samples received</p>
  </div>

  <section class="instrument-grid" aria-label="Metric charts">
    <article class="instrument instrument-wide">
      <header class="instrument-head"><div><p class="instrument-kicker">Core</p><h3>Processor &amp; memory</h3></div><p class="instrument-note">Total CPU and active memory · percent</p></header>
      <div class="plot-frame"><div class="empty" id="core-empty">Waiting for core samples.</div><div class="plot" id="core-chart"></div></div>
    </article>
    <article class="instrument">
      <header class="instrument-head"><div><p class="instrument-kicker">Scheduler pressure</p><h3>Load average</h3></div><p class="instrument-note">1, 5 and 15 minute windows</p></header>
      <div class="plot-frame"><div class="empty" id="load-empty">Waiting for load samples.</div><div class="plot" id="load-chart"></div></div>
    </article>
    <article class="instrument">
      <header class="instrument-head"><div><p class="instrument-kicker">Logical topology</p><h3>Per-CPU utilization</h3></div><p class="instrument-note">First 16 logical processors · percent</p></header>
      <div class="plot-frame"><div class="empty" id="cpu-empty">Enable collectors.cpu.per_cpu to populate this chart.</div><div class="plot" id="cpu-chart"></div></div>
    </article>
    <article class="instrument">
      <header class="instrument-head"><div><p class="instrument-kicker">Memory pressure</p><h3>Swap utilization</h3></div><p class="instrument-note">Used swap space · percent</p></header>
      <div class="plot-frame"><div class="empty" id="swap-empty">Waiting for swap samples.</div><div class="plot" id="swap-chart"></div></div>
    </article>
    <article class="instrument">
      <header class="instrument-head"><div><p class="instrument-kicker">Filesystems</p><h3>Disk utilization</h3></div><p class="instrument-note">First 8 mounted filesystems · percent</p></header>
      <div class="plot-frame"><div class="empty" id="disk-space-empty">Waiting for disk usage samples.</div><div class="plot" id="disk-space-chart"></div></div>
    </article>
    <article class="instrument">
      <header class="instrument-head"><div><p class="instrument-kicker">Block devices</p><h3>Disk throughput</h3></div><p class="instrument-note">Aggregate read and write · MiB/s</p></header>
      <div class="plot-frame"><div class="empty" id="disk-io-empty">Waiting for disk I/O samples.</div><div class="plot" id="disk-io-chart"></div></div>
    </article>
    <article class="instrument">
      <header class="instrument-head"><div><p class="instrument-kicker">Interfaces</p><h3>Network throughput</h3></div><p class="instrument-note">Aggregate receive and transmit · MiB/s</p></header>
      <div class="plot-frame"><div class="empty" id="network-empty">Waiting for network I/O samples.</div><div class="plot" id="network-chart"></div></div>
    </article>
  </section>

  <div class="section-heading">
    <h2>Graphics telemetry</h2>
    <p>Metrics depend on GPU vendor and driver support</p>
  </div>

  <section class="instrument-grid" aria-label="GPU metric charts">
    <article class="instrument instrument-wide instrument-gpu">
      <header class="instrument-head"><div><p class="instrument-kicker">Accelerator load</p><h3>GPU &amp; graphics memory</h3></div><p class="instrument-note">Processor and VRAM utilization by device · percent</p></header>
      <div class="plot-frame"><div class="empty" id="gpu-load-empty">GPU detected data will appear when utilization metrics are supported.</div><div class="plot" id="gpu-load-chart"></div></div>
    </article>
    <article class="instrument instrument-gpu">
      <header class="instrument-head"><div><p class="instrument-kicker">Thermal envelope</p><h3>GPU temperature</h3></div><p class="instrument-note">Reported device temperature · °C</p></header>
      <div class="plot-frame"><div class="empty" id="gpu-temperature-empty">Waiting for supported GPU temperature metrics.</div><div class="plot" id="gpu-temperature-chart"></div></div>
    </article>
    <article class="instrument instrument-gpu">
      <header class="instrument-head"><div><p class="instrument-kicker">Power envelope</p><h3>GPU power draw</h3></div><p class="instrument-note">Reported board power · watts</p></header>
      <div class="plot-frame"><div class="empty" id="gpu-power-empty">Waiting for supported GPU power metrics.</div><div class="plot" id="gpu-power-chart"></div></div>
    </article>
  </section>

  <details class="inspection">
    <summary>Inspect latest payload</summary>
    <pre id="payload">No payload received.</pre>
  </details>
  <footer class="footer"><span>History keeps the latest ${MAX_BATCHES} batches.</span><span><code>GET /api/payloads</code> · <code>POST /api/ingest</code></span></footer>
</main>

<script src="https://cdn.jsdelivr.net/npm/uplot@1.6.32/dist/uPlot.iife.min.js"></script>
<script>
const MAX_SAMPLES = ${MAX_POINTS};
const MAX_HISTORY_BATCHES = ${MAX_BATCHES};
const MIB = 1024 * 1024;
const COLORS = ["#54c6bb", "#f2b84b", "#e46f61", "#6fa8dc", "#a58bd4", "#73b477", "#d68fbe", "#c4d36f"];
const plots = new Map();
let batches = [];
let stats = { batches: 0, core_samples: 0, points: 0, last_received_at: null };

function latestPayload() {
  return batches.length ? batches[batches.length - 1] : null;
}

function allCoreSamples() {
  return batches.flatMap((batch) => batch.core).sort((a, b) => a.time - b.time).slice(-MAX_SAMPLES);
}

function allPoints() {
  return batches.flatMap((batch) => batch.points);
}

function coreChart(fields) {
  const samples = allCoreSamples();
  return {
    labels: fields.map((field) => field.label),
    data: [
      samples.map((sample) => sample.time / 1000),
      ...fields.map((field) => samples.map((sample) => field.read(sample))),
    ],
  };
}

function pointChart(metrics, options) {
  const metricSet = new Set(Object.keys(metrics));
  const matching = allPoints().filter((point) => metricSet.has(point.metric));
  const grouped = new Map();
  const keys = new Set();

  for (const point of matching) {
    const key = options.aggregate
      ? point.metric
      : options.seriesByMetric
        ? metrics[point.metric] + " · " + point.device
        : point.device;
    keys.add(key);
    if (!grouped.has(point.time)) grouped.set(point.time, new Map());
    const row = grouped.get(point.time);
    const value = options.transform ? options.transform(point.value) : point.value;
    row.set(key, options.aggregate ? (row.get(key) ?? 0) + value : value);
  }

  let orderedKeys = [...keys].sort((a, b) => a.localeCompare(b, undefined, { numeric: true }));
  if (options.rootFirst) orderedKeys.sort((a, b) => a === "/" ? -1 : b === "/" ? 1 : 0);
  if (options.maxSeries) orderedKeys = orderedKeys.slice(0, options.maxSeries);
  const times = [...grouped.keys()].sort((a, b) => a - b).slice(-MAX_SAMPLES);

  return {
    labels: orderedKeys.map((key) => options.aggregate ? metrics[key] : key),
    data: [
      times.map((time) => time / 1000),
      ...orderedKeys.map((key) => times.map((time) => grouped.get(time).get(key) ?? null)),
    ],
  };
}

const chartSpecs = [
  {
    id: "core",
    percent: true,
    build: () => coreChart([
      { label: "CPU", read: (sample) => sample.cpu },
      { label: "Memory", read: (sample) => sample.mem_total ? sample.mem_used / sample.mem_total * 100 : 0 },
    ]),
  },
  {
    id: "load",
    build: () => coreChart([
      { label: "1 min", read: (sample) => sample.load_1 },
      { label: "5 min", read: (sample) => sample.load_5 },
      { label: "15 min", read: (sample) => sample.load_15 },
    ]),
  },
  {
    id: "cpu",
    percent: true,
    build: () => pointChart({ cpu_usage_percent: "CPU" }, { maxSeries: 16 }),
  },
  {
    id: "swap",
    percent: true,
    build: () => pointChart({ swap_used_percent: "Swap used" }, { aggregate: true }),
  },
  {
    id: "disk-space",
    percent: true,
    build: () => pointChart({ disk_used_percent: "Disk used" }, { maxSeries: 8, rootFirst: true }),
  },
  {
    id: "disk-io",
    build: () => pointChart({
      disk_read_bytes_per_second: "Read",
      disk_write_bytes_per_second: "Write",
    }, { aggregate: true, transform: (value) => value / MIB }),
  },
  {
    id: "network",
    build: () => pointChart({
      network_receive_bytes_per_second: "Receive",
      network_transmit_bytes_per_second: "Transmit",
    }, { aggregate: true, transform: (value) => value / MIB }),
  },
  {
    id: "gpu-load",
    percent: true,
    build: () => pointChart({
      gpu_usage_percent: "GPU",
      gpu_memory_used_percent: "VRAM",
    }, { seriesByMetric: true, maxSeries: 8 }),
  },
  {
    id: "gpu-temperature",
    build: () => pointChart({ gpu_temperature_celsius: "Temperature" }, { maxSeries: 4 }),
  },
  {
    id: "gpu-power",
    build: () => pointChart({ gpu_power_watts: "Power" }, { maxSeries: 4 }),
  },
];

function chartOptions(spec, labels, width) {
  return {
    width: Math.max(width, 280),
    height: spec.id === "core" ? 300 : 260,
    scales: { y: spec.percent ? { range: [0, 100] } : { range: (u, min, max) => [Math.min(0, min), max <= 0 ? 1 : max * 1.12] } },
    axes: [
      { stroke: "#81949d", grid: { stroke: "#1d303a", width: 1 }, ticks: { stroke: "#304650" }, font: "10px IBM Plex Mono" },
      { stroke: "#81949d", grid: { stroke: "#1d303a", width: 1 }, ticks: { stroke: "#304650" }, font: "10px IBM Plex Mono", size: 52 },
    ],
    cursor: { drag: { x: true, y: false } },
    series: [
      { label: "Time" },
      ...labels.map((label, index) => ({
        label,
        stroke: COLORS[index % COLORS.length],
        width: index < 2 ? 2 : 1.4,
        spanGaps: true,
        points: { show: false },
        value: (u, value) => value == null ? "--" : value.toFixed(2),
      })),
    ],
  };
}

function renderChart(spec) {
  const result = spec.build();
  const container = document.getElementById(spec.id + "-chart");
  const empty = document.getElementById(spec.id + "-empty");
  const hasData = result.data[0].length > 0 && result.labels.length > 0;
  empty.hidden = hasData;

  if (!hasData) {
    const previous = plots.get(spec.id);
    if (previous) previous.plot.destroy();
    plots.delete(spec.id);
    container.replaceChildren();
    return;
  }

  const signature = result.labels.join("|");
  const previous = plots.get(spec.id);
  if (!previous || previous.signature !== signature) {
    if (previous) previous.plot.destroy();
    container.replaceChildren();
    const plot = new uPlot(chartOptions(spec, result.labels, container.clientWidth), result.data, container);
    plots.set(spec.id, { plot, signature });
    return;
  }
  previous.plot.setData(result.data);
}

function setSignal(id, value) {
  const element = document.getElementById(id);
  element.textContent = value || "No signal";
  element.dataset.empty = value ? "false" : "true";
}

function formatBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return (bytes / Math.pow(1024, unit)).toFixed(unit >= 3 ? 1 : 0) + " " + units[unit];
}

function formatOperatingSystem(system) {
  let name = system?.platform || system?.os || "";
  const normalized = name.toLowerCase();
  if (normalized === "darwin" || normalized === "macos") name = "macOS";
  else if (normalized === "windows") name = "Windows";
  else if (name) name = name.charAt(0).toUpperCase() + name.slice(1);
  return [name, system?.platform_version].filter(Boolean).join(" ");
}

function graphicsDeviceNames() {
  return [...new Set(
    allPoints()
      .filter((point) => point.metric === "gpu_present" && point.value > 0)
      .map((point) => point.device)
  )].slice(0, 2).join(" · ");
}

function renderHeader() {
  const payload = latestPayload();
  const system = payload?.system || {};
  const latestCore = payload?.core?.length ? payload.core[payload.core.length - 1] : null;
  setSignal("host", system.computer_name || payload?.hostname);
  setSignal("agent-version", payload?.agent_version ? "v" + payload.agent_version : "");
  setSignal("batch-id", payload?.batch_id ? payload.batch_id.slice(0, 16) : "");
  setSignal("operating-system", formatOperatingSystem(system));
  setSignal("processor-model", system.processor_model);
  setSignal("installed-memory", formatBytes(system.memory_total_bytes || latestCore?.mem_total));
  setSignal("graphics-device", graphicsDeviceNames());
  setSignal(
    "cpu-topology",
    system.physical_cores && system.logical_cpus
      ? system.physical_cores + " physical · " + system.logical_cpus + " logical"
      : ""
  );
  setSignal(
    "kernel-details",
    [system.kernel_version, system.kernel_architecture].filter(Boolean).join(" · ")
  );
  document.getElementById("batch-count").textContent = stats.batches + (stats.batches === 1 ? " batch" : " batches");
  document.getElementById("sample-summary").textContent = stats.core_samples + " core samples · " + stats.points + " metric points";
  document.getElementById("payload").textContent = payload ? JSON.stringify(payload, null, 2) : "No payload received.";
}

function renderAll() {
  renderHeader();
  chartSpecs.forEach(renderChart);
}

function setConnection(state, label) {
  const connection = document.getElementById("connection");
  connection.dataset.state = state;
  document.getElementById("connection-label").textContent = label;
}

function acceptPayload(payload) {
  batches.push(payload);
  if (batches.length > MAX_HISTORY_BATCHES) batches.shift();
  stats.batches++;
  stats.core_samples += payload.core.length;
  stats.points += payload.points.length;
  stats.last_received_at = new Date().toISOString();
  renderAll();
}

async function boot() {
  try {
    document.querySelector(".inspection").open = false;
    const [stateResponse, historyResponse] = await Promise.all([fetch("/api/data"), fetch("/api/payloads")]);
    if (!stateResponse.ok || !historyResponse.ok) throw new Error("history request failed");
    const state = await stateResponse.json();
    const history = await historyResponse.json();
    stats = state.stats;
    batches = history.map((entry) => entry.payload);
    renderAll();

    const events = new EventSource("/stream");
    events.onopen = () => setConnection("live", "Stream connected");
    events.onmessage = (event) => acceptPayload(JSON.parse(event.data));
    events.onerror = () => setConnection("offline", "Stream disconnected");
  } catch {
    setConnection("offline", "Dashboard unavailable");
  }
}

let resizeTimer;
window.addEventListener("resize", () => {
  window.clearTimeout(resizeTimer);
  resizeTimer = window.setTimeout(() => {
    for (const [id, entry] of plots) {
      const container = document.getElementById(id + "-chart");
      entry.plot.setSize({ width: Math.max(container.clientWidth, 280), height: id === "core" ? 300 : 260 });
    }
  }, 120);
});

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
