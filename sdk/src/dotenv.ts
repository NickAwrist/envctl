const PLACEHOLDER_RE = /%ENVCTL-([A-Za-z0-9_][A-Za-z0-9_./-]*)%/g;

export interface ParsedDotenv {
  values: Record<string, string>;
  order: string[];
}

export function parseDotenv(content: string): ParsedDotenv {
  const values: Record<string, string> = {};
  const order: string[] = [];

  let rest = content.replace(/^\uFEFF/, "").replaceAll("\r\n", "\n");
  while (rest !== "") {
    let line: string;
    [line, rest] = cutLine(rest);
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
    let rawVal = trimmed.slice(eqIdx + 1);

    // A quoted value may span lines. Without a closing quote it ends on this line.
    if (closingQuote(rawVal.trimStart()) === UNTERMINATED) {
      const tail = `${line.slice(line.indexOf("=") + 1).trimStart()}\n${rest}`;
      const end = closingQuote(tail);
      if (end >= 0) {
        rawVal = tail.slice(0, end + 1);
        [, rest] = cutLine(tail.slice(end + 1));
      }
    }
    const val = parseValue(rawVal);

    if (!(key in values)) {
      order.push(key);
    }
    values[key] = val;
  }

  return { values, order };
}

function cutLine(text: string): [line: string, rest: string] {
  const idx = text.indexOf("\n");
  return idx < 0 ? [text, ""] : [text.slice(0, idx), text.slice(idx + 1)];
}

const NOT_QUOTED = -1;
const UNTERMINATED = -2;

/**
 * Returns the index of the quote that closes a value starting with ' or ",
 * NOT_QUOTED if the value is not quoted, or UNTERMINATED.
 */
function closingQuote(val: string): number {
  const quote = val[0];
  if (quote !== '"' && quote !== "'") {
    return NOT_QUOTED;
  }
  for (let i = 1; i < val.length; i++) {
    if (quote === '"' && val[i] === "\\") {
      i++;
    } else if (val[i] === quote) {
      return i;
    }
  }
  return UNTERMINATED;
}

/**
 * Parses the text after "=", which keeps its leading whitespace so that a "#"
 * right after it ("KEY= # note") is recognized as a comment.
 */
function parseValue(raw: string): string {
  const val = raw.trim();
  if (val.length === 0) {
    return "";
  }
  if (val[0] === '"') {
    return parseDoubleQuoted(val.slice(1));
  }
  if (val[0] === "'") {
    const endIdx = val.indexOf("'", 1);
    return endIdx >= 0 ? val.slice(1, endIdx) : val.slice(1);
  }
  for (let i = 0; i < raw.length; i++) {
    if (raw[i] === "#" && i > 0 && (raw[i - 1] === " " || raw[i - 1] === "\t")) {
      return raw.slice(0, i).trim();
    }
  }
  return val;
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
