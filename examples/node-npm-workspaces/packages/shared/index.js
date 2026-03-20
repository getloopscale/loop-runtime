const APP_NAME = "npm-workspaces-api";

function formatMessage(text) {
  return `[${APP_NAME}] ${text}`;
}

module.exports = { APP_NAME, formatMessage };
