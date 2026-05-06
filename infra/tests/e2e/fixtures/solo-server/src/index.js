const express = require("express");

const app = express();
const port = process.env.PORT || 3001;

app.get("/", (req, res) => {
  res.json({
    project: "solo-server",
    message: "Hello from solo-server (dogfood fixture)",
  });
});

app.get("/health", (req, res) => res.json({ status: "ok" }));
app.get("/ready", (req, res) => res.json({ status: "ready" }));

const server = app.listen(port, () => console.log(`solo-server listening on ${port}`));

const shutdown = (sig) => {
  console.log(`${sig} received`);
  server.close(() => process.exit(0));
  setTimeout(() => process.exit(1), 10000);
};
process.on("SIGTERM", () => shutdown("SIGTERM"));
process.on("SIGINT", () => shutdown("SIGINT"));
