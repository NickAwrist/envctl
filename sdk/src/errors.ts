export type EnvctlErrorCode =
  | "LOCKED"
  | "MISSING_KEYS"
  | "INVALID_PASSWORD"
  | "INVALID_REQUEST"
  | "VERSION_MISMATCH"
  | "NOT_FOUND"
  | "PROTOCOL_ERROR"
  | "INTERNAL";

export class EnvctlError extends Error {
  readonly code: EnvctlErrorCode;

  constructor(code: EnvctlErrorCode, message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "EnvctlError";
    this.code = code;
  }
}

export class EnvctlLockedError extends EnvctlError {
  constructor(
    message = "envctl vault is locked. Run 'envctl unlock' and retry.",
    options?: ErrorOptions,
  ) {
    super("LOCKED", message, options);
    this.name = "EnvctlLockedError";
  }
}

export class EnvctlMissingKeysError extends EnvctlError {
  readonly missingKeys: readonly string[];

  constructor(missingKeys: readonly string[]) {
    super(
      "MISSING_KEYS",
      `Missing envctl secrets: ${missingKeys.join(", ")}`,
    );
    this.name = "EnvctlMissingKeysError";
    this.missingKeys = [...missingKeys];
  }
}

export class EnvctlProtocolError extends EnvctlError {
  constructor(message: string, code: EnvctlErrorCode = "PROTOCOL_ERROR", options?: ErrorOptions) {
    super(code, message, options);
    this.name = "EnvctlProtocolError";
  }
}
