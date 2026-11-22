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
exec "$@"
