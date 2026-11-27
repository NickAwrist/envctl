# Architecture and protocol

## Storage layout

All files and sockets live inside `0700` directories with `0600` file permissions. `envctl` creates the directories when they are missing and refuses to use an existing directory that other users can access:

- `~/.config/envctl/config.json` stores `unlock_mode` and `unlock_ttl`
- `~/.local/share/envctl/vault.enc` stores the encrypted vault envelope
- `$XDG_RUNTIME_DIR/envctl/agent.sock` is the daemon Unix domain socket, falling back to `$TMPDIR/envctl-<uid>/agent.sock` or `/tmp/envctl-<uid>/agent.sock` to stay under Linux's 108-byte `sun_path` limit

## Encryption design

`envctl` uses a two-layer key hierarchy so password verification and secret encryption happen in a single authenticated step:

1. **Key derivation.** `Argon2id` (`time=3, memory=64 MiB, threads=4, keyLen=32`) derives a 256-bit Key Encryption Key (KEK) from the master password and a random 16-byte salt.
2. **Key wrapping.** `envctl init` generates a random 32-byte Data Encryption Key (DEK) and encrypts it with the KEK using `AES-256-GCM`, a 12-byte random nonce, and associated data `envctl:v1:dek`.
3. **Payload encryption.** The entire secrets JSON payload is encrypted with the DEK using `AES-256-GCM`, a fresh 12-byte nonce on every write, and associated data `envctl:v1:payload`.

When unlocking, deriving the KEK and decrypting the wrapped DEK verifies the password via the GCM authentication tag and yields the DEK in memory. Rotating the master password (`envctl passwd`) re-wraps the DEK with a new salt and KEK without re-encrypting the payload.

Every write to `vault.enc` goes to a `0600` temporary file in the same directory, calls `fsync`, and atomically replaces `vault.enc` via `rename`. Each read-modify-write cycle holds an exclusive `flock` on `vault.enc.lock`, so a secret written by the daemon and a password change made by `envctl passwd` cannot overwrite each other.

## Daemon lifecycle

`envctl` is a single Go binary. When `envctl unlock` runs and no daemon is listening, it spawns itself detached (`Setsid: true`) as `envctl __daemon`.

- **Minimal RAM exposure.** The daemon holds only the 32-byte DEK in memory, not a plaintext map of secrets. Each `get` or `list` reads and decrypts `vault.enc` on demand, extracts only the requested keys, and zeroes temporary byte buffers.
- **Auto-exit on lock.** On `envctl lock`, fixed TTL expiry, `SIGINT`/`SIGTERM`, or session exit, the daemon zeroes the DEK slice, removes the socket file, and exits so the OS reclaims its address space. If a newly started daemon receives no valid `unlock` within 15 seconds, it shuts down automatically.
- **Session mode.** `--session` monitors the login shell session leader PID (`Getsid(0)`) via `pidfd_open` and `poll`, locking immediately when the SSH or terminal session exits. Kernels without `pidfd_open` fall back to polling `kill(pid, 0)`.
- **Peer authentication.** Every accepted socket connection is verified with `SO_PEERCRED` to confirm the caller's UID matches the daemon's UID. Clients also `lstat` the socket before connecting to verify it is a real Unix socket owned by the current user.

## Wire protocol

Clients and the daemon exchange versioned (`"v": 1`) newline-delimited JSON objects over the Unix socket, one request and response per connection (max message size 16 MiB).

Request operations: `status`, `unlock`, `lock`, `get` (batched `keys: string[]`), `list`, `set`, and `delete`. The TypeScript SDK only issues `status` and `get`.

A request may carry `"vault": "<absolute path>"`. The daemon then rejects it with `INVALID_REQUEST` unless that is the vault it serves. The CLI sends it so that a command run with a different `ENVCTL_DATA_DIR` cannot read or write the wrong vault.

```json
{"v":1,"op":"get","keys":["OPENROUTER_API_KEY","LOCAL_DB_PASS"]}
```

```json
{"v":1,"ok":true,"values":{"OPENROUTER_API_KEY":"..."},"missing":["LOCAL_DB_PASS"]}
```

## Threat model

`envctl` protects against leaking raw secrets into project `.env` files, git repositories, backups, and unencrypted files at rest on disk.

Once unlocked, any process running under your same OS user can query the daemon until it locks (the same boundary as `ssh-agent`). Use `resolveEnv()` when you want to avoid placing secrets in `process.env`, keep `unlock_ttl` short or use `--session` when appropriate, and run `envctl lock` when done.
