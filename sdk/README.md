# @nickawrist/envctl

Zero-dependency TypeScript SDK for resolving `%ENVCTL-KEY%` placeholders in `.env` files through the local `envctl` daemon.

## Usage

Put placeholders in `.env` instead of raw API keys:

```dotenv
OPENROUTER_API_KEY=%ENVCTL-OPENROUTER_API_KEY%
DATABASE_URL=postgres://app:%ENVCTL-LOCAL_DB_PASS%@localhost:5432/mydb
PORT=3000
```

### Scoped resolution (does not write to `process.env`)

```ts
import { resolveEnv, getSecret } from "@nickawrist/envctl";

const env = await resolveEnv();
const apiKey = await getSecret("OPENROUTER_API_KEY");
```

### Populate `process.env`

```ts
import { loadEnv } from "@nickawrist/envctl";

await loadEnv();
```

Or preload before static imports run:

```bash
node --import @nickawrist/envctl/register dist/index.js
```

## Precedence in production / CI

Existing environment variables in `process.env` that do not contain `%ENVCTL-...%` take precedence unless `override: true` is passed. If no placeholders need resolution, the SDK does not connect to the `envctl` daemon at all.
