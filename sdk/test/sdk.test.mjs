import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { after, before, describe, it } from "node:test";
import {
  EnvctlLockedError,
  EnvctlMissingKeysError,
  extractPlaceholders,
  getSecret,
  getSecrets,
  getStatus,
  loadEnv,
  parseDotenv,
  replacePlaceholders,
  resolveEnv,
} from "../dist/esm/index.js";

const require = createRequire(import.meta.url);
const repoRoot = resolve(import.meta.dirname, "..", "..");

describe("dotenv parser & placeholder helpers", () => {
  it("parses standard .env formats and comments", () => {
    const parsed = parseDotenv(`
# header comment
export API_KEY=%ENVCTL-OPENROUTER_KEY%
PLAIN=hello world # comment
URL=https://example.com#frag
DOUBLE="a\\nb\\"c\\""
SINGLE='raw\\n%ENVCTL-RAW%'
`);
    assert.deepEqual(parsed.values, {
      API_KEY: "%ENVCTL-OPENROUTER_KEY%",
      PLAIN: "hello world",
      URL: "https://example.com#frag",
      DOUBLE: 'a\nb"c"',
      SINGLE: "raw\\n%ENVCTL-RAW%",
    });
  });

  it("extracts and replaces inline placeholders", () => {
    const raw = "postgres://u:%ENVCTL-DB_PASS%@host/%ENVCTL-DB_NAME%";
    assert.deepEqual(extractPlaceholders(raw), ["DB_PASS", "DB_NAME"]);
    const res = replacePlaceholders(raw, { DB_PASS: "p@ss", DB_NAME: "app" });
    assert.equal(res.value, "postgres://u:p@ss@host/app");
    assert.deepEqual(res.missing, []);
  });

  it("skips daemon connection when existing env already provides non-placeholder values", async () => {
    const dir = mkdtempSync(join(tmpdir(), "envctl-sdk-nod-"));
    try {
      const envPath = join(dir, ".env");
      writeFileSync(envPath, "API_KEY=%ENVCTL-MY_KEY%\nPORT=8080\n");

      const resolved = await resolveEnv({
        path: envPath,
        socketPath: join(dir, "does-not-exist.sock"),
        baseEnv: { API_KEY: "already-from-ci" },
      });
      assert.deepEqual(resolved, { PORT: "8080" });
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});

describe("SDK integration against real Go envctl daemon", () => {
  let workDir;
  let sockDir;
  let binPath;
  let sockPath;
  let cliEnv;

  before(() => {
    workDir = mkdtempSync(join(tmpdir(), "envctl-e2e-"));
    sockDir = mkdtempSync(join("/tmp", "envctl-sdk-"));
    binPath = join(workDir, "envctl");
    sockPath = join(sockDir, "a.sock");
    cliEnv = {
      ...process.env,
      ENVCTL_CONFIG_DIR: join(workDir, "config"),
      ENVCTL_DATA_DIR: join(workDir, "data"),
      ENVCTL_SOCKET: sockPath,
    };

    execFileSync("go", ["build", "-o", binPath, "./cmd/envctl"], {
      cwd: repoRoot,
      stdio: "pipe",
    });

    const pwFile = join(workDir, "pw.txt");
    writeFileSync(pwFile, "sdk-integration-pass\n", { mode: 0o600 });

    execFileSync(binPath, ["init", "--password-file", pwFile], { env: cliEnv });
    execFileSync(binPath, ["unlock", "--until-lock", "--password-file", pwFile], { env: cliEnv });
    execFileSync(binPath, ["set", "OPENROUTER_API_KEY", "--stdin"], {
      env: cliEnv,
      input: "sk-or-from-go-daemon\n",
    });
    execFileSync(binPath, ["set", "DB_PASS", "--stdin"], {
      env: cliEnv,
      input: "pg-secret\n",
    });
  });

  after(() => {
    try {
      execFileSync(binPath, ["lock"], { env: cliEnv });
    } catch {
      // ignore if already locked
    }
    rmSync(workDir, { recursive: true, force: true });
    rmSync(sockDir, { recursive: true, force: true });
  });

  it("reads status and individual/batched secrets over Unix socket", async () => {
    const status = await getStatus({ socketPath: sockPath });
    assert.equal(status.locked, false);
    assert.equal(status.mode, "until_lock");

    const single = await getSecret("OPENROUTER_API_KEY", { socketPath: sockPath });
    assert.equal(single, "sk-or-from-go-daemon");

    const batch = await getSecrets(["OPENROUTER_API_KEY", "DB_PASS"], { socketPath: sockPath });
    assert.deepEqual(batch, {
      OPENROUTER_API_KEY: "sk-or-from-go-daemon",
      DB_PASS: "pg-secret",
    });
  });

  it("throws EnvctlMissingKeysError with all missing key names", async () => {
    await assert.rejects(
      () => getSecrets(["OPENROUTER_API_KEY", "MISSING_A", "MISSING_B"], { socketPath: sockPath }),
      (err) => {
        assert.ok(err instanceof EnvctlMissingKeysError);
        assert.deepEqual(err.missingKeys, ["MISSING_A", "MISSING_B"]);
        return true;
      },
    );
  });

  it("resolves .env without mutating process.env in resolveEnv, and mutates target in loadEnv", async () => {
    const envPath = join(workDir, "app.env");
    writeFileSync(
      envPath,
      [
        "API_KEY=%ENVCTL-OPENROUTER_API_KEY%",
        "DATABASE_URL=postgres://app:%ENVCTL-DB_PASS%@localhost:5432/db",
        "PORT=3000",
      ].join("\n"),
    );

    const pure = await resolveEnv({ path: envPath, socketPath: sockPath, baseEnv: {} });
    assert.deepEqual(pure, {
      API_KEY: "sk-or-from-go-daemon",
      DATABASE_URL: "postgres://app:pg-secret@localhost:5432/db",
      PORT: "3000",
    });
    assert.equal(process.env["API_KEY"], undefined);

    const target = { PORT: "9999" };
    await loadEnv({ path: envPath, socketPath: sockPath, target });
    assert.equal(target.API_KEY, "sk-or-from-go-daemon");
    assert.equal(target.DATABASE_URL, "postgres://app:pg-secret@localhost:5432/db");
    assert.equal(target.PORT, "9999");
  });

  it("supports CJS require() consumer", async () => {
    const cjs = require("../dist/cjs/index.js");
    const val = await cjs.getSecret("OPENROUTER_API_KEY", { socketPath: sockPath });
    assert.equal(val, "sk-or-from-go-daemon");
  });

  it("supports --import @nickawrist/envctl/register preload hook", () => {
    const projDir = join(workDir, "preload-proj");
    execFileSync("mkdir", ["-p", projDir]);
    writeFileSync(join(projDir, ".env"), "PRELOADED_KEY=%ENVCTL-OPENROUTER_API_KEY%\n");
    const registerPath = resolve(repoRoot, "sdk", "dist", "esm", "register.js");

    const out = execFileSync(
      process.execPath,
      ["--import", registerPath, "-e", "process.stdout.write(process.env.PRELOADED_KEY ?? '')"],
      {
        cwd: projDir,
        env: { ...process.env, ENVCTL_SOCKET: sockPath },
        encoding: "utf8",
      },
    );
    assert.equal(out, "sk-or-from-go-daemon");
  });

  it("throws EnvctlLockedError and reports locked status once daemon is locked", async () => {
    execFileSync(binPath, ["lock"], { env: cliEnv });

    const status = await getStatus({ socketPath: sockPath });
    assert.equal(status.locked, true);

    await assert.rejects(
      () => getSecret("OPENROUTER_API_KEY", { socketPath: sockPath }),
      (err) => err instanceof EnvctlLockedError,
    );
  });
});
