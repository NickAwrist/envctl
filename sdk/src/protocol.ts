export const PROTOCOL_VERSION = 1;

export type UnlockMode = "ttl" | "session" | "until_lock";

export interface DaemonStatus {
  locked: boolean;
  mode?: UnlockMode;
  expiresAt?: Date;
  sessionPid?: number;
  vaultPath?: string;
}

export interface WireStatus {
  locked: boolean;
  mode?: UnlockMode;
  expires_at?: string;
  session_pid?: number;
  vault_path?: string;
}

export interface WireRequest {
  v: number;
  op: "status" | "get";
  keys?: readonly string[];
}

export interface WireResponse {
  v: number;
  ok: boolean;
  error_code?: string;
  error?: string;
  status?: WireStatus;
  values?: Record<string, string>;
  missing?: string[];
}
