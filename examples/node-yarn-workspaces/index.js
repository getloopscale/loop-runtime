const { greet } = require("pkg-a");

const name = process.env.USER_NAME || "World";
console.log(greet(name));
