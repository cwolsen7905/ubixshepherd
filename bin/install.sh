#!/bin/sh
# Install uBixShepherd from a GitHub release (macOS and Linux).
#
#   sh install.sh                      # the newest release, pre-releases included
#   VERSION=v0.1.0-beta.1 sh install.sh
#   PREFIX=/usr/local/bin sh install.sh
#
# It downloads the archive for this OS and architecture and SHA256SUMS, checks the
# archive against it, and installs `shepherd` into $PREFIX (default ~/.local/bin). It does
# not run cosign: docs/RELEASING.md#verifying-a-download has the stronger check.
# Windows: see docs/INSTALL.md. Read this script before you run it.
set -eu

REPO="ubixsys/ubixshepherd"
PREFIX="${PREFIX:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v tar >/dev/null 2>&1 || die "tar is required"

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "unsupported OS $(uname -s): on Windows follow docs/INSTALL.md" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) die "unsupported architecture $(uname -m)" ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
  sha_check() { sha256sum -c -; }
elif command -v shasum >/dev/null 2>&1; then
  sha_check() { shasum -a 256 -c -; }
else
  die "sha256sum or shasum is required to verify the download"
fi

version="${VERSION:-}"
if [ -z "$version" ]; then
  # /releases/latest skips pre-releases, and every beta is one: take the newest of the list.
  version="$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=1" |
    sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)"
  [ -n "$version" ] || die "could not find a release of $REPO; set VERSION=vX.Y.Z"
fi

name="shepherd-$version-$os-$arch"
base="https://github.com/$REPO/releases/download/$version"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Installing shepherd $version ($os/$arch) into $PREFIX"
curl -fsSL -o "$tmp/$name.tar.gz" "$base/$name.tar.gz" || die "could not download $base/$name.tar.gz"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || die "could not download $base/SHA256SUMS"

line="$(grep "  $name.tar.gz\$" "$tmp/SHA256SUMS" || true)"
[ -n "$line" ] || die "SHA256SUMS has no entry for $name.tar.gz"
(cd "$tmp" && printf '%s\n' "$line" | sha_check >/dev/null) || die "checksum mismatch for $name.tar.gz: not installing"
say "Checksum OK"

tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
[ -f "$tmp/$name/shepherd" ] || die "the archive does not hold shepherd"

mkdir -p "$PREFIX"
cp "$tmp/$name/shepherd" "$PREFIX/.shepherd.new"
chmod 755 "$PREFIX/.shepherd.new"
# A browser download is quarantined on macOS; curl's is not. Clearing is harmless.
[ "$os" = darwin ] && xattr -d com.apple.quarantine "$PREFIX/.shepherd.new" 2>/dev/null || true
mv -f "$PREFIX/.shepherd.new" "$PREFIX/shepherd"

say "Installed $PREFIX/shepherd"
case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *) say "Note: $PREFIX is not on your PATH. Add it, for example:"
     say "  echo 'export PATH=\"$PREFIX:\$PATH\"' >> ~/.profile" ;;
esac
if [ -x "$PREFIX/shepherd" ] && SHEPHERD_NO_AUTOSTART=1 "$PREFIX/shepherd" daemon status 2>/dev/null | grep -q 'running, pid'; then
  say "A daemon is running the old build: run  shepherd daemon restart"
fi
say "Next: shepherd version, then see docs/INSTALL.md (First run)."
