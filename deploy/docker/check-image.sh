#!/bin/sh
# Check a built image: check-image.sh <image reference> <expected tag>
# The release workflow runs it against the pushed digest, on amd64 and (under emulation)
# arm64, before the digest is signed.
set -eu
img="${1:?image reference}"; tag="${2:?expected tag}"

for platform in linux/amd64 linux/arm64; do
  run() { docker run --rm --platform "$platform" "$@"; }
  echo "== $platform"
  [ "$(run --entrypoint id "$img" -u)" = 10001 ]
  run --entrypoint sh "$img" -c 'git --version && glab --version && gh --version && ssh -V && sqlite3 --version'
  got="$(run --entrypoint shepherd "$img" version)"
  [ "$got" = "shepherd $tag" ] || { echo "tag $tag, but the image reports: $got"; exit 1; }
done
docker pull -q "$img" >/dev/null
[ "$(docker image inspect -f '{{.Config.User}}' "$img")" = 10001:10001 ]
docker image inspect -f '{{json .Config.Entrypoint}}' "$img" | grep -q '/usr/bin/tini'

# An empty volume starts and answers the health check (amd64). The key file is the one
# credential the daemon needs to start with a hosted config.
k="$(mktemp -d)"
head -c 32 /dev/urandom > "$k/totp-key"
chmod 0600 "$k/totp-key"
sudo chown 10001:10001 "$k/totp-key"
docker run -d --name shepherd-check \
  -e SHEPHERD_PUBLIC_URL=https://shepherd.example.com -e SHEPHERD_TRUSTED_PROXIES=172.17.0.0/16 \
  -v "$(docker volume create)":/data/home -v "$(docker volume create)":/data/workspace \
  -v "$k/totp-key":/run/secrets/shepherd/totp-key:ro "$img"
for i in $(seq 1 30); do
  docker exec shepherd-check shepherd status >/dev/null 2>&1 && { echo "health check answers"; exit 0; }
  sleep 2
done
docker logs shepherd-check
exit 1
