// argo-rollouts-progressive-delivery-plan T5.2 fixture.
//
// This app deliberately fails / and /health with 500 so that:
//   - kubelet readinessProbe (path /readiness) keeps the pod Ready,
//   - Traefik traffic to / and /health returns 500 → success-rate < 0.90
//   - Argo Rollouts AnalysisTemplate detects degraded canary and auto-aborts.
//
// Isolated cause: pod IS healthy from kubelet's perspective (readinessProbe
// hits /readiness which always returns 200). Only application traffic
// fails. This validates the success-rate query path specifically — not
// the kubelet pod-failure path.

const express = require("express");
const app = express();
const port = process.env.PORT || 3000;

// /readiness returns 200 — kubelet sees pod as Ready.
app.get("/readiness", (req, res) => res.json({ ready: true }));

// /health returns 500 — Traefik logs 500 → Prometheus success-rate drops.
app.get("/health", (req, res) => {
  res.writeHead(500, { "Content-Type": "application/json" });
  res.end(JSON.stringify({ error: "intentionally-bad-canary-health" }));
});

// Default route also 500 so any traffic produces 5xx metrics.
app.get("*", (req, res) => {
  res.writeHead(500, { "Content-Type": "application/json" });
  res.end(JSON.stringify({ error: "intentionally-bad-canary-default" }));
});

const server = app.listen(port, () => console.log(`bad-health canary listening on ${port} (intentionally returns 500)`));
const shutdown = (sig) => {
  console.log(sig);
  server.close(() => process.exit(0));
  setTimeout(() => process.exit(1), 10000);
};
process.on("SIGTERM", () => shutdown("SIGTERM"));
process.on("SIGINT", () => shutdown("SIGINT"));
