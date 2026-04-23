// Watches logviz log lines for an `Authorization: Bearer <token>` phrase and
// writes the token into the `dev-server-gateway` entry of ~/.cursor/mcp.json
// (at mcpServers.dev-server-gateway.headers.Authorization). Useful when a dev
// server prints a fresh bearer token on each start and you want Cursor's MCP
// client to pick it up automatically. The `dev-server-gateway` entry must
// already exist in mcp.json; the plugin won't create it from scratch. Updates
// are skipped when the token hasn't changed since the last write.

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const readline = require('node:readline');

const SERVER_NAME = 'dev-server-gateway';

const MCP_PATH = path.join(os.homedir(), '.cursor', 'mcp.json');
const BEARER_RE = /Authorization:\s*Bearer\s+([A-Za-z0-9._\-]+)/;

let lastToken = null;

function setAuth(token) {
  let config = {};
  try {
    config = JSON.parse(fs.readFileSync(MCP_PATH, 'utf8'));
  } catch (err) {
    if (err.code !== 'ENOENT') throw err;
  }
  config.mcpServers ??= {};
  if (!config.mcpServers[SERVER_NAME]) {
    console.error('dev-server-gateway not found in MCP config');
    return;
  }
  config.mcpServers[SERVER_NAME] ??= {};
  config.mcpServers[SERVER_NAME].headers ??= {};
  config.mcpServers[SERVER_NAME].headers.Authorization = `Bearer ${token}`;
  fs.mkdirSync(path.dirname(MCP_PATH), { recursive: true });
  fs.writeFileSync(MCP_PATH, JSON.stringify(config, null, 2) + '\n');
}

readline.createInterface({ input: process.stdin }).on('line', (line) => {
  let log;
  try { log = JSON.parse(line); } catch { return; }
  const m = BEARER_RE.exec(log.text);
  if (!m) return;
  const token = m[1];
  if (token === lastToken) return;
  lastToken = token;
  setAuth(token);
  console.error(`updated dev-server-gateway Authorization (${token.slice(0, 6)}…)`);
});
