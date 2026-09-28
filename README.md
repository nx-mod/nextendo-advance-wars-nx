# advance-wars

**A new game server implementation by nx-mod** for the Nextendo Network.

NEX game server for **Advance Wars 1+2: Re-Boot Camp** (Nintendo Switch, `0100300012F2A000`), built on the NextendoNetwork [nextendo-nex](https://github.com/NextendoNetwork/nextendo-nex) core. Source only: no binaries, no certs, no game assets. Not affiliated with WayForward, Intelligent Systems, Nintendo.

## Status

Tested on **one console only** (a CFW Switch, 2026-09-14). There has been no two-sided test: the game's only online mode is a match against a friend, and no second player has connected.

- **Seen working live:** the game's server id (`0x27723500`) and NEX access key (`c001f85f`, the default here, both read from the game's Unity metadata), the logins, and "Online connection successful" in the game.
- **Written from what the game sends, not checked from the other side:** the DataStore that keeps players' ID tags (`datastore.go`, covered by unit tests). The calls a friend's console makes to read a tag, the invite and join calls, and map sharing are not observed yet.
- **Not tested:** any match. Citron (S22) crashes running the game, so it cannot be the second player.

See [NOTES.md](NOTES.md) for the calls observed and how the key and server id were found.

## Requirements

This server runs behind the rest of the Nextendo stack. It needs nothing cloned next to it: `go build` fetches the NEX core ([nextendo-nex](https://github.com/NextendoNetwork/nextendo-nex), a Go module) by itself.

| component | needed? | what it must provide |
|---|---|---|
| **sni-router** | required | A route sending `g27723500-lp1.s.n.srv.nintendo.net` to `BACKEND_AW` (default `127.0.0.1:8459`). The route is not in sni-router's `main` yet: it is the `feat/advance-wars` branch of [nx-mod/sni-router](https://github.com/nx-mod/sni-router), one commit on top of `main`. |
| **nextendo-account** | required with `NEXTENDO_REQUIRE_ACCOUNT=1` | `GET /api/nsa` (a console's NSA id to a Nextendo account) and `POST /internal/online-check`, with `X-Internal-Key`. With the gate on, a login whose NSA id cannot be resolved is refused. |
| **nextendo-dashboard** | optional | A `aw` source polling `/api/stats` on port 8097 (`DASH_AW_URL`, `DASH_AW_TOKEN`): the `feat/advance-wars-stats` branch of [nx-mod/nextendo-dashboard](https://github.com/nx-mod/nextendo-dashboard). Without it the server works but is not on the shared dashboard. |
| **DNS** | required | `g27723500-lp1.s.n.srv.nintendo.net` must resolve to the machine running sni-router, and never to Nintendo. On a console that is an Atmosphere hosts entry; the standard Nextendo hosts file already sends `*.srv.nintendo.net` to the stack. |
| **TLS certificate** | required | A certificate and key for the game's auth host, from a CA your clients trust (`CERT_FILE`, `KEY_FILE`). Yours to provide; none is shipped. |

The secure server is not behind the router: the game connects to `NEXTENDO_HOST:60015` directly, so `NEXTENDO_HOST` must be the address players can reach.

## Install

1. Build: `go build -o server.exe .` (Go 1.23 or later).
2. Copy `example.env` to `.env` and set the secrets. The server does **not** read `.env` itself: export the variables into its environment (a launcher or service file), and set `NEXTENDO_SECRET` or `NEXTENDO_SECRET_FILE`, `NEXTENDO_INTERNAL_KEY`, `NEXTENDO_SECURE_PASSWORD` and `DASH_TOKEN`.
3. Put `cert.pem` and `key.pem` next to the binary, or point `CERT_FILE` and `KEY_FILE` at them.
4. Start it. Auth listens on `AUTH_PORT` (`8459` behind sni-router), the secure server on `60015`, the dashboard on `8097`.

| setting | default | meaning |
|---|---|---|
| `AW_ACCESS_KEY` | `c001f85f` | the game's NEX access key, confirmed live |
| `AW_NEX_VERSION` | `40604` | NEX 4.6.4, from the version string in the game binary |
| `AW_DATASTORE_DIR` | `datastore` | where ID tags are kept (one JSON file per object) |

Game: Advance Wars 1+2: Re-Boot Camp, title `0100300012F2A000`, game server id `0x27723500`.

## Known limits

- **One console only.** The game's only online mode is a match against a friend (pick a friend, or an invite), so it needs two consoles or emulators whose accounts are Nextendo friends. No second player has connected.
- **ID tags.** The game stores each player's ID tag as a DataStore object. `datastore.go` keeps them (one JSON file each in `datastore/`, `AW_DATASTORE_DIR`) and serves them to friends. That is written from what the game sends and covered by unit tests; the calls a friend's console makes to read a tag have not been observed.
- **Not looked at yet:** invites and joining (whatever the core's matchmaking handlers do with them is unchecked), and map sharing (DataStore type 1, probably with an upload URL). Unhandled calls are logged as `[AW Secure] UNHANDLED ...` with the full request and answered with an empty success.
- **Citron** (S22) crashes running the game, so it cannot be the second player.
- No other persistence: games and connections live in memory.

## To do

- Get a second console or emulator into a match and watch the invite and join calls.
- Watch what a friend's console calls to read an ID tag.
- Map sharing.

## Credits

- **[Nextendo Network](https://nextendo.network)**: the NEX core, gates, dashboard and server pattern this server follows (template: torchlight-2 / borderlands-1).
- **[exlaunch](https://github.com/shadowninja108/exlaunch)** by **Shadow**: the in-game instrumentation used to map the game's online calls (`aw-hack`, an exlaunch logging module that is not part of this repository).
- **[kinnay/NintendoClients](https://github.com/kinnay/NintendoClients)** and its [wiki](https://github.com/kinnay/NintendoClients/wiki): NEX protocol method ids and parameters.
- **[Pretendo Network](https://pretendo.network)**: NEX documentation ([developer docs](https://developer.pretendo.network/overview/nex)).

References were read and reimplemented; no code was copied.

## Credits

Built by nx-mod for the **Nextendo Network**, on the work of the Nextendo Network team — https://nextendo.network. Nextendo is awesome.
