const { greet } = require("pkg-a");
const { combinedGreeting } = require("pkg-b");

console.log(greet("World"));
console.log(combinedGreeting("Developer"));
