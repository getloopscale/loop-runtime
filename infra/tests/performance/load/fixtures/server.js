// Minimal Node.js HTTP server for load test fixtures.
// This is the app that gets built and deployed during load tests.
// Must be as small as possible — the goal is testing the pipeline, not the app.
const http = require("http");
const port = process.env.PORT || 8080;
http.createServer((req, res) => {
  res.writeHead(200, { "Content-Type": "application/json" });
  res.end(JSON.stringify({ status: "ok", app: process.env.LOOPRUNTIME_PROJECT || "loadtest" }));
}).listen(port, () => console.log(`listening on ${port}`));
