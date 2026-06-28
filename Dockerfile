# build on the native arch (fast), cross-compile to the target arch
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/headlessgit ./cmd/app

FROM alpine:3.20

# git provides git-upload-pack / git-receive-pack, which the server shells out to
# for both SSH and smart HTTP. No git-daemon/git-http-backend needed: the HTTP
# transport frames the smart protocol itself. No openssh: the SSH server is built in.
RUN apk add --no-cache git ca-certificates

# Bare repos arrive via a bind mount owned by the host UID; allow git to use
# them regardless of owner.
RUN git config --system --add safe.directory '*'

COPY --from=build /out/headlessgit /usr/local/bin/headlessgit

ENV REPO_ROOT=/data/repos
ENV SSH_HOST_KEY_PATH=/data/ssh/host_ed25519

# git http, control api, git ssh
EXPOSE 4000 4001 2222

# readiness probe against the control API (busybox wget exits non-zero on 5xx)
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
	CMD wget -q -O - "http://localhost:${CONTROL_PORT:-4001}/healthz" > /dev/null 2>&1 || exit 1

ENTRYPOINT ["headlessgit"]
