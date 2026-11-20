// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Behaviour tests for worker.mjs, run with `node --test`. The Worker is a service
// worker script: it is loaded into an isolated context whose globals stand in for the
// Cloudflare runtime (the LOOPZERO_ROUTING KV binding and fetch).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";

const source = readFileSync(new URL("./worker.mjs", import.meta.url), "utf8");

function loadWorker(kv) {
  const fetches = [];
  let handler;
  const context = vm.createContext({
    URL,
    Headers,
    Response,
    addEventListener: (_type, fn) => { handler = fn; },
    LOOPZERO_ROUTING: { get: async (key) => (key in kv ? kv[key] : null) },
    fetch: async (input, init) => {
      fetches.push({ url: typeof input === "string" ? input : input.url, init });
      return new Response("upstream", { status: 200 });
    },
  });
  vm.runInContext(source, context);
  const handle = (url) =>
    new Promise((resolve) => handler({ request: new Request(url), respondWith: resolve }));
  return { handle, fetches };
}

// #448: the engine publishes route:<label> → {cellId}; the edge maps the cell to its own
// ingress. The request keeps its URL and Host (the cell routes by Host) and is resolved to
// the cell's ingress hostname — a Worker cannot fetch a raw IP.
test("a published host is sent to its cell's ingress with its Host intact", async () => {
  const { handle, fetches } = loadWorker({
    "route:tn-a-web-production": { cellId: "cell-1" },
    "ingress:cell-1": { resolveOverride: "cell-1.ingress.dev.getloopscale.com" },
  });
  const res = await handle("https://tn-a-web-production.dev.getloopscale.com/api?x=1");
  assert.equal(res.status, 200);
  assert.equal(fetches.length, 1);
  assert.equal(new URL(fetches[0].url).host, "tn-a-web-production.dev.getloopscale.com");
  assert.equal(fetches[0].init.cf.resolveOverride, "cell-1.ingress.dev.getloopscale.com");
});

// A cell the edge has no ingress for is refused, never guessed: 503, nothing fetched.
test("a route whose cell has no ingress is refused with 503", async () => {
  const { handle, fetches } = loadWorker({ "route:tn-a-web-production": { cellId: "cell-9" } });
  const res = await handle("https://tn-a-web-production.dev.getloopscale.com/");
  assert.equal(res.status, 503);
  assert.equal(fetches.length, 0);
});

// A custom domain is published under its full name.
test("a custom domain is routed by its full hostname", async () => {
  const { handle, fetches } = loadWorker({
    "route:shop.example.com": { cellId: "cell-0" },
    "ingress:cell-0": { resolveOverride: "cell-0.ingress.dev.getloopscale.com" },
  });
  const res = await handle("https://shop.example.com/");
  assert.equal(res.status, 200);
  assert.equal(fetches[0].init.cf.resolveOverride, "cell-0.ingress.dev.getloopscale.com");
});

// The old tenant-parsing path (cell:<tenant> from an {app}.{tenant} host) is gone: the
// platform never emits such hosts, and nothing ever wrote those keys.
test("no route and no static app passes the request through unchanged", async () => {
  const { handle, fetches } = loadWorker({ "cell:web": { cellId: "cell-1", ingressIp: "10.0.0.1" } });
  await handle("https://app.web.dev.getloopscale.com/");
  assert.equal(fetches.length, 1);
  assert.equal(fetches[0].init, undefined, "a pass-through carries no override");
});
