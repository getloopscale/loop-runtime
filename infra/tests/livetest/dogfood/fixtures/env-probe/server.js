// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// env-probe: the app the staging validate deploys (journey_app_lifecycle_live_test.go).
//
//   GET /health       {"status":"ok"}
//   GET /env/<NAME>   {"present": bool, "sha256": hex or null} for the variable NAME
//
// The value of a variable never leaves the process: the caller compares the
// SHA-256 with the one of the value it set. Only names that start with PROBE_
// are answered, so the route says nothing about the platform's own variables.
//
// Every request is one JSON line on stdout, which is what the runtime log
// stream is checked against.
//
// No dependency: node's standard library only.
"use strict";

const http = require("node:http");
const crypto = require("node:crypto");

const port = Number(process.env.PORT) || 3000;
const probeName = /^PROBE_[A-Z0-9_]{1,64}$/;

function send(res, status, body) {
  res.writeHead(status, { "Content-Type": "application/json", "Cache-Control": "no-store" });
  res.end(JSON.stringify(body));
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, "http://env-probe");
  console.log(JSON.stringify({ msg: "request", method: req.method, path: url.pathname, marker: url.searchParams.get("marker") }));

  if (req.method !== "GET") {
    return send(res, 405, { error: "method not allowed" });
  }
  if (url.pathname === "/health") {
    return send(res, 200, { status: "ok" });
  }
  const match = /^\/env\/([^/]+)$/.exec(url.pathname);
  if (match) {
    if (!probeName.test(match[1])) {
      return send(res, 400, { error: "only PROBE_* variables are answered" });
    }
    const value = process.env[match[1]];
    if (value === undefined) {
      return send(res, 200, { present: false, sha256: null });
    }
    return send(res, 200, { present: true, sha256: crypto.createHash("sha256").update(value).digest("hex") });
  }
  return send(res, 404, { error: "not found" });
});

server.listen(port, "0.0.0.0", () => console.log(JSON.stringify({ msg: "listening", port })));

function shutdown(signal) {
  console.log(JSON.stringify({ msg: "shutdown", signal }));
  server.close(() => process.exit(0));
  setTimeout(() => process.exit(1), 10000).unref();
}
process.on("SIGTERM", () => shutdown("SIGTERM"));
process.on("SIGINT", () => shutdown("SIGINT"));
