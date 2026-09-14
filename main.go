// Command advance-wars runs the Advance Wars 1+2: Re-Boot Camp (Switch, 0100300012F2A000) online servers on the Nextendo NEX
// stack.
//
// The game is a Unity (IL2CPP) title that statically links Nintendo NEX 4.6.4 and Pia
// ("NintendoSDK-NEX-for_NX-4_6_4" in its main): NEX matchmaking with host migration, Pia sessions
// and DataStore. Scaffold, not yet run against the game:
//   - auth   (:443 behind sni-router)  TicketGranting: LoginEx issues the Kerberos ticket.
//   - secure (:60015)                  SecureConnection + matchmaking + NAT traversal + ranking + utility.
//
// Known from NetworkConfig in global-metadata.dat: access key c001f85f, game server 0x27723500\n// (g27723500-lp1.s.n.srv.nintendo.net). Still unknown: the DataStore calls (ID tags, map share).
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

const (
	securePID     = 2
	sessionKeyLen = 32
	appID         = "0100300012f2a000"
)

var (
	// accessKey is the game's NEX access key: NetworkConfig.omas_server_accessKey, a C# const
	// read from the IL2CPP field default values in romfs global-metadata.dat (see NOTES.md).
	// The same class gives the game server id, omas_server_gameId = 0x27723500.
	accessKey = envOr("AW_ACCESS_KEY", "c001f85f")
	// NEX 4.6.4 per the SDK path in the game's main.
	nexVersion = envOrInt("AW_NEX_VERSION", 40604)

	nextendoHost   = envOr("NEXTENDO_HOST", "127.0.0.1")
	authPort       = envOrInt("AUTH_PORT", 443)
	securePort     = envOrInt("SECURE_PORT", 60015) // borderlands-1=60012 world-war-z=60013 torchlight-2=60014 advance-wars=60015
	securePassword = envOr("NEXTENDO_SECURE_PASSWORD", "")
	certFile       = envOr("CERT_FILE", "cert.pem")
	keyFile        = envOr("KEY_FILE", "key.pem")

	nextendoSecret = loadNextendoSecret()
	requireAccount = os.Getenv("NEXTENDO_REQUIRE_ACCOUNT") == "1"
)

func main() {
	// Refuse to start with a guessed key: PRUDP signatures would fail silently and only
	// confuse captures.
	if len(accessKey) != 8 {
		fmt.Println("[AW] FATAL: AW_ACCESS_KEY is not set (8 hex chars).")
		fmt.Println("[AW] Recover it with aw-hack (SetSandboxAccessKey is logged), then set AW_ACCESS_KEY.")
		os.Exit(1)
	}
	// No public default Kerberos password: anyone knowing it could forge tickets.
	if securePassword == "" {
		fmt.Println("[AW] FATAL: NEXTENDO_SECURE_PASSWORD is not set.")
		os.Exit(1)
	}

	settings := nex.NewSwitchSettings(accessKey, nexVersion)

	// --- Auth server ---
	secureURL := nex.NewStationURL("prudps")
	secureURL.Set("address", nextendoHost)
	secureURL.SetInt("port", securePort)
	secureURL.SetInt("CID", 1)
	secureURL.SetInt("PID", securePID)
	secureURL.SetInt("sid", 1)
	secureURL.SetInt("stream", 10)
	secureURL.SetInt("type", 2)

	authEndpoint := nex.NewEndpoint(settings)
	authCfg := &nex.AuthConfig{
		Settings:         settings,
		SecurePID:        securePID,
		SecurePassword:   securePassword,
		SecureStationURL: secureURL,
		ServerName:       "Nextendo",
		SessionKeyLength: sessionKeyLen,
		ResolveUser:      resolveUser,
	}
	authEndpoint.Register(nex.ProtocolTicketGranting, authCfg.Handler())
	authEndpoint.RegisterFallback(func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		// Unknown auth calls are logged in full: this server is still being mapped.
		fmt.Printf("[AW Auth] UNHANDLED pid=%d proto=%#x method=%d call=%d body=%x\n", c.PID, req.Protocol, req.Method, req.CallID, req.Body)
		return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, nil)
	})
	authEndpoint.OnRMC = logRMC("Auth")
	authServer := nex.NewServer(authEndpoint)

	// --- Secure server ---
	secureEndpoint := nex.NewEndpoint(settings)
	secureEndpoint.SetSecureAccount(securePassword, securePID)

	mm := nex.NewMatchmaking()
	// Answer FindMatchmakeSessionByParticipant (0x6D.0x33) for real: Torchlight II needed it for
	// its player list. Revisit once this game's calls are logged.
	mm.FindByParticipantEnabled = true
	secureEndpoint.Register(nex.ProtocolSecureConnection, nex.SecureConnectionHandler())
	extHandler := mm.ExtensionHandler()
	secureEndpoint.Register(nex.ProtocolMatchmakeExtension, func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		if req.Method == methodUpdateMatchmakeSessionAttribute {
			return updateSessionAttributes(c, req, extHandler)
		}
		return extHandler(c, req)
	})
	secureEndpoint.Register(nex.ProtocolMatchMaking, mm.MatchMakingHandler())
	secureEndpoint.Register(nex.ProtocolMatchMakingExt, mm.MatchMakingExtHandler())
	secureEndpoint.Register(nex.ProtocolNATTraversal, nex.NATTraversalHandler())
	secureEndpoint.Register(nex.ProtocolRanking, nex.RankingHandler())
	secureEndpoint.Register(nex.ProtocolUtility, nex.UtilityHandler())
	secureEndpoint.RegisterFallback(func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		fmt.Printf("[AW Secure] UNHANDLED pid=%d proto=%#x method=%d call=%d body=%x\n", c.PID, req.Protocol, req.Method, req.CallID, req.Body)
		return nex.NewRMCSuccess(c.Settings, req.Protocol, req.Method, req.CallID, nil)
	})
	logSecure := logRMC("Secure")
	secureEndpoint.OnRMC = func(c *nex.Connection, req *nex.RMCMessage) {
		logSecure(c, req)
		noteRMC(c, req)
	}
	secureEndpoint.OnConnect = func(c *nex.Connection) {
		fmt.Printf("[AW Secure] connected pid=%d id=%d addr=%s\n", c.PID, c.ID, c.RemoteAddr)
	}
	// A crashed client never unregisters its gathering: drop it with the connection.
	secureEndpoint.OnDisconnect = func(c *nex.Connection) {
		mm.RemovePlayer(c.PID)
	}
	secureServer := nex.NewServer(secureEndpoint)

	secureEndpoint.StartReaper()
	go startDashboard(secureEndpoint, mm)

	// Behind sni-router the PROXY header carries the console's real address; the secure
	// connection arrives direct, and the two must agree.
	proxyProto := os.Getenv("NEXTENDO_PROXY_PROTOCOL") == "1"
	go func() {
		fmt.Printf("[AW Auth] listening WSS :%d (proxyProto=%v, secure URL -> %s)\n", authPort, proxyProto, secureURL.String())
		var err error
		if proxyProto {
			err = authServer.ListenSecureProxy(authPort, certFile, keyFile)
		} else {
			err = authServer.ListenSecure(authPort, certFile, keyFile)
		}
		if err != nil {
			fmt.Printf("[AW Auth] stopped: %v\n", err)
		}
	}()

	fmt.Printf("[AW Secure] listening WSS :%d (title %s, nex %d)\n", securePort, appID, nexVersion)
	if err := secureServer.ListenSecure(securePort, certFile, keyFile); err != nil {
		fmt.Printf("[AW Secure] stopped: %v\n", err)
	}
}

// MatchmakeExtension UpdateMatchmakeSessionAttribute: u32 gid + qList<u32>. Borderlands needed
// it to open a public lobby; kept here because nextendo-nex does not answer it.
const methodUpdateMatchmakeSessionAttribute = 12

// updateSessionAttributes answers UpdateMatchmakeSessionAttribute. nextendo-nex has no handler
// for it and refuses with Core::NotImplemented, which stops the lobby. Each attribute is applied
// through the core's ModifyCurrentGameAttribute handler (same owner check, same store, so later
// searches see them), then the call is acknowledged with an empty success.
func updateSessionAttributes(c *nex.Connection, req *nex.RMCMessage, ext nex.RMCHandler) *nex.RMCMessage {
	in := nex.NewStreamIn(req.Body, c.Settings)
	gid := in.U32()
	attribs := nex.ReadList(in, func(i *nex.StreamIn) uint32 { return i.U32() })
	fmt.Printf("[AW Secure] UpdateMatchmakeSessionAttribute pid=%d gid=%d attribs=%v\n", c.PID, gid, attribs)

	for index, value := range attribs {
		out := nex.NewStreamOut(c.Settings)
		out.U32(gid)
		out.U32(uint32(index))
		out.U32(value)
		sub := *req
		sub.Method = nex.MethodModifyCurrentGameAttribute
		sub.Body = out.Bytes()
		ext(c, &sub)
	}
	return nex.NewRMCSuccess(c.Settings, nex.ProtocolMatchmakeExtension, req.Method, req.CallID, nil)
}

// resolveUser maps a LoginEx username to an account, as the other Nextendo NEX servers do:
// a signed nx2 token or a proven emulator PID keeps its account PID, a console NSA id is
// resolved through nextendo-account, anything else is anonymous (unless an account is required).
func resolveUser(username string, extraData []byte) (uint64, []byte, bool) {
	// The source key encrypts the client ticket; the console expects 32 bytes.
	sk := sha256.Sum256([]byte("nextendo-src:" + username))
	sourceKey := sk[:]

	if pid, ok := nextendoPIDFromToken(username); ok {
		if allow, reason := nextendoOnlineCheck(pid, "ryujinx"); !allow {
			fmt.Printf("[AW Auth] pid=%d online refused (%s)\n", pid, reason)
			return 0, nil, false
		}
		return pid, sourceKey, true
	}

	if n, err := strconv.ParseUint(username, 10, 64); err == nil && n >= 1800000000 {
		provenPID, proven := uint64(0), false
		if tok, ok := nex.NexTokenFromLoginExtraData(extraData); ok {
			provenPID, proven = nextendoPIDFromToken(tok)
		}
		if n < 1810000000 {
			switch {
			case proven && provenPID == n:
				fmt.Printf("[AW Auth][bind] pid=%d OK: nx2 proves the PID\n", n)
			case proven && provenPID != n:
				fmt.Printf("[AW Auth][bind] pid=%d IMPERSONATION: nx2 proves %d\n", n, provenPID)
			default:
				fmt.Printf("[AW Auth][bind] pid=%d NO PROOF: no nx2 in extraData\n", n)
			}
			if requireSignedToken() && !(proven && provenPID == n) {
				fmt.Printf("[AW Auth] pid=%d refused: identity not proven\n", n)
				return 0, nil, false
			}
		}
		pid, kind := n, "ryujinx"
		if n >= 1810000000 {
			kind = "switch"
			rp, st := resolveNSAtoPID(n)
			switch st {
			case nsaOK:
				pid = rp
				fmt.Printf("[AW Auth] NSA %d -> account pid=%d\n", n, pid)
			case nsaUnknown, nsaUnreachable:
				fmt.Printf("[AW Auth] NSA %d refused (%v)\n", n, st)
				return 0, nil, false
			}
		}
		if allow, reason := nextendoOnlineCheck(pid, kind); !allow {
			fmt.Printf("[AW Auth] pid=%d online refused (%s)\n", pid, reason)
			return 0, nil, false
		}
		return pid, sourceKey, true
	}

	if requireAccount {
		fmt.Printf("[AW Auth] anonymous refused: %q\n", username)
		return 0, nil, false
	}
	return anonymousPID(username), sourceKey, true
}

// revokedNexPayloads lists leaked nex_token payloads ("pid.username.expiry") rejected despite
// a valid HMAC. Keep in sync with nextendo-account and the sibling servers.
var revokedNexPayloads = map[string]bool{
	"1800000006.Kazuu.1787343209": true, // leaked in the 1.6.5-win release
}

func nextendoPIDFromToken(s string) (uint64, bool) {
	if len(nextendoSecret) == 0 || !strings.HasPrefix(s, "nx2.") {
		return 0, false
	}
	parts := strings.Split(s[len("nx2."):], ".")
	if len(parts) != 2 {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, false
	}
	mac := hmac.New(sha256.New, nextendoSecret)
	mac.Write([]byte("nex:" + string(raw)))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return 0, false
	}
	if revokedNexPayloads[string(raw)] {
		return 0, false
	}
	f := strings.SplitN(string(raw), ".", 3)
	if len(f) != 3 {
		return 0, false
	}
	pid, err := strconv.ParseUint(f[0], 10, 64)
	if err != nil {
		return 0, false
	}
	if exp, err := strconv.ParseInt(f[2], 10, 64); err != nil || time.Now().Unix() > exp {
		return 0, false
	}
	return pid, true
}

func loadNextendoSecret() []byte {
	if v := os.Getenv("NEXTENDO_SECRET"); v != "" {
		return []byte(v)
	}
	path := envOr("NEXTENDO_SECRET_FILE", "nextendo_secret.key")
	if b, err := os.ReadFile(path); err == nil {
		if dec, derr := hex.DecodeString(strings.TrimSpace(string(b))); derr == nil && len(dec) >= 16 {
			return dec
		}
	}
	return nil
}

func anonymousPID(username string) uint64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(username))
	return 1800000000 + uint64(h.Sum32()%100000000)
}

func logRMC(tag string) func(*nex.Connection, *nex.RMCMessage) {
	return func(c *nex.Connection, req *nex.RMCMessage) {
		fmt.Printf("[AW %s] pid=%d proto=%#x method=%d call=%d\n", tag, c.PID, req.Protocol, req.Method, req.CallID)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func requireSignedToken() bool {
	v := os.Getenv("NEXTENDO_REQUIRE_SIGNED_TOKEN")
	return v == "1" || v == "true"
}
