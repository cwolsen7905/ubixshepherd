#!/bin/sh
# Seed an empty home, then run the command (the daemon) by exec, so tini's child is the daemon.
# Read only when config.yaml does not exist yet:
# - a worker: SHEPHERD_CONTROLLER_URL, the https address of the controller it links to;
# - otherwise a hosted controller: SHEPHERD_PUBLIC_URL and SHEPHERD_TRUSTED_PROXIES (a comma
#   list of addresses or CIDRs of the reverse proxy).
set -eu
home="${SHEPHERD_HOME:?SHEPHERD_HOME is not set}"
seeds="${SHEPHERD_SEED_DIR:-/usr/share/shepherd}"
# A value made safe for the right side of a sed s|...|...|: | & and \ would otherwise end or change it.
sed_value() { printf '%s' "$1" | sed 's/[|&\\]/\\&/g'; }
if [ ! -e "$home/config.yaml" ]; then
  mkdir -p "$home"
  if [ -n "${SHEPHERD_CONTROLLER_URL:-}" ]; then
    case "$SHEPHERD_CONTROLLER_URL" in https://*) ;; *) echo "shepherd: SHEPHERD_CONTROLLER_URL must be an https:// address" >&2; exit 1 ;; esac
    sed -e "s|@CONTROLLER_URL@|$(sed_value "$SHEPHERD_CONTROLLER_URL")|" \
        "$seeds/config.seed-worker.yaml" > "$home/config.yaml.tmp"
  else
    : "${SHEPHERD_PUBLIC_URL:?set SHEPHERD_PUBLIC_URL (https://shepherd.example.com) to seed a new home, or SHEPHERD_CONTROLLER_URL for a worker}"
    : "${SHEPHERD_TRUSTED_PROXIES:?set SHEPHERD_TRUSTED_PROXIES (the reverse proxy address or CIDR) to seed a new home}"
    case "$SHEPHERD_TRUSTED_PROXIES" in REPLACE*) echo "shepherd: SHEPHERD_TRUSTED_PROXIES is still the placeholder; set the reverse proxy address or CIDR" >&2; exit 1 ;; esac
    sed -e "s|@PUBLIC_URL@|$(sed_value "$SHEPHERD_PUBLIC_URL")|" \
        -e "s|@TRUSTED_PROXIES@|$(sed_value "$SHEPHERD_TRUSTED_PROXIES")|" \
        "$seeds/config.seed.yaml" > "$home/config.yaml.tmp"
  fi
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
  list="$(mktemp)"
  (cd "$seed/$t" && find . -type f) > "$list" || { echo "shepherd: could not read $seed/$t (check the owner and mode of the mount)" >&2; exit 1; }
  while IFS= read -r f; do
    [ -e "$home/cli/$t/$f" ] && continue
    mkdir -p "$(dirname "$home/cli/$t/$f")"
    cp "$seed/$t/$f" "$home/cli/$t/$f" || { echo "shepherd: could not copy $seed/$t/$f into $home/cli/$t (check the owner and mode of the mount)" >&2; exit 1; }
  done < "$list"
  rm -f "$list"
done
exec "$@"
