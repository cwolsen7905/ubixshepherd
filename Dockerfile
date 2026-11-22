# uBixShepherd daemon image. Build from the repository root:
#   docker build --build-arg VERSION=v0.1.0 -t shepherd:v0.1.0 .
# See docs/hosted.md and deploy/.

# 1. The web UI, embedded in the binary. Needs Node 22 or later.
FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# 2. The binary: no cgo, so it cross-compiles on the build platform for either
# architecture and needs nothing from the runtime image.
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist/ internal/webui/app/
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X github.com/ubixsys/ubixshepherd/internal/version.Version=${VERSION}" \
      -o /out/shepherd ./cmd/shepherd

# 3. The runtime. Debian 13 ships git 2.47 (the daemon needs 2.31 or newer), glab, gh,
# tini and the sqlite3 shell the backup job uses.
FROM debian:trixie-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates git openssh-client tini glab gh sqlite3 \
 && rm -rf /var/lib/apt/lists/* \
 && git --version

# A fixed non-root user. The home and workspace are volumes owned by it.
RUN useradd --uid 10001 --user-group --create-home --home-dir /home/shepherd --shell /usr/sbin/nologin shepherd \
 && install -d -o shepherd -g shepherd -m 0700 /data/home \
 && install -d -o shepherd -g shepherd -m 0755 /data/workspace

# Credentials are files under /run/secrets/shepherd, never environment variables, which
# agents would inherit. ssh reads the push key and known hosts from there. The glab, gh
# and agent CLI directories there are seeds only: the entrypoint copies them once into
# the home (/data/home/cli), where the tools can write their sessions and refreshed tokens.
RUN printf 'Host *\n  IdentityFile /run/secrets/shepherd/ssh/id\n  UserKnownHostsFile /run/secrets/shepherd/ssh/known_hosts\n  IdentitiesOnly yes\n  StrictHostKeyChecking yes\n' \
      > /etc/ssh/ssh_config.d/shepherd.conf

COPY --from=build /out/shepherd /usr/local/bin/shepherd
COPY deploy/docker/entrypoint.sh /usr/local/bin/shepherd-entrypoint
COPY deploy/docker/config.seed.yaml /usr/share/shepherd/config.seed.yaml

ENV SHEPHERD_HOME=/data/home \
    SHEPHERD_NO_AUTOSTART=1 \
    SHEPHERD_NO_UPDATE_CHECK=1 \
    GLAB_CONFIG_DIR=/data/home/cli/glab \
    GH_CONFIG_DIR=/data/home/cli/gh \
    CLAUDE_CONFIG_DIR=/data/home/cli/claude

USER 10001:10001
WORKDIR /data/workspace
VOLUME ["/data/home", "/data/workspace"]
EXPOSE 8443

# `shepherd status` asks the running daemon over loopback with the operator token in the
# home, so it needs no credential of its own. The public listener opens only once an
# owner account exists, so it is not what a probe can wait for.
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
  CMD ["shepherd", "status"]

# tini is PID 1: it reaps the agents' orphaned children and passes SIGTERM on, which
# starts the daemon's drain.
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/shepherd-entrypoint"]
CMD ["shepherd", "daemon"]
