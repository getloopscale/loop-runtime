const express = require("express");
const { APP_NAME, formatMessage } = require("@monorepo/shared");

const app = express();
const port = process.env.PORT || 3000;

app.get("/", (req, res) => {
  res.json({
    app: APP_NAME,
    message: formatMessage("Welcome to the API"),
  });
});

app.get("/health", (req, res) => {
  res.json({ status: "healthy", app: APP_NAME });
});

app.listen(port, () => {
  console.log(`${APP_NAME} listening on port ${port}`);
});
