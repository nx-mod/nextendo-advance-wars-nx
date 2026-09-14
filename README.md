# advance-wars

NEX game server for **Advance Wars 1+2: Re-Boot Camp** (Nintendo Switch, `0100300012F2A000`), built on the NextendoNetwork [nextendo-nex](https://github.com/NextendoNetwork/nextendo-nex) core. Source only: no binaries, no certs, no game assets. Not affiliated with WayForward, Intelligent Systems, Nintendo.

Work in progress: an untested scaffold. The server refuses to start until the game's NEX access key is known. See [NOTES.md](NOTES.md).

## Build

Clone this repo and `nextendo-nex` side by side, then:

    go build -o server.exe .

See `example.env` for configuration.

## Credits

- **[Nextendo Network](https://nextendo.network)**: the NEX core, gates, dashboard and server pattern this server follows (template: torchlight-2 / borderlands-1).
- **[exlaunch](https://github.com/shadowninja108/exlaunch)** by **Shadow**: the in-game instrumentation used to map the game's online calls (`aw-hack`).
- **[kinnay/NintendoClients](https://github.com/kinnay/NintendoClients)** and its [wiki](https://github.com/kinnay/NintendoClients/wiki): NEX protocol method ids and parameters.
- **[Pretendo Network](https://pretendo.network)**: NEX documentation ([developer docs](https://developer.pretendo.network/overview/nex)).

References were read and reimplemented; no code was copied.