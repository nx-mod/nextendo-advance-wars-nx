package main

import (
	"testing"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

func resetUnhandled() {
	unhandledMu.Lock()
	unhandled = map[[2]uint32]*unhandledStat{}
	unhandledMu.Unlock()
}

func TestNoteUnhandledCountsAndSamples(t *testing.T) {
	resetUnhandled()

	call := func(pid uint64, proto uint16, method uint32, body []byte) {
		noteUnhandled(&nex.Connection{PID: pid}, &nex.RMCMessage{Protocol: proto, Method: method, Body: body})
	}
	// same method twice, plus a second method once
	call(10, 0x6d, 99, []byte{1, 2, 3})
	call(11, 0x6d, 99, []byte{4, 5, 6})
	call(12, 0x70, 7, []byte{0xaa})

	got := snapshotUnhandled()
	if len(got) != 2 {
		t.Fatalf("distinct methods %d, want 2", len(got))
	}
	// most-called first
	if got[0].Method != 99 || got[0].Count != 2 || got[0].LastPID != 11 {
		t.Fatalf("top entry %+v", got[0])
	}
	// the sample body is the most recent call's, hex-encoded
	if got[0].BodyHex != "040506" {
		t.Fatalf("body sample %q", got[0].BodyHex)
	}
	if got[1].Method != 7 || got[1].Count != 1 {
		t.Fatalf("second entry %+v", got[1])
	}
}

func TestNoteUnhandledTruncatesBody(t *testing.T) {
	resetUnhandled()
	big := make([]byte, unhandledBodySample+50)
	for i := range big {
		big[i] = byte(i)
	}
	noteUnhandled(&nex.Connection{PID: 1}, &nex.RMCMessage{Protocol: 0x6d, Method: 1, Body: big})
	got := snapshotUnhandled()
	if len(got) != 1 || len(got[0].BodyHex) != unhandledBodySample*2 {
		t.Fatalf("body not truncated: %d hex chars", len(got[0].BodyHex))
	}
}
