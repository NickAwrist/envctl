# CLI guide

## Requirements

- Linux
- Go 1.26.3+ to build from source

## Install

```sh
go build -o ~/.local/bin/envctl ./cmd/envctl
```

Check that `~/.local/bin` is on your `PATH`:

```sh
envctl version
```

Every command prints its arguments and flags with `--help` (or `envctl help <command>`):

```sh
envctl set --help
```

## First-time setup

Create the encrypted vault and default config file:

```sh
envctl init
```

This creates `~/.local/share/envctl/vault.enc` (`0600`) and `~/.config/envctl/config.json` (`0600`). You can also pass `--password-file <path>` or `--stdin` for non-interactive setup.

## Unlock modes

`envctl unlock` starts the background daemon and holds the decrypted data encryption key in memory until the chosen lock condition triggers.

```sh
envctl unlock                 # uses unlock_mode and unlock_ttl from config.json
envctl unlock --ttl 30m       # fixed 30-minute window (reads do not extend it)
envctl unlock --session       # unlocked until the current shell or SSH session exits
envctl unlock --until-lock    # unlocked until 'envctl lock' or machine reboot
```

To unlock without an interactive TTY prompt (for example from a secret file or script):

```sh
envctl unlock --password-file /path/to/secret
cat /path/to/secret | envctl unlock --stdin
```

Change the default mode or fixed TTL in `~/.config/envctl/config.json`:

```sh
envctl config
envctl config set unlock_mode until_lock
envctl config set unlock_ttl 4h
envctl config get unlock_mode
```

Valid `unlock_mode` values are `ttl`, `session`, and `until_lock`. `unlock_ttl` accepts any positive Go duration string such as `15m`, `2h`, or `12h`.

## Managing secrets

Secret key names must start with a letter, digit, or underscore and use 1 to 128 characters from `[A-Za-z0-9_./-]`.

```sh
# Prompt for the value with hidden input (keeps secrets out of shell history)
envctl set OPENROUTER_API_KEY -d "Shared OpenRouter key"

# Pipe a value from stdin or read from a file
cat key.txt | envctl set OPENROUTER_API_KEY
envctl set OPENROUTER_API_KEY --value-file ./key.txt

# Read a single secret (raw when piped, newline on TTY)
envctl get OPENROUTER_API_KEY

# List keys, last updated timestamps, and descriptions (never prints values)
envctl list

# Delete a secret
envctl rm OPENROUTER_API_KEY
```

Updating an existing key without passing `-d` / `--description` keeps its previous description.

When stdin is not a terminal, `envctl set` reads all of it as the value, so multi-line secrets such as PEM keys are stored whole. `--stdin` forces the same behavior on a terminal. One trailing newline is stripped. An empty value is rejected and leaves any existing secret unchanged. The value is never read from the command line: `envctl set KEY=VALUE` fails without printing the argument.

## Working with `.env` files

Reference secrets anywhere in a `.env` file using `%ENVCTL-<KEY>%`, either as a full value or inline inside a larger string:

```dotenv
OPENROUTER_API_KEY=%ENVCTL-OPENROUTER_API_KEY%
DATABASE_URL=postgres://app:%ENVCTL-LOCAL_DB_PASS%@localhost:5432/mydb
PORT=3000
```

Quoted values may span several lines. Text after ` #` is a comment, so `KEY= # note` is an empty value.

Verify that every referenced secret exists in the vault without printing any values:

```sh
envctl check
envctl check --env-file .env.local
```

`envctl check` also fails on references that would be passed through unresolved, such as `%ENVCTL-KEY` without the closing `%` or a lowercase `%envctl-key%`.

Run any command with resolved environment variables injected in memory (never writing plaintext secrets to disk):

```sh
envctl run -- pnpm dev
envctl run --env-file .env.local --override -- bun run src/index.ts
```

If the vault is locked when you run `envctl run` in an interactive terminal, it prompts for your master password inline, unlocks the daemon, and launches the command.

`envctl run` also resolves `%ENVCTL-<KEY>%` references in variables that are already in its environment (for example from a systemd `EnvironmentFile` or Docker Compose), the same way the SDK does. A variable already set in the environment wins over the `.env` file unless `--override` is passed.

Once the variables are resolved, `envctl run` replaces itself with the command. Signals such as Ctrl-C reach the command directly, and the command's exit status is the one your shell sees. If the command is not found `envctl run` exits 127, and 126 if it cannot be executed.

## Locking, status, and rotating passwords

```sh
envctl status   # lock state, active mode, expiry or watched PID, vault and socket paths
envctl lock     # zeroes the key in memory and stops the daemon
envctl passwd   # changes the master password and re-encrypts the vault with a new DEK
```

`envctl passwd` also accepts `--old-password-file` and `--new-password-file`. It locks the vault when it finishes, so unlock again with the new password.

## Environment variables

| Variable | Effect |
| --- | --- |
| `ENVCTL_CONFIG_DIR` | Overrides the config directory (default `$XDG_CONFIG_HOME/envctl` or `~/.config/envctl`) |
| `ENVCTL_DATA_DIR` | Overrides the vault directory (default `$XDG_DATA_HOME/envctl` or `~/.local/share/envctl`) |
| `ENVCTL_SOCKET` | Overrides the daemon Unix socket path |
| `NO_COLOR` | Disables ANSI colors on terminal output |

`envctl` creates these directories with mode `0700` when they do not exist. It never changes an existing directory: one that other users can access is rejected, and a socket path that points at an existing non-socket file is refused. A daemon serves one vault, so use a separate `ENVCTL_SOCKET` for each `ENVCTL_DATA_DIR`. Commands fail if the daemon on the socket serves a different vault.
