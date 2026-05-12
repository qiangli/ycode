#!/usr/bin/env node
// Fixture stand-in for a child_process.exec heavy Node agent (Claude
// Code / opencode style). exec is always shell-form; it invokes
// /bin/sh -c <cmd> under the hood.
//
// Baseline test for what Claude Code's shell-string behavior produces
// without the Piece D runtime hooks in place.
const { execSync } = require("node:child_process");

const cmds = [
  "git --version",
  "ls -la",
  "echo 'shell=true ran' | wc -c",
];

// Inherit stderr so the shim's per-exec span debug line reaches the
// outer ycode wrap stderr (where the smoke matrix counts it). Real
// agents capture stderr; Piece D's runtime hook closes that gap.
let rc = 0;
for (const cmd of cmds) {
  try {
    const out = execSync(cmd, { encoding: "utf8", stdio: ["pipe", "pipe", "inherit"] });
    process.stdout.write(`[fixture] $ ${cmd}\n  rc=0 bytes=${out.length}\n`);
  } catch (err) {
    process.stdout.write(`[fixture] $ ${cmd}\n  rc=${err.status}\n`);
    rc = err.status ?? 1;
  }
}
process.exit(rc);
