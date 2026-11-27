<img src="assets/icon.svg" width="56" height="56" alt="envctl" />

# envctl

Local encrypted environment variable manager for reusing secrets across projects on one machine.

Instead of copying the same OpenRouter or third-party API key into every repo's `.env`, store it once in an encrypted vault and reference it by placeholder:

```dotenv
OPENROUTER_API_KEY=%ENVCTL-OPENROUTER_API_KEY%
DATABASE_URL=postgres://app:%ENVCTL-LOCAL_DB_PASS%@localhost:5432/app
PORT=3000
```

At runtime, either run your command through the CLI (`envctl run -- pnpm dev`) or resolve values in TypeScript with `@nickawrist/envctl`:

```ts
import { loadEnv } from "@nickawrist/envctl";

await loadEnv();
```

In CI or production, real environment variables already set on the host take precedence. If no `%ENVCTL-...%` placeholders remain, the SDK never connects to `envctl`.

## How it works

`envctl` ships as a single Go binary (CLI plus background daemon) and a zero-dependency TypeScript SDK under `sdk/`.

- **Encrypted at rest.** Secrets live in `~/.local/share/envctl/vault.enc` (`0600`). Your master password derives a key with Argon2id that wraps a random 32-byte AES-256-GCM data encryption key (DEK). Nothing decrypted is written to disk.
- **In-memory unlock window.** Unlocking starts a detached daemon on a `0600` Unix domain socket verified with `SO_PEERCRED`. The daemon keeps only the 32-byte DEK between requests and decrypts secrets on demand. Other processes running as your user cannot read its memory, and it writes no core dump. It zeroes the key and exits when locked, when a fixed TTL expires, or when a watched shell session ends.
- **Thin TypeScript SDK.** `@nickawrist/envctl` parses `.env`, scans `process.env` for placeholders, and batches lookups over the Unix socket. It never reads `vault.enc` or handles passwords.

## Documentation

- [CLI guide](docs/cli.md) for installation, vault setup, unlock modes, and every command
- [TypeScript SDK guide](docs/sdk.md) for API usage, preload hooks, errors, and production precedence
- [Architecture and protocol](docs/architecture.md) for encryption details, daemon lifecycle, wire format, and security boundaries

## Development

```sh
go test -race ./...
cd sdk && pnpm test
```

MIT licensed. See [LICENSE](LICENSE).
