#!/usr/bin/env node
// Fixture stand-in for a child_process.spawnSync list-arg Node agent.
// Each call passes argv as a list — libuv's uv_spawn uses execvp, so
// PATH lookup catches the wrap shim. The third call uses an absolute
// path on purpose; that one bypasses the shim until Piece D runtime
// hooks land.
const { spawnSync } = require("node:child_process");

const calls = [
  [["git", ["--version"]], "PATH lookup"],
  [["ls", ["-la"]], "PATH lookup"],
  [["/usr/bin/env", ["echo", "absolute path"]], "BYPASSES wrap shim"],
];

let rc = 0;
for (const [[cmd, args], note] of calls) {
  // stdio[2]=inherit so shim spans land in outer stderr; see
  // node_shell_true.cjs for rationale.
  const r = spawnSync(cmd, args, { encoding: "utf8", stdio: ["pipe", "pipe", "inherit"] });
  process.stdout.write(`[fixture] ${note}: ${cmd} ${args.join(" ")}\n  rc=${r.status}\n`);
  if (r.status !== 0 && r.status !== null) rc = r.status;
}
process.exit(rc);
