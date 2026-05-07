// argo-rollouts-progressive-delivery-plan T5.2 fixture.
//
// This app deliberately fails / and /health with 500 so that:
//   - the renderer wires Rollout pod readinessProbe to /health, so canary
//     pods never reach Ready → Argo Rollouts marks the Rollout phase
//     Degraded after spec.progressDeadlineSeconds (set to 300s by the
//     renderer for ALL canaries), AND
//   - if a Prometheus AnalysisTemplate is wired (pro/team plan +
//     PrometheusAddress configured), success-rate on the canary Service
//     drops below the analysis threshold and triggers an explicit abort.
//
// Locally (starter plan, no Prometheus) the Degraded transition comes
// via the readiness path. /readiness still returns 200 to keep kubelet
// from killing the pod; the renderer just doesn't probe it.

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
