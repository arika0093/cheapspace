import fs from "node:fs";
import path from "node:path";
import { spawn } from "node:child_process";

const dataDir = path.resolve("tmp", "e2e");
fs.rmSync(dataDir, { recursive: true, force: true });
fs.mkdirSync(dataDir, { recursive: true });

const child = spawn(
  "go",
  ["run", "./cmd/cheapspace", "serve"],
  {
    stdio: "inherit",
    env: {
      ...process.env,
      CHEAPSPACE_RUNTIME: "mock",
      CHEAPSPACE_ADDR: "127.0.0.1:4173",
      CHEAPSPACE_DATA_DIR: dataDir,
      CHEAPSPACE_PUBLIC_HOST: "localhost",
      CHEAPSPACE_APP_SECRET: "cheapspace-e2e-secret",
      CHEAPSPACE_AUTO_MIGRATE: "true",
    },
  },
);

const forward = (signal) => {
  if (!child.killed) {
    child.kill(signal);
  }
};

process.on("SIGINT", () => forward("SIGINT"));
process.on("SIGTERM", () => forward("SIGTERM"));
child.on("exit", (code) => process.exit(code ?? 0));

