const express = require("express");
const fs = require("fs");
const path = require("path");
const app = express();
const port = process.env.PORT || 3001;

// VERSION is read from a file that the journey rewrites between deploys.
// Default fallback to env var APP_VERSION if file absent.
let version = "v0";
try {
  version = fs.readFileSync(path.join(__dirname, "VERSION.txt"), "utf-8").trim();
} catch (_) {
  version = process.env.APP_VERSION || "v0";
}

app.get("/health", (req, res) => res.json({ status: "ok", version }));
app.get("/version", (req, res) => res.json({ version }));

const server = app.listen(port, () => console.log(`versioned-app v=${version} listening on ${port}`));
const shutdown = (sig) => { console.log(sig); server.close(() => process.exit(0)); setTimeout(() => process.exit(1), 10000); };
process.on("SIGTERM", () => shutdown("SIGTERM"));
process.on("SIGINT", () => shutdown("SIGINT"));
