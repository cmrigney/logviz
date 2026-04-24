// Watches logviz log lines for an `Authorization: Bearer <token>` phrase and
// writes the token into the configured entry of the Cursor MCP config file.
//
// Protocol 1 plugin: reads a `hello` message first to configure serverName
// and mcpPath. Falls back to defaults if not provided.

const readline = require('node:readline');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

let serverName = 'dev-server-gateway';          // default (matches manifest)
let mcpPath    = path.join(os.homedir(), '.cursor', 'mcp.json');
const BEARER_RE = /Authorization:\s*Bearer\s+([A-Za-z0-9._\-]+)/;
let lastToken = null;

function expandHome(p) {
  return p.startsWith('~') ? path.join(os.homedir(), p.slice(1)) : p;
}

function setAuth(token) {
  let config = {};
  try {
    config = JSON.parse(fs.readFileSync(mcpPath, 'utf8'));
  } catch (err) {
    if (err.code !== 'ENOENT') throw err;
  }
  config.mcpServers ??= {};
  if (!config.mcpServers[serverName]) {
    console.error(`${serverName} not found in MCP config`);
    return;
  }
  config.mcpServers[serverName].headers ??= {};
  config.mcpServers[serverName].headers.Authorization = `Bearer ${token}`;
  fs.mkdirSync(path.dirname(mcpPath), { recursive: true });
  fs.writeFileSync(mcpPath, JSON.stringify(config, null, 2) + '\n');
}

readline.createInterface({ input: process.stdin }).on('line', (raw) => {
  let msg;
  try { msg = JSON.parse(raw); } catch { return; }

  if (msg.type === 'hello') {
    if (msg.config?.serverName) serverName = msg.config.serverName;
    if (msg.config?.mcpPath)    mcpPath    = expandHome(msg.config.mcpPath);
    return;
  }

  if (msg.type !== 'log') return;

  const m = BEARER_RE.exec(msg.line.text);
  if (!m) return;
  if (m[1] === lastToken) return;
  lastToken = m[1];
  setAuth(lastToken);
  console.error(`updated ${serverName} Authorization (${lastToken.slice(0, 6)}…)`);
});
