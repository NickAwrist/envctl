import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fetchSecrets, fetchStatus, type ClientOptions } from "./client.js";
import {
  extractPlaceholders,
  hasPlaceholder,
  parseDotenv,
  replacePlaceholders,
  type ParsedDotenv,
} from "./dotenv.js";
import {
  EnvctlError,
  EnvctlLockedError,
  EnvctlMissingKeysError,
  EnvctlProtocolError,
  type EnvctlErrorCode,
} from "./errors.js";
import { resolveSocketPath } from "./paths.js";
import type { DaemonStatus, UnlockMode } from "./protocol.js";

export {
  EnvctlError,
  EnvctlLockedError,
  EnvctlMissingKeysError,
  EnvctlProtocolError,
  extractPlaceholders,
  hasPlaceholder,
  parseDotenv,
  replacePlaceholders,
  resolveSocketPath,
};
export type { ClientOptions, DaemonStatus, EnvctlErrorCode, ParsedDotenv, UnlockMode };

export interface ResolveEnvOptions extends ClientOptions {
  /** Path to .env file. Defaults to ".env" in process.cwd(). */
  path?: string;
  /** Override values already set in process.env. Defaults to false. */
  override?: boolean;
  /** Existing environment used for precedence checks. Defaults to process.env. */
  baseEnv?: Readonly<Record<string, string | undefined>>;
  /**
   * Do not throw if the .env file does not exist.
   * Defaults to true when `path` is omitted, false when `path` is explicit.
   */
  optional?: boolean;
}

export interface LoadEnvOptions extends ResolveEnvOptions {
  /** Target object to mutate. Defaults to process.env. */
  target?: Record<string, string | undefined>;
}

function isEnoent(err: unknown): boolean {
  return err instanceof Error && "code" in err && (err as NodeJS.ErrnoException).code === "ENOENT";
}

/**
 * Reads a .env file (if present) and scans baseEnv for %ENVCTL-...% placeholders,
 * resolves all referenced secrets via the running envctl daemon in a single batch
 * call, and returns the effective value of every variable named in the .env file
 * or resolved from baseEnv, without mutating process.env.
 */
export async function resolveEnv(
  options: ResolveEnvOptions = {},
): Promise<Record<string, string>> {
  const envPath = resolve(options.path ?? ".env");
  const override = options.override ?? false;
  const baseEnv = options.baseEnv ?? process.env;
  const optional = options.optional ?? options.path === undefined;

  let parsed: ParsedDotenv = { values: {}, order: [] };
  try {
    const content = await readFile(envPath, "utf8");
    parsed = parseDotenv(content);
  } catch (err: unknown) {
    if (!optional || !isEnoent(err)) {
      const reason = err instanceof Error ? err.message : String(err);
      throw new EnvctlError("ENV_FILE", `Cannot read env file: ${reason}`, { cause: err });
    }
  }

  const resolved: Record<string, string> = {};
  const pending = new Map<string, string>();
  const neededSecrets = new Set<string>();

  for (const envKey of parsed.order) {
    const val = parsed.values[envKey] ?? "";
    if (!override) {
      const existing = baseEnv[envKey];
      if (existing !== undefined && !hasPlaceholder(existing)) {
        resolved[envKey] = existing;
        continue;
      }
    }
    pending.set(envKey, val);
    for (const secretName of extractPlaceholders(val)) {
      neededSecrets.add(secretName);
    }
  }

  // Also resolve any %ENVCTL-...% placeholders already injected into process.env
  // (for example by systemd EnvironmentFile, deployctl, or Docker Compose).
  for (const [envKey, existing] of Object.entries(baseEnv)) {
    if (existing === undefined || !hasPlaceholder(existing)) {
      continue;
    }
    if (override && pending.has(envKey)) {
      continue;
    }
    pending.set(envKey, existing);
    for (const secretName of extractPlaceholders(existing)) {
      neededSecrets.add(secretName);
    }
  }

  let secrets: Record<string, string> = {};
  if (neededSecrets.size > 0) {
    const sorted = [...neededSecrets].sort();
    const res = await fetchSecrets(sorted, options);
    if (res.missing.length > 0) {
      throw new EnvctlMissingKeysError(res.missing);
    }
    secrets = res.values;
  }

  for (const [envKey, raw] of pending) {
    if (hasPlaceholder(raw)) {
      const { value, missing } = replacePlaceholders(raw, secrets);
      if (missing.length > 0) {
        throw new EnvctlMissingKeysError(missing);
      }
      resolved[envKey] = value;
    } else {
      resolved[envKey] = raw;
    }
  }

  return resolved;
}

/**
 * Reads a .env file, resolves %ENVCTL-...% placeholders via the envctl daemon,
 * writes the resolved values into process.env (or options.target), and returns
 * them. Precedence is checked against options.target unless baseEnv is given.
 */
export async function loadEnv(
  options: LoadEnvOptions = {},
): Promise<Record<string, string>> {
  const target = options.target ?? process.env;
  const resolved = await resolveEnv({
    ...options,
    baseEnv: options.baseEnv ?? target,
  });
  for (const [key, value] of Object.entries(resolved)) {
    target[key] = value;
  }
  return resolved;
}

/**
 * Fetches a single secret directly from the running envctl daemon.
 */
export async function getSecret(
  key: string,
  options: ClientOptions = {},
): Promise<string> {
  const res = await fetchSecrets([key], options);
  if (res.missing.length > 0 || !Object.prototype.hasOwnProperty.call(res.values, key)) {
    throw new EnvctlMissingKeysError([key]);
  }
  return res.values[key] ?? "";
}

/**
 * Fetches multiple secrets in a single daemon request.
 */
export async function getSecrets(
  keys: readonly string[],
  options: ClientOptions = {},
): Promise<Record<string, string>> {
  const res = await fetchSecrets(keys, options);
  if (res.missing.length > 0) {
    throw new EnvctlMissingKeysError(res.missing);
  }
  return res.values;
}

/**
 * Checks whether the local envctl daemon is unlocked and returns its metadata.
 */
export async function getStatus(options: ClientOptions = {}): Promise<DaemonStatus> {
  return fetchStatus(options);
}
