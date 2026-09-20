# advance-wars

NEX game server for **Advance Wars 1+2: Re-Boot Camp** (Nintendo Switch, `0100300012F2A000`), built on the NextendoNetwork [nextendo-nex](https://github.com/NextendoNetwork/nextendo-nex) core. Source only: no binaries, no certs, no game assets. Not affiliated with WayForward, Intelligent Systems, Nintendo.

## Status

Tested on **one console only** (a CFW Switch, 2026-09-14). There has been no two-sided test: the game's only online mode is a match against a friend, and no second player has connected.

- **Seen working live:** the game's server id (`0x27723500`) and NEX access key (`c001f85f`, the default here, both read from the game's Unity metadata), the logins, and "Online connection successful" in the game.
- **Written from what the game sends, not checked from the other side:** the DataStore that keeps players' ID tags (`datastore.go`, covered by unit tests). The calls a friend's console makes to read a tag, the invite and join calls, and map sharing are not observed yet.
- **Not tested:** any match. Citron (S22) crashes running the game, so it cannot be the second player.

See [NOTES.md](NOTES.md) for the calls observed and how the key and server id were found.

## Build

Clone this repo and `nextendo-nex` side by side, then:

    go build -o server.exe .

See `example.env` for configuration.

## Credits

- **[Nextendo Network](https://nextendo.network)**: the NEX core, gates, dashboard and server pattern this server follows (template: torchlight-2 / borderlands-1).
- **[exlaunch](https://github.com/shadowninja108/exlaunch)** by **Shadow**: the in-game instrumentation used to map the game's online calls (`aw-hack`, an exlaunch logging module that is not part of this repository).
- **[kinnay/NintendoClients](https://github.com/kinnay/NintendoClients)** and its [wiki](https://github.com/kinnay/NintendoClients/wiki): NEX protocol method ids and parameters.
- **[Pretendo Network](https://pretendo.network)**: NEX documentation ([developer docs](https://developer.pretendo.network/overview/nex)).

References were read and reimplemented; no code was copied.