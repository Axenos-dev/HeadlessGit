# AGENTS.md

## Project

`headlessgit` is a headless Git server: Git hosting primitives (transport, auth,
permissions, storage) for products that need to host repositories without a forge
UI. It is a secure gateway around the system `git` binary, not a GitHub/Gitea clone.

It provides:

- Git over SSH and HTTP (clone / fetch / push).
- Git LFS (object transfer over HTTP; disk or S3 storage).
- A control API to manage repositories, users, SSH keys, tokens, permissions, and webhooks.
- Repo APIs on the control plane: contents listing, blob read/upload, streamed
  zip/tar.gz archives (optional LFS smudging), and commit creation on bare repos
  (blobs + commits, CAS ref updates, `.gitattributes`-driven LFS cleaning) — so
  products never need local clones.
- A `read` / `write` / `admin` permission model enforced before every Git operation.
- Push webhooks, signed and delivered off the push path.

Out of scope unless explicitly requested: issues, pull requests, wikis, CI/actions,
stars, social profiles, a repo-browsing UI, package registries.

## Commands

```sh
./dev.sh test   # build + vet + test — the CI gate (go build/vet/test ./...)
./dev.sh up     # build and run the stack (docker compose up --build)
./dev.sh gen    # regenerate sqlc code after editing internal/db/queries/*.sql
```

- Run one package: `go test ./internal/server/git/gitssh`.
- The Git e2e tests skip when `git` / `git-lfs` are not on `PATH`; the S3/LFS
  tests skip unless `LFS_S3_*` env vars point at a bucket. `./dev.sh test` is green
  either way.
- Local config is via env vars — copy `.env.example` and set at least `DATABASE_URL`.

## Architecture

A request is authenticated, the repo is resolved through metadata, permissions are
checked, then bytes are piped to a system `git` subprocess (or an LFS object is
streamed). The server never reimplements the Git protocol.

```txt
client -> SSH/HTTP transport -> auth + permissions -> system git subprocess -> bare repo on disk
git-lfs -> HTTP LFS API       -> auth + permissions -> local/S3 object storage
```

Package map:

| Path                          | Purpose                                                                                                           |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `cmd/app`                     | Entrypoint: build deps, seed admin, wire `server`, handle signals.                                                |
| `internal/config`             | Env-based configuration.                                                                                          |
| `internal/db`                 | SQLite open/migrate; sqlc queries live in `gen/`, SQL in `queries/`, migrations in `migrations/`.                 |
| `internal/domain`             | Core types (repository, account, role, token, ssh key, lfs).                                                      |
| `internal/services/*`         | Business logic per area (repositories, users, auth, permissions, lfs); each has a service + a registry over `db`. |
| `internal/storage`            | LFS object storage behind an interface (`disk`, `s3`).                                                            |
| `internal/archive`            | Pure mechanism: streaming tar re-encode to zip/tar.gz with an injected LFS smudge callback.                       |
| `internal/gitbackend`         | Git subprocesses behind a small interface: pack protocol, read ops (ls-tree, blobs, archive), commit creation.    |
| `internal/server`             | Composition root: wires control + git servers, runs and shuts down listeners.                                     |
| `internal/server/control`     | Control API (REST); sub-handlers in `repositories/`, `users/`, `permissions/`.                                    |
| `internal/server/git/gitssh`  | Git-over-SSH transport (custom in-process SSH server).                                                            |
| `internal/server/git/githttp` | Git-over-HTTP transport; `smart/` (smart-HTTP framing over the git backend) and `lfs/` (Batch API).               |
| `internal/server/audit`       | Request-scoped audit event + HTTP middleware.                                                                     |
| `internal/server/response`    | JSON envelope + error helpers for the control API.                                                                |

## Core rules

### Git transport

Use the system Git binary; do not reimplement protocol internals:
`git-upload-pack` (fetch), `git-receive-pack` (push). Smart HTTP runs those same
helpers with `--stateless-rpc`/`--advertise-refs` and frames the pkt-lines
in-process (no `git-http-backend`/CGI), so both transports share one backend.

### Repo path safety

Never map user input to a filesystem path. Always go
`namespace/name.git -> database lookup -> repo_id -> controlled storage path`.
Repo paths are created by trusted code and stored as internal metadata; never build
a path by concatenating user input onto the storage root.

### Permission model

Keep it small: `read` = clone/fetch + LFS download; `write` = read + push + LFS
upload; `admin` = write + repo settings/permissions/webhooks. No organizations,
teams, nested groups, or inheritance until actually needed. LFS authorization uses
the same model as normal Git access. A caller's access from another product is
translated into explicit repo permissions here.

### Security

Security-sensitive code prefers boring, explicit logic over clever abstractions.

Always:

- Check permissions before running Git subprocesses.
- Execute subprocesses with argv arrays, not shell strings.
- Validate requested Git commands strictly (allowlist).
- Sign outbound webhooks; use constant-time comparison for secrets and tokens.
- Add timeouts to subprocesses and external calls.
- Treat repo names, refs, paths, webhook URLs, SSH commands, and LFS object IDs as untrusted.
- Store tokens hashed.

Never:

- Pass raw user input to shell commands.
- Allow path traversal.
- Trust clone URLs as storage paths.
- Let LFS upload/download bypass repo permissions.
- Give Git subprocesses broad filesystem access.
- Hand an SSH client an interactive shell or pty.

## Conventions

Prefer simple Go: small interfaces at module boundaries, context-aware I/O, explicit
errors with context, table-driven tests, standard library first. Avoid large global
state, framework-heavy abstractions, and dependencies added for small tasks.

**File layout.** Do not multiply files or packages. A service is `service.go` +
`errors.go` + `registry.go` — new service methods go into `service.go`, not new
files. One test file per package where practical (`service_test.go`,
`handlers_test.go`). Small parsing/vocabulary helpers (formats, pointers, modes)
belong in `domain` next to the types they produce, not in new packages.

**Dependencies & interfaces.** The package that _invokes_ a dependency defines a
minimal consumer interface for it (e.g. `gitssh`'s `Authenticator`/`TokenMinter`,
the handler packages' own interfaces). Composition/router layers (`server`, `git`,
`githttp`, the control router) hold concrete types and forward them down. So a
monolithic router+consumer like `gitssh` defines interfaces while a delegating
router like `githttp` stays concrete — that asymmetry is intentional. Constructors
take a single `Services` struct, not a long positional list; `main` builds the
concretes once.

**HTTP.** One `net/http` + `chi` stack (no `fasthttp`/Fiber: the smart-HTTP path
streams bodies to/from the `git-upload-pack`/`git-receive-pack` subprocesses). The
Git/LFS routes must stay streaming-safe — no body-size limits or body-buffering
middleware; the same applies to the control API's archive route, which streams
`git archive` output. Health (`/healthz`) is unauthenticated and sits outside the
audit chain.

**Logging.** `zap` structured logging. Never log secrets (tokens, webhook secrets,
SSH key material, full auth headers). Each transport request emits one audit line:

```txt
request_id  identity_id  transport(ssh|http)  repo_id  git_command  result(ok|denied|error)  duration
```

## Dependency policy

Add a production dependency only with a clear reason; prefer maintained, boring,
widely-used packages. Acceptable areas: SSH server library, HTTP router/middleware
(`chi`), SQLite driver/query tooling, structured logging, config loading, object
storage clients. Do not roll your own SSH or Git protocol implementation.

## Webhooks

- Emitted only **after** a ref actually moved, off the push path: receive-pack
  runs and `gitbackend` diffs the repo's refs before/after (transports dispatch),
  or an api commit lands its CAS ref update (repositories service dispatches).
  Both produce identical push events. Delivery is an in-process bounded queue
  with worker goroutines and retries — no external job system.
- One delivery per changed ref. Payload is self-describing: `event`, `ref`,
  `before`/`after`, `created`/`deleted`, a `repository` object (`id`, `name`,
  `full_name`), a `pusher` object (`id`, `username`), and `timestamp`. Creates/
  deletes use the all-zero SHA for the missing side.
- Each delivery is signed with the per-webhook secret: `X-HeadlessGit-Signature:
  sha256=<hmac>` over the raw body. The secret is generated server-side and
  returned once at registration; it is stored recoverably (needed to sign), unlike
  hashed tokens.
- Registered per repo via the control API; webhook detection lives in `gitbackend`
  (refs), the service in `internal/services/webhooks`, dispatch at the receive-pack
  call sites.
