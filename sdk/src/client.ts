import { lstatSync } from "node:fs";
import { createConnection } from "node:net";
import {
  EnvctlError,
  EnvctlLockedError,
  EnvctlProtocolError,
  type EnvctlErrorCode,
} from "./errors.js";
import { resolveSocketPath } from "./paths.js";
import {
  PROTOCOL_VERSION,
  type DaemonStatus,
  type WireRequest,
  type WireResponse,
} from "./protocol.js";

const DEFAULT_TIMEOUT_MS = 5000;
const MAX_RESPONSE_BYTES = 16 * 1024 * 1024;

export interface ClientOptions {
  socketPath?: string;
  timeoutMs?: number;
}

function isErrnoException(err: unknown): err is NodeJS.ErrnoException {
  return err instanceof Error && "code" in err;
}

function assertSocketSafe(socketPath: string): void {
  let stat;
  try {
    stat = lstatSync(socketPath);
  } catch (err: unknown) {
    if (isErrnoException(err) && err.code === "ENOENT") {
      throw new EnvctlLockedError(
        `envctl daemon is not running (socket not found at ${socketPath}). Run 'envctl unlock' and retry.`,
        { cause: err },
      );
    }
    throw new EnvctlProtocolError(
      `Failed to inspect envctl socket at ${socketPath}`,
      "INTERNAL",
      { cause: err },
    );
  }

  if (!stat.isSocket()) {
    throw new EnvctlProtocolError(
      `Path ${socketPath} is not a Unix domain socket`,
      "INVALID_REQUEST",
    );
  }
  if (typeof process.getuid === "function" && stat.uid !== process.getuid()) {
    throw new EnvctlProtocolError(
      `Refusing to connect to socket ${socketPath} owned by uid ${stat.uid}`,
      "INVALID_REQUEST",
    );
  }
}

export async function sendRequest(
  req: Omit<WireRequest, "v">,
  options: ClientOptions = {},
): Promise<WireResponse> {
  const socketPath = resolveSocketPath(options.socketPath);
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;

  assertSocketSafe(socketPath);

  const wireReq: WireRequest = { ...req, v: PROTOCOL_VERSION };
  const payload = JSON.stringify(wireReq) + "\n";

  return new Promise<WireResponse>((resolve, reject) => {
    let settled = false;
    const chunks: Buffer[] = [];
    let totalBytes = 0;

    const finish = (err: Error | null, resp?: WireResponse): void => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      socket.destroy();
      if (err) {
        reject(err);
      } else if (resp) {
        resolve(resp);
      }
    };

    const socket = createConnection({ path: socketPath });

    const timer = setTimeout(() => {
      finish(
        new EnvctlProtocolError(
          `Timed out after ${timeoutMs}ms waiting for envctl daemon`,
          "INTERNAL",
        ),
      );
    }, timeoutMs);

    socket.on("connect", () => {
      socket.write(payload);
    });

    socket.on("data", (chunk: Buffer) => {
      totalBytes += chunk.length;
      if (totalBytes > MAX_RESPONSE_BYTES) {
        finish(new EnvctlProtocolError("Daemon response exceeded maximum size"));
        return;
      }
      chunks.push(chunk);
      const newlineIdx = chunk.indexOf(0x0a);
      if (newlineIdx !== -1) {
        const raw = Buffer.concat(chunks).toString("utf8");
        const line = raw.slice(0, raw.indexOf("\n"));
        parseAndFinish(line, finish);
      }
    });

    socket.on("end", () => {
      if (settled) {
        return;
      }
      const raw = Buffer.concat(chunks).toString("utf8").trim();
      if (!raw) {
        finish(new EnvctlProtocolError("Daemon closed connection without a response"));
        return;
      }
      parseAndFinish(raw, finish);
    });

    socket.on("error", (err: NodeJS.ErrnoException) => {
      if (err.code === "ENOENT" || err.code === "ECONNREFUSED") {
        finish(
          new EnvctlLockedError(
            "envctl daemon is not running (vault is locked). Run 'envctl unlock' and retry.",
            { cause: err },
          ),
        );
        return;
      }
      finish(
        new EnvctlProtocolError(
          `Failed to communicate with envctl daemon: ${err.message}`,
          "INTERNAL",
          { cause: err },
        ),
      );
    });
  });
}

function parseAndFinish(
  line: string,
  finish: (err: Error | null, resp?: WireResponse) => void,
): void {
  let parsed: WireResponse;
  try {
    parsed = JSON.parse(line) as WireResponse;
  } catch (err: unknown) {
    finish(new EnvctlProtocolError("Received invalid JSON from envctl daemon", "PROTOCOL_ERROR", { cause: err }));
    return;
  }

  if (parsed.v !== PROTOCOL_VERSION) {
    finish(
      new EnvctlProtocolError(
        `Protocol version mismatch: SDK supports v${PROTOCOL_VERSION}, daemon returned v${parsed.v}`,
        "VERSION_MISMATCH",
      ),
    );
    return;
  }

  if (!parsed.ok) {
    const code = (parsed.error_code ?? "INTERNAL") as EnvctlErrorCode;
    const message = parsed.error ?? "envctl daemon returned an error";
    if (code === "LOCKED") {
      finish(new EnvctlLockedError(message));
      return;
    }
    finish(new EnvctlError(code, message));
    return;
  }

  finish(null, parsed);
}

export async function fetchStatus(options: ClientOptions = {}): Promise<DaemonStatus> {
  try {
    const resp = await sendRequest({ op: "status" }, options);
    const st = resp.status;
    if (!st) {
      return { locked: true };
    }
    const result: DaemonStatus = { locked: st.locked };
    if (st.mode !== undefined) {
      result.mode = st.mode;
    }
    if (st.expires_at !== undefined) {
      result.expiresAt = new Date(st.expires_at);
    }
    if (st.session_pid !== undefined) {
      result.sessionPid = st.session_pid;
    }
    if (st.vault_path !== undefined) {
      result.vaultPath = st.vault_path;
    }
    return result;
  } catch (err: unknown) {
    if (err instanceof EnvctlLockedError) {
      return { locked: true };
    }
    throw err;
  }
}

export async function fetchSecrets(
  keys: readonly string[],
  options: ClientOptions = {},
): Promise<{ values: Record<string, string>; missing: string[] }> {
  if (keys.length === 0) {
    return { values: {}, missing: [] };
  }
  const resp = await sendRequest({ op: "get", keys }, options);
  return {
    values: resp.values ?? {},
    missing: resp.missing ?? [],
  };
}
