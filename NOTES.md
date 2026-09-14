# Advance Wars 1+2: Re-Boot Camp (Switch) - Nextendo server: notes

Keep this file updated as you go: it is the map for this server.

- Game: Advance Wars 1+2: Re-Boot Camp, title `0100300012F2A000` (WayForward), Unity 2020.2.1 IL2CPP, NintendoSDK 10.4.1.
- Binary: `sd:/atmosphere/contents/0100300012F2A000/exefs/main` (dumped 2026-09-14), 36,928,540 bytes,
  SHA-256 `F920CFADE7D5CC55DC06C674669D4A904DE6730F4719DA70797D54681F4F4AF2`; segments text @0,
  rodata @0x0311F000, data @0x046DB000. Copy + segments in `aw-hack/capture/nso/` (git-ignored).
- Status (2026-09-14): **scaffold only, never run against the game.**

## Online stack (from the binary)

- **NEX 4.6.4** (`NintendoSDK-NEX-for_NX-4_6_4-S1040-20200417`) + **Pia**, statically linked, symbols stripped.
  Strings: `ONLN [Nex_impl_switch::Nex_impl]`, `ONLN [Pia_switch::MakeRef]`, `NexMatchJoinSessionJob`,
  `NexMatchJointSessionJob`, `RandomMatchmakeJob`, `NexMatchCommunityManagementJob::DestroyCommunity`,
  `NexProcessHostMigrationJob`, `RendezVous::SessionVoid`.
- **DataStore**: `nn::nex::DataStoreClient::PostObject() failed`, `ChangeMeta() operation failed` (map or replay
  sharing; see super-mario-maker-2 for the core's DataStore support).
- SDK imports present: `nn::nsd::ResolveEx`, sockets, `nn::err::ShowError`, `nn::fs::SetAllocator`. No curl, no ldn.

## To find

1. **Access key**: not in `main` (no 8-hex literal; NEX symbols stripped, so no SetSandboxAccessKey hook). Unity
   keeps C# string literals in `romfs:/Data/Managed/Metadata/global-metadata.dat`: dump that file and search it.
2. **Game server id**: aw-hack logs `nsd resolve 'g<id>-%.s.n.srv.nintendo.net'`. Add the sni-router route
   (`BACKEND_AW=127.0.0.1:8459`).
3. **main.npdm**: dump the game's own exefs `main.npdm` before installing aw-hack (a generic one crashed World War Z).
4. The RMC calls it makes, DataStore first: unhandled ones are logged in full (`[AW Secure] UNHANDLED ...`).

## Ports (local stack)

| What | Port |
|---|---|
| auth (behind sni-router) | 8459 |
| secure | 60015 |
| dashboard | 8097 |

## References

- kinnay/NintendoClients wiki (NEX protocols, DataStore), Pretendo developer docs, exlaunch.
- Sibling servers: `torchlight-2` (NEX + Pia, stripped, live), `super-mario-maker-2` (DataStore).
## First launch with aw-hack (2026-09-14 02:24, CFW Switch)

- Game launched and ran with aw-hack installed: the NPDM built from the game's own values (16 MB system resource,
  optimized allocation) works; 25+ hooks installed, log opened after nn::fs::SetAllocator.
- **No network activity at all** from boot to the main menu: no nsd resolve, getaddrinfo or connect. The NEX login only
  happens when online play starts, and the game gates online behind unlocking ID tags. Next: a save with ID tags
  unlocked (JKSV), then one online attempt for the game server id; access key still to be read from
  global-metadata.dat.