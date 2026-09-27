package main

// Unhandled-RMC map.
//
// This server is still being mapped against the game: nextendo-nex implements
// the common protocols, and every method it does not answer falls through to the
// endpoint fallback, which logs one line and returns an empty success. That log
// is scrollback — easy to miss and gone on restart.
//
// noteUnhandled turns those fall-throughs into structured data: which
// protocol/method the game called, how many times, when, by whom, and a bounded
// sample of the request body. The dashboard surfaces it (/api/stats
// "unhandled"), so the exact remaining protocol surface is visible while a real
// console drives the server — the fastest way to know what to implement next.

import (
	"encoding/hex"
	"sort"
	"sync"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// unhandledBodySample bounds the stored request body (the full body is still in
// the server log).
const unhandledBodySample = 64

type unhandledStat struct {
	proto    uint16
	method   uint32
	count    int64
	last     time.Time
	lastPID  uint64
	lastBody []byte
}

var (
	unhandledMu sync.Mutex
	unhandled   = map[[2]uint32]*unhandledStat{} // {proto, method} -> stat
)

// noteUnhandled records one RMC the server did not implement. Call it from the
// endpoint fallback, for every call regardless of PID (pre-auth calls count too).
func noteUnhandled(c *nex.Connection, req *nex.RMCMessage) {
	key := [2]uint32{uint32(req.Protocol), req.Method}
	body := req.Body
	if len(body) > unhandledBodySample {
		body = body[:unhandledBodySample]
	}
	unhandledMu.Lock()
	s := unhandled[key]
	if s == nil {
		s = &unhandledStat{proto: req.Protocol, method: req.Method}
		unhandled[key] = s
	}
	s.count++
	s.last = time.Now()
	s.lastPID = c.PID
	s.lastBody = append([]byte(nil), body...)
	unhandledMu.Unlock()
}

type apiUnhandled struct {
	Name       string `json:"name"`
	Proto      uint16 `json:"proto"`
	Method     uint32 `json:"method"`
	Count      int64  `json:"count"`
	AgoSeconds int    `json:"agoSeconds"`
	LastPID    uint64 `json:"lastPid"`
	BodyHex    string `json:"bodyHex,omitempty"`
}

// snapshotUnhandled returns the recorded unhandled methods, most-called first,
// for the dashboard.
func snapshotUnhandled() []apiUnhandled {
	unhandledMu.Lock()
	out := make([]apiUnhandled, 0, len(unhandled))
	for _, s := range unhandled {
		out = append(out, apiUnhandled{
			Name:       rmcName(s.proto, s.method),
			Proto:      s.proto,
			Method:     s.method,
			Count:      s.count,
			AgoSeconds: int(time.Since(s.last).Seconds()),
			LastPID:    s.lastPID,
			BodyHex:    hex.EncodeToString(s.lastBody),
		})
	}
	unhandledMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}
