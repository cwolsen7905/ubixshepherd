#!/bin/sh
# Seed an empty home, then run the command (the daemon) by exec, so tini's child is the daemon.
# SHEPHERD_PUBLIC_URL and SHEPHERD_TRUSTED_PROXIES (a comma list of addresses or CIDRs of
# the reverse proxy) are read only when config.yaml does not exist yet.
set -eu
home="${SHEPHERD_HOME:?SHEPHERD_HOME is not set}"
if [ ! -e "$home/config.yaml" ]; then
  : "${SHEPHERD_PUBLIC_URL:?set SHEPHERD_PUBLIC_URL (https://shepherd.example.com) to seed a new home}"
  case "$SHEPHERD_TRUSTED_PROXIES" in REPLACE*) echo "shepherd: SHEPHERD_TRUSTED_PROXIES is still the placeholder; set the reverse proxy address or CIDR" >&2; exit 1 ;; esac
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
seed="/run/secrets/shepherd"
for t in glab gh claude; do
  mkdir -p "$home/cli/$t"
  [ -d "$seed/$t" ] || continue
  # A failure stops the start with the reason, rather than starting without credentials.
  (cd "$seed/$t" && find . -type f) | while IFS= read -r f; do
    [ -e "$home/cli/$t/$f" ] && continue
    mkdir -p "$(dirname "$home/cli/$t/$f")"
    cp "$seed/$t/$f" "$home/cli/$t/$f"
  done || { echo "shepherd: could not copy $seed/$t into $home/cli/$t (check the owner and mode of the mount)" >&2; exit 1; }
done
exec "$@"
