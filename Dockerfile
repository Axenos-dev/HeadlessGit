# build on the native arch (fast), cross-compile to the target arch
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/headlessgit ./cmd/app

FROM alpine:3.20

# git provides git-upload-pack / git-receive-pack; git-daemon provides
# git-http-backend on Alpine. The server shells out to all three.
# No openssh needed: the SSH server is built in.
RUN apk add --no-cache git git-daemon ca-certificates

# Bare repos arrive via a bind mount owned by the host UID; allow git to use
# them regardless of owner.
RUN git config --system --add safe.directory '*'

COPY --from=build /out/headlessgit /usr/local/bin/headlessgit

ENV REPO_ROOT=/data/repos
ENV SSH_HOST_KEY_PATH=/data/ssh/host_ed25519

# git http, control api, git ssh
EXPOSE 4000 4001 2222

ENTRYPOINT ["headlessgit"]
