const express = require("express");
const app = express();
const port = process.env.PORT || 3001;

app.get("/health", (req, res) => res.json({ status: "ok" }));

// Returns the SECRET_PING value if set; otherwise a fixed sentinel string.
// Test asserts on this endpoint to verify ESO sync + env var injection.
app.get("/secret-check", (req, res) => res.json({
  secret_ping: process.env.SECRET_PING || "unset",
}));

const server = app.listen(port, () => console.log(`secret-app listening on ${port}`));
const shutdown = (sig) => { console.log(sig); server.close(() => process.exit(0)); setTimeout(() => process.exit(1), 10000); };
process.on("SIGTERM", () => shutdown("SIGTERM"));
process.on("SIGINT", () => shutdown("SIGINT"));
