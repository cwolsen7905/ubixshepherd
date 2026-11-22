# launchd (macOS)

On macOS the daemon is normally a per-user agent, and Shepherd writes it for you:

    shepherd daemon install      # writes ~/Library/LaunchAgents/..., loads it, starts the daemon
    shepherd daemon uninstall

launchd is told to wait 30 seconds after SIGTERM before it kills the daemon, which is
longer than the daemon takes to end its agents. The home is `~/.shepherd` (or
`SHEPHERD_HOME`, which the plist records) and the workspace is any directory you register.

For a hosted daemon on a Mac, add `daemon.public` to `config.yaml` as in
`docs/hosted.md`, with a local reverse proxy (Caddy, nginx) as the trusted proxy, or
the certificate and key as files. Keep credentials as files of mode 0600 in your user's
home (the Keychain is not read), and run `shepherd account set` once.
