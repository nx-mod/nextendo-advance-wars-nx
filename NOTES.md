# Advance Wars 1+2: Re-Boot Camp (Switch) - Nextendo server: notes

Keep this file updated as you go: it is the map for this server.

- Game: Advance Wars 1+2: Re-Boot Camp, title `0100300012F2A000` (WayForward), Unity 2020.2.1 IL2CPP, NintendoSDK 10.4.1.
- Binary: `sd:/atmosphere/contents/0100300012F2A000/exefs/main` (dumped 2026-09-14), 36,928,540 bytes,
  SHA-256 `F920CFADE7D5CC55DC06C674669D4A904DE6730F4719DA70797D54681F4F4AF2`; segments text @0,
  rodata @0x0311F000, data @0x046DB000. Copy + segments in `aw-hack/capture/nso/` (git-ignored).
- Status (2026-09-14): **online login works on a real Switch**; ID tags stored (DataStore). Versus play not tested yet
  (friends-only, needs a second player).

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
4. The RMC calls it makes, DataStore first: unhandled ones are logged in full
   (`[AW Secure] UNHANDLED ...`) and now recorded structurally by `unhandled.go`
   (proto/method, count, last body sample), surfaced on the dashboard under
   `/api/stats` → `unhandled`. Watch that list to map map-share and the friend's
   ID-tag reads without scraping the log.

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
## Access key and game server id (2026-09-14, from global-metadata.dat)

Unity keeps C# const values in the IL2CPP metadata's field default values, not in main. Parsed from
`romfs:/Data/Managed/Metadata/global-metadata.dat` (IL2CPP metadata v27, 7,163,420 bytes, copy in `aw-hack/capture/`):

| NetworkConfig field | Value |
|---|---|
| `omas_server_accessKey` | **`c001f85f`** (string, the NEX access key) |
| `omas_server_gameId` | **`0x27723500`**: host `g27723500-lp1.s.n.srv.nintendo.net` |
| `kSwitchLocalCommunicationId` | `0x0100300012F2A000` |
| `nex_pluginMemSize` / `nex_nexMemSize` / `nex_reserveMemSize` | 0x200000 / 0x264000 / 0x64000 |
| `AsyncDataStoreType` / `MapShareDataStoreType` / `IDTagDataStoreType` | 0 / 1 / 4 |
| `CryptoKey` | 64-char string (likely the Pia session key; not needed by the server) |

How: header +0x40 fieldDefaultValues (12-byte entries: field index, type index, data index), +0x48 default value data,
+0x60 field definitions (name index into the identifier strings at +0x18). The access key is also the only 8-hex
literal in the literal table. `Nex.NgsLogin(gameServerId, accessKey)` is the login entry point.

ID tags are NEX **DataStore** objects ("Getting IDTag DataStores", "IDTag DataStore Create Error", "Not online, can't
get IDTag DataStore"), data type 4. Map sharing uses DataStore type 1 ("[NETMAPSHARE]"), async play type 0, with
notifications NEW_MATCH / TURN_DONE / MAP_SHARE.

## First online session (2026-09-14 03:03, CFW Switch, ID tags unlocked)

"Online connection successful" in game. Server side:

1. sni-router routes `g27723500-lp1.s.n.srv.nintendo.net` to 8459; `ValidateAndRequestTicketWithParam` (0xA.6) with the
   console NSA id, resolved to the account pid through nextendo-account.
2. Secure connect, `Register` (0xB.1).
3. **DataStore `ChangeMeta` (0x73.38)**, no PostObject first: dataId 0, persistence target {own pid, slot 0},
   modifiesFlag 0x91 (name, metaBinary, dataType), name `"<idtag>"`, dataType 4, metaBinary `"Version=1\nForce=-1\n"`,
   later `"Version=1\nForce=2\n"`. permission/delPermission in the param are the unset default (3, private) and not
   flagged. The game re-sends it each time the online menu refreshes.
4. `0x6D.52` (BrowseMatchmakeSessionNoHolder) after each ChangeMeta: answered with the open sessions (none).

Online play has **no random mode**: only "Pick a friend" and invites (`TrySendInvite`, `popup_no_friends_online`,
"Other account for map is not a friend"). A match needs two consoles/emulators whose accounts are Nextendo friends.
Citron (S22) crashes running the game.

`datastore.go` answers the DataStore calls: ChangeMeta creates the caller's missing persistent object from the param
(the game never posts its ID tag), and PostMetaBinary, GetMeta, GetMetasMultipleParam, SearchObject(Light),
GetPersistenceInfo and DeleteObject serve those objects to friends. One JSON file per object in `datastore/`
(`AW_DATASTORE_DIR`). Next to watch: the calls a friend's console makes to read the ID tag, the invite/join
matchmaking calls, and map share (DataStore type 1, likely PreparePostObject with an upload URL).