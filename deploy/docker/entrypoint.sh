#!/bin/sh
# Seed an empty home, then run the command (the daemon) by exec, so tini's child is the daemon.
# SHEPHERD_PUBLIC_URL and SHEPHERD_TRUSTED_PROXIES (a comma list of addresses or CIDRs of
# the reverse proxy) are read only when config.yaml does not exist yet.
set -eu
home="${SHEPHERD_HOME:?SHEPHERD_HOME is not set}"
if [ ! -e "$home/config.yaml" ]; then
  : "${SHEPHERD_PUBLIC_URL:?set SHEPHERD_PUBLIC_URL (https://shepherd.example.com) to seed a new home}"
  : "${SHEPHERD_TRUSTED_PROXIES:?set SHEPHERD_TRUSTED_PROXIES (the reverse proxy address or CIDR) to seed a new home}"
  mkdir -p "$home"
  sed -e "s|@PUBLIC_URL@|$SHEPHERD_PUBLIC_URL|" \
      -e "s|@TRUSTED_PROXIES@|$SHEPHERD_TRUSTED_PROXIES|" \
      /usr/share/shepherd/config.seed.yaml > "$home/config.yaml.tmp"
  mv "$home/config.yaml.tmp" "$home/config.yaml"
  echo "shepherd: seeded $home/config.yaml"
fi
# Seed the CLIs' config directories from the read-only credential mount, once. A file that
# is already there is kept: the tool may have refreshed it. To replace one, delete it.
umask 077
for t in glab gh claude; do
  mkdir -p "$home/cli/$t"
  if [ -d "/run/secrets/shepherd/$t" ]; then
    cp -Rn "/run/secrets/shepherd/$t/." "$home/cli/$t/" 2>/dev/null || true
  fi
done
exec "$@"
