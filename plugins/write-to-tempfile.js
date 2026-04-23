const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const readline = require('node:readline');

const outPath = path.join(os.tmpdir(), `logviz-${process.pid}-${Date.now()}.log`);
const out = fs.createWriteStream(outPath, { flags: 'a' });

console.error(`writing logs to ${outPath}`);

readline.createInterface({ input: process.stdin }).on('line', (line) => {
  const log = JSON.parse(line);
  out.write(`${new Date(log.timeMs).toISOString()} [${log.source}] ${log.text}\n`);
});
