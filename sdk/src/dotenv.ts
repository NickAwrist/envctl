const PLACEHOLDER_RE = /%ENVCTL-([A-Za-z0-9_][A-Za-z0-9_./-]*)%/g;

export interface ParsedDotenv {
  values: Record<string, string>;
  order: string[];
}

export function parseDotenv(content: string): ParsedDotenv {
  const values: Record<string, string> = {};
  const order: string[] = [];

  for (const rawLine of content.split("\n")) {
    const line = rawLine.endsWith("\r") ? rawLine.slice(0, -1) : rawLine;
    let trimmed = line.trim();
    if (trimmed === "" || trimmed.startsWith("#")) {
      continue;
    }
    if (trimmed.startsWith("export ") || trimmed.startsWith("export\t")) {
      trimmed = trimmed.slice("export".length).trim();
    }
    const eqIdx = trimmed.indexOf("=");
    if (eqIdx <= 0) {
      continue;
    }
    const key = trimmed.slice(0, eqIdx).trim();
    if (!key) {
      continue;
    }
    const rawVal = trimmed.slice(eqIdx + 1).trim();
    const val = parseValue(rawVal);

    if (!(key in values)) {
      order.push(key);
    }
    values[key] = val;
  }

  return { values, order };
}

function parseValue(raw: string): string {
  if (raw.length === 0) {
    return "";
  }
  if (raw[0] === '"') {
    return parseDoubleQuoted(raw.slice(1));
  }
  if (raw[0] === "'") {
    const endIdx = raw.indexOf("'", 1);
    return endIdx >= 0 ? raw.slice(1, endIdx) : raw.slice(1);
  }
  for (let i = 0; i < raw.length; i++) {
    if (raw[i] === "#" && i > 0 && (raw[i - 1] === " " || raw[i - 1] === "\t")) {
      return raw.slice(0, i).trim();
    }
  }
  return raw;
}

function parseDoubleQuoted(rest: string): string {
  let out = "";
  for (let i = 0; i < rest.length; i++) {
    const ch = rest[i];
    if (ch === '"') {
      return out;
    }
    if (ch === "\\" && i + 1 < rest.length) {
      i++;
      const next = rest[i];
      switch (next) {
        case "n":
          out += "\n";
          break;
        case "r":
          out += "\r";
          break;
        case "t":
          out += "\t";
          break;
        case '"':
          out += '"';
          break;
        case "\\":
          out += "\\";
          break;
        default:
          out += "\\" + (next ?? "");
          break;
      }
      continue;
    }
    out += ch ?? "";
  }
  return out;
}

export function extractPlaceholders(value: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  PLACEHOLDER_RE.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = PLACEHOLDER_RE.exec(value)) !== null) {
    const name = match[1];
    if (name && !seen.has(name)) {
      seen.add(name);
      out.push(name);
    }
  }
  return out;
}

export function hasPlaceholder(value: string): boolean {
  PLACEHOLDER_RE.lastIndex = 0;
  return PLACEHOLDER_RE.test(value);
}

export function replacePlaceholders(
  value: string,
  secrets: Readonly<Record<string, string>>,
): { value: string; missing: string[] } {
  const missingSet = new Set<string>();
  PLACEHOLDER_RE.lastIndex = 0;
  const replaced = value.replace(PLACEHOLDER_RE, (token, name: string) => {
    if (Object.prototype.hasOwnProperty.call(secrets, name)) {
      return secrets[name] ?? "";
    }
    missingSet.add(name);
    return token;
  });
  return { value: replaced, missing: [...missingSet].sort() };
}
