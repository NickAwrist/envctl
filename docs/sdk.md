# TypeScript SDK guide

The `@nickawrist/envctl` package lives in `sdk/`, targets Node 20+ (and Bun), ships dual ESM and CommonJS builds with TypeScript types, and has zero runtime dependencies.

## Install

```sh
pnpm add @nickawrist/envctl
```

Or pack a local tarball from this repository before publishing:

```sh
cd sdk && pnpm run build && npm pack
```

## Placeholder syntax

Put `%ENVCTL-<KEY>%` tokens in `.env` or in injected environment variables:

```dotenv
OPENROUTER_API_KEY=%ENVCTL-OPENROUTER_API_KEY%
DATABASE_URL=postgres://app:%ENVCTL-LOCAL_DB_PASS%@localhost:5432/mydb
PORT=3000
```

Inline interpolation and multiple placeholders in one value are supported.

## API

### `resolveEnv(options?)`

Reads `.env` (if present) and scans `baseEnv` (default `process.env`) for `%ENVCTL-...%` placeholders, fetches all referenced keys in one daemon call, and returns the resolved key-value map without mutating `process.env`.

```ts
import { resolveEnv } from "@nickawrist/envctl";

const env = await resolveEnv();
const apiKey = env.OPENROUTER_API_KEY;
```

Use `resolveEnv()` when you want to keep secrets out of `process.env` so child processes and unrelated libraries cannot read them.

### `loadEnv(options?)`

Calls `resolveEnv()` and writes the resolved entries into `process.env` (or `options.target`).

```ts
import { loadEnv } from "@nickawrist/envctl";

await loadEnv();
```

Options shared by `resolveEnv` and `loadEnv`:

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `path` | `string` | `".env"` | Path to the `.env` file |
| `optional` | `boolean` | `true` when `path` is omitted, `false` when explicit | Ignore missing `.env` files |
| `override` | `boolean` | `false` | Overwrite existing non-placeholder entries in `baseEnv` |
| `baseEnv` | `Record<string, string \| undefined>` | `process.env` | Source environment used for precedence and placeholder scanning |
| `socketPath` | `string` | Auto-detected | Override the daemon Unix socket path |
| `timeoutMs` | `number` | `5000` | Socket request timeout in milliseconds |

`loadEnv` also accepts `target` (default `process.env`) to populate a custom object instead of `process.env`.

### Preload hook for ESM

Static `import` statements run before top-level code in your entry file. To resolve `.env` before application modules read `process.env`, preload the register entrypoint:

```sh
node --import @nickawrist/envctl/register dist/index.js
tsx --import @nickawrist/envctl/register src/index.ts
```

### Direct lookups and status

```ts
import { getSecret, getSecrets, getStatus } from "@nickawrist/envctl";

const status = await getStatus();
const openrouterKey = await getSecret("OPENROUTER_API_KEY");
const secrets = await getSecrets(["OPENROUTER_API_KEY", "LOCAL_DB_PASS"]);
```

Pure parsing helpers (`parseDotenv`, `extractPlaceholders`, `hasPlaceholder`, `replacePlaceholders`, `resolveSocketPath`) are also exported.

## Precedence in production, CI, and systemd

1. If an environment variable is already set in `process.env` and does not contain `%ENVCTL-...%`, it wins unless `override: true` is passed.
2. If an environment variable in `process.env` does contain `%ENVCTL-...%` (for example from Bun's automatic `.env` loader or a systemd `EnvironmentFile`), `loadEnv()` resolves it via the daemon even when no `.env` file is on disk.
3. If zero unresolved placeholders remain after precedence checks, the SDK skips connecting to the daemon completely. Production deployments with real environment variables do not need `envctl` installed.

## Error handling

All errors extend `EnvctlError` (with a `.code` string property):

- `EnvctlLockedError` (`code: "LOCKED"`) when the daemon is not running or the vault is locked
- `EnvctlMissingKeysError` (`code: "MISSING_KEYS"`, `.missingKeys: readonly string[]`) when one or more referenced secrets do not exist in the vault
- `EnvctlProtocolError` on socket framing, ownership, or protocol version mismatches
