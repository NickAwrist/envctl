# envctl

Local encrypted environment variable manager for reusing secrets across projects on one machine.

- **Go CLI & daemon (`envctl`)**: Encrypts secrets with Argon2id + AES-256-GCM (wrapped DEK). The background daemon holds only the 32-byte DEK in RAM and serves requests over a `0600` Unix domain socket verified with `SO_PEERCRED`.
- **TypeScript SDK (`sdk/`, `@nickawrist/envctl`)**: Zero-dependency npm package that parses `.env`, replaces `%ENVCTL-KEY%` placeholders via the running daemon, and stays out of the way in CI/production when real env vars are already set.

## Install CLI

```bash
go build -o ~/.local/bin/envctl ./cmd/envctl
```

## Quick start

```bash
# 1. Create the encrypted vault (~/.local/share/envctl/vault.enc)
envctl init

# 2. Configure default unlock behavior (ttl, session, or until_lock)
envctl config set unlock_mode until_lock
envctl config set unlock_ttl 30m

# 3. Unlock the vault
envctl unlock                 # uses config default
envctl unlock --ttl 4h        # fixed 4-hour window (no extending on read)
envctl unlock --session       # unlocked until current SSH/shell session exits
envctl unlock --until-lock    # unlocked in RAM until 'envctl lock' or reboot
envctl unlock --password-file /path/to/secret

# 4. Store secrets (prompts with hidden input, or pipe with --stdin)
envctl set OPENROUTER_API_KEY --description "Shared OpenRouter key"
```

Reference stored secrets in any project's `.env`:

```dotenv
OPENROUTER_API_KEY=%ENVCTL-OPENROUTER_API_KEY%
DATABASE_URL=postgres://app:%ENVCTL-LOCAL_DB_PASS%@localhost:5432/mydb
PORT=3000
```

Run any tool with resolved env vars in memory:

```bash
envctl check
envctl run -- pnpm dev
```

## TypeScript SDK

See [sdk/README.md](./sdk/README.md).
