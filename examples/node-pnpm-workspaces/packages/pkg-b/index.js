const { greet } = require("pkg-a");

function combinedGreeting(name) {
  const base = greet(name);
  return `${base} Enhanced by pkg-b.`;
}

module.exports = { combinedGreeting };
