// argo-rollouts-progressive-delivery-plan T5.1 fixture.
// Healthy app backing the canary happy-path Rollout. /health returns
// 200 so kubelet readinessProbe (which the renderer wires to /health)
// succeeds and the Rollout phase progresses to Healthy after the
// canary steps complete.

const express = require("express");

const app = express();
const port = process.env.PORT || 3001;

app.get("/", (req, res) => {
  res.json({
    project: "rollout-canary-app",
    message: "Hello from canary-happy fixture",
  });
});

app.get("/health", (req, res) => res.json({ status: "ok" }));
app.get("/ready", (req, res) => res.json({ status: "ready" }));

const server = app.listen(port, () => console.log(`rollout-canary-app listening on ${port}`));

const shutdown = (sig) => {
  console.log(`${sig} received`);
  server.close(() => process.exit(0));
  setTimeout(() => process.exit(1), 10000);
};
process.on("SIGTERM", () => shutdown("SIGTERM"));
process.on("SIGINT", () => shutdown("SIGINT"));
