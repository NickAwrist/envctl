import { tmpdir } from "node:os";
import { join } from "node:path";

export function resolveSocketPath(override?: string): string {
  if (override) {
    return override;
  }
  const envSock = process.env["ENVCTL_SOCKET"];
  if (envSock) {
    return envSock;
  }
  const runtimeDir = process.env["XDG_RUNTIME_DIR"];
  if (runtimeDir) {
    const candidate = join(runtimeDir, "envctl", "agent.sock");
    if (candidate.length < 96) {
      return candidate;
    }
  }
  const uid = typeof process.getuid === "function" ? process.getuid() : 0;
  const tmpCandidate = join(tmpdir(), `envctl-${uid}`, "agent.sock");
  if (tmpCandidate.length < 96) {
    return tmpCandidate;
  }
  return join("/tmp", `envctl-${uid}`, "agent.sock");
}
