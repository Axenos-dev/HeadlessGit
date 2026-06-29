# HeadlessGit

A headless Git server for platforms and internal tools.

Provides Git hosting _primitives_ (git over SSH/HTTP, authentication, permissions, storage) and pluggable storage.

Basically, this is a Git layer of infrastructure you'd put _underneath_ a project. This service is not responsible for billing and the UI, etc. It just handles the actual `git` transport, enforces access, and stores the bare repositories.

## What it is

- Basic Git over **SSH** and **HTTP** for clone / fetch / push.
- **Git LFS** for large files, with object storage on local disk or any S3-compatible bucket (AWS S3, Cloudflare R2, MinIO).
- A small **control API**, RESTful api to manage repositories, users, SSH keys, tokens, and permissions.
- Simple **permission model** (`read` / `write` / `admin`) enforced before every Git operation.
- **Push webhooks** — signed deliveries on every successful push.
- Bare-repository storage on a filesystem, with SQLite for metadata.

## Example

Start the server using `headlessgit` image (it already bundles `git`):

```sh
docker run --rm \
  -p 4000:4000 -p 4001:4001 -p 2222:2222 \
  -v "$PWD/data:/data" \
  -e DATABASE_URL=/data/headlessgit.db \
  -e ADMIN_TOKEN="$(openssl rand -hex 32)" \
  ghcr.io/axenos-dev/headlessgit:latest
```

That brings up three listeners - Git over HTTP (`4000`) and SSH (`2222`) for clients, and the control API (`4001`) for your backend. `ADMIN_TOKEN` seeds an admin service account on boot — your backend uses it as a bearer token to provision accounts, repositories, and permissions through the [control API](#control-api).

Once a user has a repository and credentials, they can use it like any other Git remote — the path is always `<owner-username>/<repo-name>.git`:

```sh
# over SSH, authenticated by a registered public key
git clone ssh://localhost:2222/username/api.git

# over HTTP, authenticated by a token
git clone http://x:<token>@localhost:4000/username/api.git
```

For local development, see [`./dev.sh up`](#development).

## Integration model

![diagram](images/diagram.png)

- **Backend** using the admin token calls the control API to create accounts, register credentials, create repositories, and grant permissions - translating its own users into explicit repo grants here.
- **Users** use the data plane directly with their own credentials (SSH key or token). Service authenticates them and authorizes each operation against the permissions they have.

### Identities

An account is either a `user` (a human with a Git client) or a `service` (a machine — backend). They authenticate identically and are authorized by the same per-repo permissions. The seeded `ADMIN_TOKEN` account is an admin service account — typically your application's backend, which uses it to provision accounts, repositories, and permissions.

### Recommended deployment

- Keep the **control API on an internal interface** — it's the privileged plane. The data-plane ports are the ones you expose to clients.
- Treat `ADMIN_TOKEN` as a secret. Rotate it by changing the env value and restarting the service (to reseed the admin account).
- Persist `/data` (bare repos, the SQLite file, and the SSH host key all live there).

## Configuration

All configuration is via environment variables.

| Variable            | Default                 | Description                                                                               |
| ------------------- | ----------------------- | ----------------------------------------------------------------------------------------- |
| `DATABASE_URL`      | _(required)_            | SQLite file path, e.g. `data/headlessgit.db`.                                             |
| `AUTO_MIGRATE`      | `true`                  | Run migrations on startup.                                                                |
| `ENVIRONMENT`       | `DEVELOPMENT`           | `DEVELOPMENT` or `PRODUCTION`.                                                            |
| `CONTROL_PORT`      | `4001`                  | Control API listener.                                                                     |
| `GIT_HTTP_PORT`     | `4000`                  | Git-over-HTTP listener.                                                                   |
| `GIT_SSH_PORT`      | `2222`                  | Git-over-SSH listener.                                                                    |
| `REPO_ROOT`         | `data/repos`            | Where bare repositories are stored.                                                       |
| `SSH_HOST_KEY_PATH` | `data/ssh/host_ed25519` | SSH host key file (generated on first boot if absent).                                    |
| `ADMIN_TOKEN`       | _(empty)_               | Raw token for the seeded admin account. Only its hash is stored. Empty = no admin seeded. |

See [`.env.example`](.env.example).

## Git LFS

Git LFS is enabled if `LFS_ENABLED=true` set in environment. Clients then use it transparently over both HTTP and SSH, and nothing beyond the usual `git lfs track`.

**Storage** sits behind an interface, separate from the bare repos. It can be one of those:

- `disk` (default) — objects stored locally on disk under `LFS_ROOT`.
- `s3` — any S3-compatible bucket (AWS S3, Cloudflare R2, MinIO). Transfers use **presigned URLs**, so object bytes flow directly between the client and the bucket instead of streaming through the server.

| Variable                   | Default                 | Description                                                                           |
| -------------------------- | ----------------------- | ------------------------------------------------------------------------------------- |
| `LFS_ENABLED`              | `false`                 | Enable Git LFS.                                                                       |
| `LFS_STORAGE_TYPE`         | `disk`                  | `disk` or `s3`.                                                                       |
| `LFS_PUBLIC_URL`           | _(required if enabled)_ | Externally-reachable base URL of the Git HTTP server, e.g. `https://git.example.com`. |
| `LFS_ROOT`                 | `data/lfs`              | Object directory when `LFS_STORAGE_TYPE=disk`.                                        |
| `LFS_S3_BUCKET`            | _(required for s3)_     | Bucket name.                                                                          |
| `LFS_S3_ENDPOINT`          | _(required for s3)_     | Host without scheme, e.g. `<account>.r2.cloudflarestorage.com`.                       |
| `LFS_S3_ACCESS_KEY_ID`     | _(required for s3)_     | Access key ID.                                                                        |
| `LFS_S3_SECRET_ACCESS_KEY` | _(required for s3)_     | Secret access key.                                                                    |
| `LFS_S3_REGION`            | _(empty)_               | Region; use `auto` for Cloudflare R2.                                                 |
| `LFS_S3_USE_SSL`           | `true`                  | Reach the endpoint over HTTPS.                                                        |
| `LFS_S3_USE_PATH_STYLE`    | `false`                 | Force path-style addressing (needed by some S3-compatible providers).                 |
| `LFS_S3_KEY_PREFIX`        | _(empty)_               | Optional prefix prepended to every object key.                                        |

## Control API

Every request requires `Authorization: Bearer <ADMIN_TOKEN>`. Responses are enveloped: `{"data": ...}` on success, `{"error": {"code", "message"}}` on failure.

**Accounts & credentials**

| Method   | Path                           | Body                 | Description                                                  |
| -------- | ------------------------------ | -------------------- | ------------------------------------------------------------ |
| `POST`   | `/users`                       | `{username, kind}`   | Create a user/service account (`kind`: `user` \| `service`). |
| `GET`    | `/users/{id}`                  | —                    | Get an account.                                              |
| `GET`    | `/users/{id}/repositories`     | —                    | List repositories owned by the account.                      |
| `POST`   | `/users/{id}/ssh-keys`         | `{title, publicKey}` | Register an SSH public key.                                  |
| `GET`    | `/users/{id}/ssh-keys`         | —                    | List the account's SSH keys.                                 |
| `DELETE` | `/users/{id}/ssh-keys/{keyId}` | —                    | Revoke an SSH key.                                           |
| `POST`   | `/users/{id}/tokens`           | `{title}`            | Mint a token; the raw value is returned **once**.            |
| `GET`    | `/users/{id}/tokens`           | —                    | List the account's tokens (never the secret).                |
| `DELETE` | `/users/{id}/tokens/{tokenId}` | —                    | Revoke a single token.                                       |
| `DELETE` | `/users/{id}/tokens`           | —                    | Revoke **all** of the account's tokens.                      |

**Repositories & permissions**

| Method   | Path                                      | Body                          | Description                                                       |
| -------- | ----------------------------------------- | ----------------------------- | ----------------------------------------------------------------- |
| `POST`   | `/repositories`                           | `{ownerId, name, visibility}` | Create a repository (`visibility`: `public` \| `private`).        |
| `GET`    | `/repositories/{id}`                      | —                             | Get repository metadata.                                          |
| `PUT`    | `/repositories/{id}/visibility`           | `{visibility}`                | Change visibility (`public` \| `private`).                        |
| `DELETE` | `/repositories/{id}`                      | —                             | Delete a repository (row + bare repo).                            |
| `GET`    | `/repositories/{id}/permissions`          | —                             | List collaborators.                                               |
| `PUT`    | `/repositories/{id}/permissions`          | `{userId, role}`              | Grant/update a collaborator role (`read` \| `write` \| `admin`).  |
| `DELETE` | `/repositories/{id}/permissions/{userId}` | —                             | Revoke a collaborator.                                            |
| `POST`   | `/repositories/{id}/webhooks`             | `{url}`                       | Register a push webhook; the signing secret is returned **once**. |
| `DELETE` | `/repositories/{id}/webhooks/{hookId}`    | —                             | Delete a webhook.                                                 |

### Health

The control port also serves an unauthenticated `GET /healthz` readiness probe. It returns `200 {"status":"ok"}` when the database is reachable and `503 {"status":"unavailable"}` otherwise, and backs the container `HEALTHCHECK`.

## Webhooks

Register a webhook on a repository and `headlessgit` will `POST` to it after every successful push.

One delivery is sent **per changed ref** (a branch/tag create, update, or delete — not per file or commit). The JSON body:

```json
{
  "event": "push",
  "ref": "refs/heads/main",
  "before": "0000000000000000000000000000000000000000",
  "after": "344018f5c8bce597cfb1b13058edc688f3a13230",
  "created": true,
  "deleted": false,
  "repository": {
    "id": 6,
    "name": "HeadlessGit",
    "full_name": "Axenos-dev/HeadlessGit"
  },
  "pusher": { "id": 7, "username": "Axenos-dev" },
  "timestamp": "2026-06-29T19:06:48Z"
}
```

`before`/`after` are the ref's SHAs around the push; a create has `before` all-zero (`created: true`), a delete has `after` all-zero (`deleted: true`). `repository.full_name` is `namespace/name`.

Each request carries these headers:

| Header                    | Value                                                                         |
| ------------------------- | ----------------------------------------------------------------------------- |
| `X-HeadlessGit-Event`     | `push`                                                                        |
| `X-HeadlessGit-Delivery`  | Unique id for this delivery attempt.                                          |
| `X-HeadlessGit-Signature` | `sha256=<hex>` — HMAC-SHA256 of the **raw body** keyed by the webhook secret. |

Verify a delivery by recomputing the HMAC over the exact request body with the secret returned at registration, e.g.:

```go
mac := hmac.New(sha256.New, []byte(secret))
mac.Write(body)
expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
ok := hmac.Equal([]byte(expected), []byte(r.Header.Get("X-HeadlessGit-Signature")))
```

The secret is generated server-side and shown **once** in the registration response.

## Development

```sh
./dev.sh up     # build and run the stack (docker compose)
./dev.sh gen    # regenerate sqlc code
./dev.sh test   # build + vet + test (what CI runs)
```

## License

[MIT](LICENSE)
