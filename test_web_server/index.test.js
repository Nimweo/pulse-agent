import assert from "node:assert/strict";
import { gzipSync } from "node:zlib";
import test from "node:test";

import { app } from "./index.js";

test("test server accepts and stores a compressed metric batch", async (t) => {
  const server = app.listen(0);
  await new Promise((resolve) => server.once("listening", resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));

  const baseURL = `http://127.0.0.1:${server.address().port}`;
  const healthResponse = await fetch(`${baseURL}/api/health`);
  assert.equal(healthResponse.status, 200);

  const dashboardResponse = await fetch(baseURL);
  assert.equal(dashboardResponse.status, 200);
  const dashboard = await dashboardResponse.text();
  assert.match(dashboard, /id="core-chart"/);
  assert.match(dashboard, /id="disk-space-chart"/);
  assert.match(dashboard, /id="disk-io-chart"/);
  assert.match(dashboard, /id="network-chart"/);
  assert.match(dashboard, /id="operating-system"/);
  assert.match(dashboard, /id="processor-model"/);
  assert.match(dashboard, /id="installed-memory"/);
  const inlineScript = dashboard.match(/<script>([\s\S]*?)<\/script>/)?.[1];
  assert.ok(inlineScript, "dashboard must contain its client script");
  assert.doesNotThrow(() => new Function(inlineScript));

  const payload = {
    schema_version: 1,
    batch_id: "server-test-batch",
    sent_at: Date.now(),
    agent_version: "0.4.0",
    hostname: "test-host",
    system: {
      computer_name: "test-computer",
      os: "linux",
      platform: "ubuntu",
      platform_version: "24.04",
      kernel_version: "6.8.0",
      kernel_architecture: "x86_64",
      processor_model: "Example Processor",
      memory_total_bytes: 16_000_000_000,
      physical_cores: 4,
      logical_cpus: 8,
    },
    core: [{ time: Date.now(), cpu: 25, mem_used: 50, mem_total: 100 }],
    points: [{ time: Date.now(), metric: "disk_used_percent", device: "/", value: 50 }],
  };

  const ingestResponse = await fetch(`${baseURL}/api/ingest`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Content-Encoding": "gzip",
    },
    body: gzipSync(JSON.stringify(payload)),
  });
  assert.equal(ingestResponse.status, 202);
  assert.deepEqual(await ingestResponse.json(), {
    accepted: true,
    duplicate: false,
    batch_id: payload.batch_id,
    core_samples: 1,
    points: 1,
  });

  const duplicateResponse = await fetch(`${baseURL}/api/ingest`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  assert.equal(duplicateResponse.status, 200);
  assert.deepEqual(await duplicateResponse.json(), { accepted: true, duplicate: true });

  const dataResponse = await fetch(`${baseURL}/api/data`);
  const data = await dataResponse.json();
  assert.equal(data.stats.batches, 1);
  assert.equal(data.stats.core_samples, 1);
  assert.equal(data.stats.points, 1);
  assert.equal(data.latest_payload.batch_id, payload.batch_id);
});

test("test server rejects an invalid payload", async (t) => {
  const server = app.listen(0);
  await new Promise((resolve) => server.once("listening", resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));

  const response = await fetch(`http://127.0.0.1:${server.address().port}/api/ingest`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ hostname: "missing-envelope" }),
  });
  assert.equal(response.status, 422);
  assert.deepEqual(await response.json(), { error: "schema_version must equal 1" });
});
