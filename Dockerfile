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

# 3. The agent CLIs, pinned and checksum-verified per architecture. Claude Code comes from
# its per-platform npm package (a single native binary, no Node needed at runtime);
# OpenCode from its release archive. To bump one, change the version and both digests.
FROM --platform=$BUILDPLATFORM debian:trixie-slim AS clis
ARG TARGETARCH
ARG CLAUDE_CODE_VERSION=2.1.331
ARG OPENCODE_VERSION=1.19.6
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /dl
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) cc=x64; cc_sha=229bd9f5cfcda293bfb2bf138562d50140d7540153ddab8b5ab85b47ac2ad5d824b897d961d4eda177fa19a8c4e4a2fd479f224edffce8e3d7ac902c338fa5f2; oc=x64; oc_sha=47734e08968b30cc22a0bffb6d9feeed76ad4aa743213a12122040392d09a057 ;; \
      arm64) cc=arm64; cc_sha=cf2f8e381ee2d49eeb0a33683c1d99043ec402d4f412eb35c07fada2495918bd97a701f2c3e3ce19418ea4404113ac5ee449006165f31524d0dee0fc52d5a546; oc=arm64; oc_sha=5b68c1f4c5e75b4134e664cc8f36d634d64208d4fcfe5bcf3b48bab699c2dbac ;; \
      *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o cc.tgz "https://registry.npmjs.org/@anthropic-ai/claude-code-linux-$cc/-/claude-code-linux-$cc-${CLAUDE_CODE_VERSION}.tgz"; \
    echo "$cc_sha  cc.tgz" | sha512sum -c -; \
    curl -fsSL -o oc.tgz "https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/opencode-linux-$oc.tar.gz"; \
    echo "$oc_sha  oc.tgz" | sha256sum -c -; \
    mkdir cc oc; tar -xzf cc.tgz -C cc --strip-components=1 package/claude; tar -xzf oc.tgz -C oc opencode; \
    install -D -m 0755 cc/claude /out/claude; install -D -m 0755 oc/opencode /out/opencode

# 4. The runtime. Debian 13 ships git 2.47 (the daemon needs 2.31 or newer), glab, gh,
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
COPY --from=clis /out/claude /out/opencode /usr/local/bin/
COPY deploy/docker/entrypoint.sh /usr/local/bin/shepherd-entrypoint
COPY deploy/docker/config.seed.yaml /usr/share/shepherd/config.seed.yaml

ENV SHEPHERD_HOME=/data/home \
    SHEPHERD_NO_AUTOSTART=1 \
    SHEPHERD_NO_UPDATE_CHECK=1 \
    GLAB_CONFIG_DIR=/data/home/cli/glab \
    GH_CONFIG_DIR=/data/home/cli/gh \
    CLAUDE_CONFIG_DIR=/data/home/cli/claude \
    DISABLE_AUTOUPDATER=1 \
    OPENCODE_DISABLE_AUTOUPDATE=true \
    XDG_DATA_HOME=/data/home/cli/xdg-data \
    XDG_CONFIG_HOME=/data/home/cli/xdg-config

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
