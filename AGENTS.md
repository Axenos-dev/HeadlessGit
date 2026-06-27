# AGENTS.md

## Project

`headlessgit` is a headless Git server for applications that need Git hosting primitives without a forge UI.

It provides:

- Git over SSH for clone, fetch, and push.
- Git over HTTP for clone, fetch, and push, plus the HTTP surface Git LFS requires.
- A control API for repositories, users, SSH keys, permissions, and webhooks.
- Git LFS support for large binary files.
- Bare repository storage on a filesystem.
- Push events/webhooks for downstream systems.

Both SSH and HTTP transports are core. They are not phased: Git LFS speaks HTTP for
object transfer, so an HTTP server exists from v1 regardless.

It is not a GitHub/Gitea/Forgejo clone.

Im not interested in issues, pull requests, wikis, actions, stars, social profiles, repository browsing UI, or package registry features unless explicitly requested.

## Design rules

### Git transport

Do not reimplement Git protocol internals unless explicitly requested.

Use the system Git binary for Git transport:

- `git-upload-pack` for clone/fetch (SSH and HTTP).
- `git-receive-pack` for push (SSH and HTTP).
- `git-http-backend` for the Git-over-HTTP smart protocol.

The server is responsible for:

1. Authenticating the caller.
2. Resolving the requested repo.
3. Checking permissions.
4. Starting the correct Git subprocess.
5. Piping bytes between the client connection and the Git subprocess.
6. Emitting events after successful writes.

### Repo path safety

Never map user input directly to filesystem paths.

Do this:

```txt
namespace/name.git
  -> database lookup
  -> repo_id
  -> controlled storage path
```

Do not do this:

```txt
repo_path = storage_root + "/" + user_input
```

All repo paths must be created by trusted code and stored as internal metadata.

### Storage

Bare Git repos live on a filesystem-backed storage root.

Example:

```txt
/var/lib/headlessgit/repos/repo_123.git
```

Object storage may be used for LFS objects, backups, etc etc.

### LFS

Git LFS is first-class.

LFS object transfer happens over HTTP (the LFS Batch API), never over SSH. When a
client reaches a repo over SSH, the server answers `git-lfs-authenticate` with the
HTTP LFS endpoint and a short-lived token; the actual upload/download then goes to the
HTTP server. This is why HTTP is a core transport rather than a later add-on.

LFS authorization must use the same repo permission model as normal Git access:
read grants LFS download, write grants LFS upload.

LFS object storage should be behind a storage interface so local disk works first and S3 can be added later.

### Security

Security-sensitive code must prefer boring, explicit logic over clever abstractions.

Always:

- Check permissions before running Git subprocesses.
- Execute subprocesses with argv arrays, not shell strings.
- Validate requested Git commands strictly.
- Sign outbound webhooks.
- Use constant-time comparison for webhook secrets and tokens where relevant.
- Add timeouts to subprocesses and external calls.
- Treat repo names, refs, paths, webhook URLs, SSH commands, and LFS object IDs as untrusted input.

Never:

- Pass raw user input to shell commands.
- Allow path traversal.
- Trust clone URLs as storage paths.
- Let LFS upload/download bypass repo permissions.
- Add broad filesystem access to Git subprocesses.

## Permission model

Keep the permission model small.

Initial roles:

```txt
read   = clone/fetch and LFS download
write  = read + push and LFS upload
admin  = write + repo settings/webhooks/permissions
```

No organizations, teams, nested groups, or complex inheritance until needed.

If a caller has access through another product, that product should translate its access model into explicit repo permissions here.

Prefer stable, small request/response types. Avoid leaking internal storage paths in API responses.

## Identity and authentication

There are three callers, each with a distinct auth mechanism:

```txt
Git/LFS over SSH   -> SSH public key  -> resolves to user or service account
Git/LFS over HTTP  -> bearer token    -> resolves to user or service account
Control API        -> bearer token    -> resolves to user or service account
```

Notes:

- A user owns one or more SSH public keys and zero or more API/access tokens.
- A service account is a non-human identity (another product, a CI system) that
  authenticates the same way and is subject to the same repo permissions.
- LFS-over-SSH bootstraps into an HTTP bearer token via `git-lfs-authenticate`
  (see the LFS section), so HTTP token auth is required even for SSH-first users.
- Tokens are stored hashed; compare with constant-time comparison.

## SSH Git server

The SSH server should:

1. Authenticate public keys.
2. Resolve the key to a user or service account.
3. Accept only Git commands:
   - `git-upload-pack`
   - `git-receive-pack`

4. Parse the requested repo path.
5. Resolve it through repository metadata.
6. Check read/write permissions.
7. Start the Git subprocess.
8. Pipe stdin/stdout/stderr safely.
9. Record audit information.

Reject unknown SSH commands.

Implement this as a custom in-process SSH server (built on a maintained SSH library)
rather than relying on a system `sshd` + `authorized_keys` forced-command setup. The
server parses the requested command itself and only ever execs an allowlisted Git
subprocess. Never hand the client an interactive shell or pty.

## HTTP stack

Use Go's standard `net/http` with the `chi` router for all HTTP, shared by both the
Control API and the Git/LFS HTTP server. Do not use a `fasthttp`-based framework
(e.g. Fiber): the Git smart HTTP path streams request/response bodies to and from the
`git-http-backend` CGI subprocess, and that integration is built around `net/http`.
One `net/http` stack keeps middleware, auth, and zap-based audit logging consistent.

The three surfaces differ in shape, not transport:

```txt
Control API   HTTP + JSON     REST handlers (create/configure repos, users, perms, webhooks)
HTTP Git/LFS  HTTP + binary   stream to git-http-backend; LFS Batch API (JSON) + object transfer
SSH Git       SSH + binary    authenticated channel piped to the git subprocess (not an HTTP API)
```

The Control API and Git/LFS share one `chi.Router` on a single listener, but live in
separate route groups with separate middleware chains:

- Keep only universally-safe middleware global: `RequestID`, `RealIP`, `Recoverer`,
  and a zap-based request/audit logger.
- The Control API group adds JSON/content-type handling, body-size limits, and the
  management-token auth.
- The Git/LFS group must stay streaming-safe: no body-size limits and no middleware
  that buffers the request/response body, since it pipes to `git-http-backend` and
  streams LFS objects. It uses repo-permission auth, not management-token auth.

Use a zap-based request-logging middleware, not chi's stdlib `middleware.Logger`, so
HTTP audit logs match the structured field set in the Logging section.

## HTTP Git and LFS server

The HTTP server exposes:

1. The Git smart HTTP endpoints (`info/refs`, `git-upload-pack`, `git-receive-pack`)
   backed by `git-http-backend`.
2. The LFS Batch API and object transfer endpoints.

It applies the same flow as SSH: authenticate the bearer token, resolve the repo
through metadata, check permissions, then run the Git subprocess or stream the LFS
object. Reject any path that does not resolve to a known repo.

## Webhooks

Push webhooks should be emitted only after a successful push.

Webhook payloads should include at least:

```txt
event
repo_id
ref
old_sha
new_sha
pusher_id
```

Sign webhook deliveries with a per-webhook secret.

Webhook delivery must not run inline in the Git push path, since a slow or failing
endpoint would slow down or break pushes. For the thin v1, an in-process background
queue with bounded retries is enough; do not pull in an external job system until
delivery volume actually demands it.

## Database

Use SQLite for metadata in v1: it is embedded, zero-ops, and matches the thin goal.

Keep all metadata access behind a small storage interface so the backend can be
swapped for Postgres later without touching transport, auth, or permission code.
Do not adopt Postgres until a task explicitly requires it (e.g. multi-node deployment).

## Logging

Use `zap` for structured logging.

Wrap it behind a small `Logger` interface at the module boundary so transport, auth,
and permission code log against our own type, not zap directly. This keeps the swap
point open and keeps the dependency from leaking through the whole codebase.

Audit logging is a first-class use of the logger. Every transport request should log a
consistent, structured set of fields:

```txt
request_id    correlation id, threaded via context
identity_id   resolved user or service account
transport     ssh | http
repo_id       resolved repo (never the raw storage path)
git_command   git-upload-pack | git-receive-pack | lfs-batch | ...
result        ok | denied | error
duration      request duration
```

Never log secrets: tokens, webhook secrets, SSH private material, or full auth
headers. Redact before logging.

## Coding style

Prefer simple Go.

Use:

- Small interfaces at module boundaries.
- Context-aware functions for I/O and subprocess work.
- Explicit errors with useful context.
- Table-driven tests where they make behavior clearer.
- Standard library packages unless a dependency clearly improves the design.

Avoid:

- Large global state.
- Framework-heavy abstractions.
- Magic behavior hidden behind generic helpers.
- Adding dependencies for small tasks.
- Premature distributed systems design.

### Dependencies and interfaces

The package that actually invokes a dependency defines a minimal consumer
interface for it (e.g. `gitssh` declares `Authenticator`/`TokenMinter`, the git
and control handlers declare their own small interfaces). Composition and router
layers (`server`, `git`, `githttp`, the control router) hold concrete types and
forward them down — they don't redeclare interfaces for things they only pass
through. So a monolithic package that is both router and direct consumer (like
`gitssh`) defines interfaces, while a router that delegates to sub-handlers (like
`githttp`) stays concrete; that asymmetry is intentional.

Constructors take a single `Services` struct rather than a long positional
parameter list. `main` builds the concrete dependencies once and passes them in.

## Dependency policy

Do not add a new production dependency without a clear reason.

Acceptable dependency areas:

- SSH server support (a maintained SSH library; do not roll your own).
- HTTP router/middleware (`chi` on top of `net/http`; not a `fasthttp` framework).
- SQLite driver/query tooling (Postgres driver only once Postgres is actually needed).
- Structured logging.
- Config loading.
- Object storage clients.

If adding a dependency, prefer maintained, boring, widely used packages.

## Mental model

`headlessgit` is a secure gateway around Git, not a replacement for Git.

```txt
Git client
  -> SSH/HTTP transport
  -> headlessgit auth and permissions
  -> system Git subprocess
  -> bare repo filesystem storage
```

For large files:

```txt
git-lfs client
  -> headlessgit LFS API
  -> LFS authorization
  -> local/S3 object storage
```
