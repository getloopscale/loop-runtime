// Minimal static site builder — produces a dist/ folder with index.html.
// No external dependencies needed (pure Node.js).
// This replaces Vite to keep the fixture zero-dependency.

const fs = require('fs');
const path = require('path');

const dist = path.join(__dirname, 'dist');
fs.mkdirSync(dist, { recursive: true });

const html = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>LoopRuntime Load Test - Static Frontend</title>
</head>
<body>
  <div id="root">
    <h1>LoopRuntime Static Frontend</h1>
    <p>Build timestamp: ${new Date().toISOString()}</p>
    <p>Status: OK</p>
  </div>
  <script>
    // Minimal JS to verify the app loaded
    document.getElementById('root').dataset.loaded = 'true';
  </script>
</body>
</html>`;

fs.writeFileSync(path.join(dist, 'index.html'), html);

// Generate a minimal package-lock.json if missing (some build steps expect it)
console.log('Static frontend built successfully → dist/index.html');
