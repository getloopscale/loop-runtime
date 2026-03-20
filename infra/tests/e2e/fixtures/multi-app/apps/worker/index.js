// Worker process — no HTTP server, just a loop.
console.log("worker started");

setInterval(() => {
  console.log("worker tick", new Date().toISOString());
}, 5000);
