package main

import (
	"encoding/hex"
	"testing"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// idTagChangeMeta is the ChangeMeta body the Switch sent on 2026-09-14 after Register:
// persistence slot 0 of pid 1800000003, name "<idtag>", dataType 4, "Version=1\nForce=-1\n".
const idTagChangeMeta = "019200000000000000000000009100000008003c69647461673e0000050000000300000000000500000003000000005a00130056657273696f6e3d310a466f7263653d2d310a00000000000000000000000000000000040000002a0000000000000001000000050000000300000000000500000003000000005a0000000000000000000000000000000a00000003d2496b000000000000"

func testDataStore(t *testing.T) *dataStore {
	t.Helper()
	datastoreDir = t.TempDir()
	return newDataStore()
}

func testConn(pid uint64) *nex.Connection {
	return &nex.Connection{PID: pid, Settings: nex.NewSwitchSettings("c001f85f", 40604)}
}

func TestChangeMetaCreatesTheIDTag(t *testing.T) {
	ds := testDataStore(t)
	body, _ := hex.DecodeString(idTagChangeMeta)
	c := testConn(1800000003)
	resp := ds.handler()(c, &nex.RMCMessage{Protocol: protocolDataStore, Method: dsChangeMeta, CallID: 14, Body: body})
	if resp.IsError {
		t.Fatalf("ChangeMeta refused: %#x", resp.Result)
	}
	o := ds.find(0, dsPersistenceTarget{1800000003, 0})
	if o == nil {
		t.Fatal("no object on persistence slot 0")
	}
	if o.Name != "<idtag>" || o.DataType != 4 || string(o.MetaBinary) != "Version=1\nForce=-1\n" || o.Period != 90 {
		t.Fatalf("stored %+v", o)
	}

	// A restart reloads it from disk.
	again := newDataStore()
	if got := again.find(0, dsPersistenceTarget{1800000003, 0}); got == nil || got.DataID != o.DataID {
		t.Fatalf("not reloaded: %+v", got)
	}
}

func TestChangeMetaOnSomeoneElsesSlotIsNotFound(t *testing.T) {
	ds := testDataStore(t)
	body, _ := hex.DecodeString(idTagChangeMeta)
	resp := ds.handler()(testConn(1800000004), &nex.RMCMessage{Protocol: protocolDataStore, Method: dsChangeMeta, Body: body})
	if !resp.IsError || resp.Result&^nex.ResultErrorMask != resultDataStoreNotFound {
		t.Fatalf("want NotFound, got %+v", resp)
	}
}

func TestFriendReadsTheIDTag(t *testing.T) {
	ds := testDataStore(t)
	body, _ := hex.DecodeString(idTagChangeMeta)
	ds.handler()(testConn(1800000003), &nex.RMCMessage{Protocol: protocolDataStore, Method: dsChangeMeta, Body: body})

	s := nex.NewSwitchSettings("c001f85f", 40604)
	out := nex.NewStreamOut(s)
	out.U32(2)
	for _, owner := range []uint64{1800000003, 1800000009} {
		out.Add(structure(func(p *nex.StreamOut) {
			p.U64(0)
			p.Add(&dsPersistenceTarget{owner, 0})
			p.U8(resultOptionMetaBinary)
			p.U64(0)
		}))
	}
	resp := ds.handler()(testConn(1800000004), &nex.RMCMessage{Protocol: protocolDataStore, Method: dsGetMetasMultipleParam, Body: out.Bytes()})
	if resp.IsError {
		t.Fatalf("refused: %#x", resp.Result)
	}

	in := nex.NewStreamIn(resp.Body, s)
	var names []string
	var metas []string
	nex.ReadList(in, func(i *nex.StreamIn) struct{} {
		i.Extract(loadOnly(func(m *nex.StreamIn) {
			_ = m.U64()
			_ = m.PID()
			_ = m.U32()
			names = append(names, m.String())
			_ = m.U16()
			metas = append(metas, string(m.QBuffer()))
			m.ReadAll()
		}))
		return struct{}{}
	})
	results := nex.ReadList(in, func(i *nex.StreamIn) uint32 { return i.Result() })
	if in.Err() != nil || len(names) != 2 || len(results) != 2 {
		t.Fatalf("reply unreadable: err=%v names=%v results=%v", in.Err(), names, results)
	}
	if names[0] != "<idtag>" || metas[0] != "Version=1\nForce=-1\n" || results[0]&nex.ResultErrorMask != 0 {
		t.Fatalf("first entry: %q %q %#x", names[0], metas[0], results[0])
	}
	if results[1] != resultDataStoreNotFound|nex.ResultErrorMask {
		t.Fatalf("second entry result %#x", results[1])
	}
}
