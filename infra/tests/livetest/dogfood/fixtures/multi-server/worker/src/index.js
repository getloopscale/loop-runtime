// Minimal worker: prints heartbeat every 10s. No HTTP server (type=worker
// in loopzero.yaml means no IngressRoute / Service is generated).
let count = 0;
const heartbeat = () => {
  count++;
  console.log(JSON.stringify({
    app: "worker",
    project: "multi-server",
    heartbeat: count,
    timestamp: new Date().toISOString(),
  }));
};
heartbeat();
const interval = setInterval(heartbeat, 10000);

const shutdown = (sig) => {
  console.log(`${sig} received — stopping worker`);
  clearInterval(interval);
  process.exit(0);
};
process.on("SIGTERM", () => shutdown("SIGTERM"));
process.on("SIGINT", () => shutdown("SIGINT"));
