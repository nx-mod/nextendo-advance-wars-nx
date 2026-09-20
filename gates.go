package main

// Online GATES enforced at NEX login, the same rules as the other Nextendo game servers:
//   - A Nextendo account is REQUIRED (requireAccount): no account identity -> refused.
//   - Online = Nextendo accounts ONLY: a real console's NSA id that is not linked, or an
//     unreachable account server -> refused (fail-CLOSED: a non-Nextendo profile never gets in).
//   - #6 a verified e-mail is REQUIRED.
//   - #5 one place at a time (REAL presence, through monitoring).
//   - a disabled account -> refused.
//
// The account server (nextendo-account) owns the gate logic; the auth server calls
// /internal/online-check + /api/nsa and rejects the LoginEx on a block. FAIL-OPEN on an
// online-check network error (a transient hiccup must never lock everyone out of online);
// FAIL-CLOSED on an unverifiable NSA identity.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
	accountBaseURL = envOr("NEXTENDO_ACCOUNT_URL", "http://nextendo-account:8080")
	internalKey    = os.Getenv("NEXTENDO_INTERNAL_KEY")
	gateClient     = &http.Client{Timeout: 3 * time.Second}
)

// nextendoOnlineCheck asks nextendo-account whether this account may go online now.
// reason ∈ {"unknown","disabled","unverified","elsewhere",""}. Fail-OPEN on error.
func nextendoOnlineCheck(pid uint64, kind string) (bool, string) {
	body, _ := json.Marshal(map[string]any{"pid": pid, "kind": kind})
	req, err := http.NewRequest("POST", accountBaseURL+"/internal/online-check", bytes.NewReader(body))
	if err != nil {
		return true, ""
	}
	req.Header.Set("Content-Type", "application/json")
	if internalKey != "" {
		req.Header.Set("X-Internal-Key", internalKey)
	}
	resp, err := gateClient.Do(req)
	if err != nil {
		return true, "" // fail-open
	}
	defer resp.Body.Close()
	var out struct {
		Allow  bool   `json:"allow"`
		Reason string `json:"reason"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return true, ""
	}
	return out.Allow, out.Reason
}

// nsaStatus is the outcome of resolving an NSA id to a Nextendo account.
type nsaStatus int

const (
	nsaOK          nsaStatus = iota // NSA linked to a Nextendo account (valid pid)
	nsaUnknown                      // 404: no account owns this NSA -> non-Nextendo profile
	nsaUnreachable                  // account server unreachable -> identity cannot be verified
)

var (
	nsaCacheMu sync.Mutex
	nsaCache   = map[uint64]uint64{}
	// nsaNegCache remembers resolutions that FAILED (404 / unreachable). Without it, every
	// login attempt carrying an unknown NSA triggers an outgoing call to the SHARED account
	// service: a flood of bogus NSA ids on the (unauthenticated) auth port would turn into
	// amplification against the service ALL the games depend on. A short TTL keeps things
	// responsive when an account has just been linked.
	nsaNegCache = map[uint64]nsaNegEntry{}
	// nsaInflight caps SIMULTANEOUS /api/nsa calls: beyond it we answer
	// "unreachable" (fail-closed) without opening another connection.
	nsaInflight = make(chan struct{}, nsaMaxInflight)
)

type nsaNegEntry struct {
	status nsaStatus
	at     time.Time
}

const (
	nsaNegTTL      = 60 * time.Second
	nsaMaxInflight = 16
	nsaNegCacheMax = 4096
)

// resolveNSAtoPID maps an NSA id (a real Switch's baasUserID) to the Nextendo account's
// PID: (pid, nsaOK) if linked, (0, nsaUnknown) if no account owns it,
// (0, nsaUnreachable) if the account server cannot be reached. Positive results are cached.
func resolveNSAtoPID(nsa uint64) (uint64, nsaStatus) {
	nsaCacheMu.Lock()
	if pid, ok := nsaCache[nsa]; ok {
		nsaCacheMu.Unlock()
		return pid, nsaOK
	}
	if neg, ok := nsaNegCache[nsa]; ok && time.Since(neg.at) < nsaNegTTL {
		nsaCacheMu.Unlock()
		return 0, neg.status // recently resolved as unknown/unreachable: no new call
	}
	nsaCacheMu.Unlock()

	// Cap on simultaneous outgoing requests to the shared account service.
	select {
	case nsaInflight <- struct{}{}:
		defer func() { <-nsaInflight }()
	default:
		return 0, nsaUnreachable // saturated: fail-closed, without opening a connection
	}

	resp, err := gateClient.Get(fmt.Sprintf("%s/api/nsa?id=%d", accountBaseURL, nsa))
	if err != nil {
		rememberNSAFailure(nsa, nsaUnreachable)
		return 0, nsaUnreachable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		rememberNSAFailure(nsa, nsaUnknown)
		return 0, nsaUnknown
	}
	if resp.StatusCode != http.StatusOK {
		rememberNSAFailure(nsa, nsaUnreachable)
		return 0, nsaUnreachable
	}
	var out struct {
		PID uint64 `json:"pid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.PID == 0 {
		rememberNSAFailure(nsa, nsaUnreachable)
		return 0, nsaUnreachable
	}
	nsaCacheMu.Lock()
	nsaCache[nsa] = out.PID
	delete(nsaNegCache, nsa) // the account was just linked: the negative memory must not outlive it
	nsaCacheMu.Unlock()
	return out.PID, nsaOK
}

// rememberNSAFailure remembers a failed resolution for nsaNegTTL. The table is bounded:
// a flood of bogus NSA ids must not grow it without limit (it is emptied entirely at the
// cap rather than left to grow; entries are only worth 60 s anyway).
func rememberNSAFailure(nsa uint64, st nsaStatus) {
	nsaCacheMu.Lock()
	if len(nsaNegCache) >= nsaNegCacheMax {
		nsaNegCache = map[uint64]nsaNegEntry{}
	}
	nsaNegCache[nsa] = nsaNegEntry{status: st, at: time.Now()}
	nsaCacheMu.Unlock()
}
