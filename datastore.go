package main

// DataStore (NEX protocol 0x73) for Advance Wars 1+2: Re-Boot Camp.
//
// Seen live (2026-09-14): right after Register, and again every time the online menu
// refreshes, the game sends ChangeMeta (38) on its persistence slot 0 with no dataId:
// name "<idtag>", dataType 4, metaBinary "Version=1\nForce=-1\n" then "Force=2". That is the
// player's ID tag ("Updating IDTag DataStore" in global-metadata.dat). Friends read each
// other's ID tags ("Getting IDTag DataStores"), and map share uses DataStore too, so the
// objects are kept for real rather than acknowledged and dropped.
//
// Objects live in memory and in one JSON file per object under AW_DATASTORE_DIR, so a
// restart keeps every ID tag and no write ever rewrites the whole store.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

const protocolDataStore uint16 = 0x73

// DataStore methods answered here (NintendoClients wiki, Data-Store-Protocol).
const (
	dsDeleteObject          = 4
	dsGetMeta               = 8
	dsSearchObject          = 12
	dsPostMetaBinary        = 21
	dsGetPersistenceInfo    = 29
	dsGetMetasMultipleParam = 36
	dsChangeMeta            = 38
	dsSearchObjectLight     = 46
)

// Result codes (kinnay errors.py).
const (
	resultDataStoreNotFound         uint32 = 0x00690004
	resultDataStorePermissionDenied uint32 = 0x00690005
	resultDataStoreInvalidArgument  uint32 = 0x00690001
)

// DataStoreChangeMetaParam.modifiesFlag bits.
const (
	modName          = 0x01
	modPermission    = 0x02
	modDelPermission = 0x04
	modPeriod        = 0x08
	modMetaBinary    = 0x10
	modTags          = 0x20
	modReferredCnt   = 0x40
	modDataType      = 0x80
	modStatus        = 0x100
)

// DataStorePermission.permission values.
const (
	permPublic  = 0
	permFriend  = 1
	permPrivate = 3
)

// GetMeta resultOption bit that asks for the metaBinary.
const resultOptionMetaBinary = 0x4

var datastoreDir = envOr("AW_DATASTORE_DIR", "datastore")

type dsPermission struct {
	Permission uint8    `json:"permission"`
	Recipients []uint64 `json:"recipients,omitempty"`
}

func (p *dsPermission) Levels() []nex.Level {
	return []nex.Level{{
		Save: func(out *nex.StreamOut) {
			out.U8(p.Permission)
			nex.WriteList(out, p.Recipients, func(o *nex.StreamOut, v uint64) { o.PID(v) })
		},
		Load: func(in *nex.StreamIn) {
			p.Permission = in.U8()
			p.Recipients = nex.ReadList(in, func(i *nex.StreamIn) uint64 { return i.PID() })
		},
	}}
}

type dsPersistenceTarget struct {
	Owner uint64
	Slot  uint16
}

func (t *dsPersistenceTarget) Levels() []nex.Level {
	return []nex.Level{{
		Save: func(out *nex.StreamOut) { out.PID(t.Owner); out.U16(t.Slot) },
		Load: func(in *nex.StreamIn) { t.Owner = in.PID(); t.Slot = in.U16() },
	}}
}

// dsObject is one stored object: the fields of DataStoreMetaInfo plus what the server keeps
// to itself (persistence slot, update password).
type dsObject struct {
	DataID         uint64       `json:"dataId"`
	Owner          uint64       `json:"ownerId"`
	Size           uint32       `json:"size"`
	Name           string       `json:"name"`
	DataType       uint16       `json:"dataType"`
	MetaBinary     []byte       `json:"metaBinary"`
	Permission     dsPermission `json:"permission"`
	DelPermission  dsPermission `json:"delPermission"`
	Created        time.Time    `json:"created"`
	Updated        time.Time    `json:"updated"`
	Period         uint16       `json:"period"`
	Status         uint8        `json:"status"`
	ReferredCnt    uint32       `json:"referredCnt"`
	ReferDataID    uint32       `json:"referDataId"`
	Flag           uint32       `json:"flag"`
	Tags           []string     `json:"tags"`
	PersistentSlot int32        `json:"persistenceSlot"` // -1 when not persistent
}

// metaInfo writes DataStoreMetaInfo. withBinary is false when the caller did not set the
// metaBinary result option.
func (o *dsObject) metaInfo(withBinary bool) nex.Structure {
	return structure(func(out *nex.StreamOut) {
		out.U64(o.DataID)
		out.PID(o.Owner)
		out.U32(o.Size)
		out.String(o.Name)
		out.U16(o.DataType)
		if withBinary {
			out.QBuffer(o.MetaBinary)
		} else {
			out.QBuffer(nil)
		}
		out.Add(&o.Permission)
		out.Add(&o.DelPermission)
		out.DateTime(nexDateTime(o.Created))
		out.DateTime(nexDateTime(o.Updated))
		out.U16(o.Period)
		out.U8(o.Status)
		out.U32(o.ReferredCnt)
		out.U32(o.ReferDataID)
		out.U32(o.Flag)
		out.DateTime(nexDateTime(o.Updated)) // referredTime
		out.DateTime(nexDateTime(o.Updated.Add(time.Duration(o.Period) * 24 * time.Hour)))
		nex.WriteList(out, o.Tags, func(s *nex.StreamOut, v string) { s.String(v) })
		out.U32(0) // ratings: none
	})
}

// emptyMetaInfo is the placeholder entry that goes with a NotFound result in list replies.
func emptyMetaInfo() nex.Structure {
	o := &dsObject{}
	return o.metaInfo(false)
}

// structure adapts a single-level save function to nex.Structure.
type structure func(out *nex.StreamOut)

func (f structure) Levels() []nex.Level { return []nex.Level{{Save: f}} }

// nexDateTime packs a time the NEX way: year<<26 | month<<22 | day<<17 | hour<<12 | min<<6 | sec.
func nexDateTime(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	t = t.UTC()
	return uint64(t.Year())<<26 | uint64(t.Month())<<22 | uint64(t.Day())<<17 |
		uint64(t.Hour())<<12 | uint64(t.Minute())<<6 | uint64(t.Second())
}

type dataStore struct {
	mu          sync.Mutex
	objects     map[uint64]*dsObject
	persistence map[dsPersistenceTarget]uint64
	nextID      uint64
}

func newDataStore() *dataStore {
	ds := &dataStore{
		objects:     map[uint64]*dsObject{},
		persistence: map[dsPersistenceTarget]uint64{},
		nextID:      100000,
	}
	ds.load()
	return ds
}

func (ds *dataStore) load() {
	files, _ := filepath.Glob(filepath.Join(datastoreDir, "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var o dsObject
		if err := json.Unmarshal(b, &o); err != nil || o.DataID == 0 {
			fmt.Printf("[AW DataStore] skipping %s: %v\n", f, err)
			continue
		}
		ds.objects[o.DataID] = &o
		if o.PersistentSlot >= 0 {
			ds.persistence[dsPersistenceTarget{o.Owner, uint16(o.PersistentSlot)}] = o.DataID
		}
		if o.DataID >= ds.nextID {
			ds.nextID = o.DataID + 1
		}
	}
	fmt.Printf("[AW DataStore] %d object(s) loaded from %s\n", len(ds.objects), datastoreDir)
}

// save writes one object's file; called with ds.mu held.
func (ds *dataStore) save(o *dsObject) {
	if err := os.MkdirAll(datastoreDir, 0o755); err != nil {
		fmt.Printf("[AW DataStore] save %d: %v\n", o.DataID, err)
		return
	}
	b, _ := json.MarshalIndent(o, "", "  ")
	path := filepath.Join(datastoreDir, strconv.FormatUint(o.DataID, 10)+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		err = os.Rename(tmp, path)
		if err != nil {
			fmt.Printf("[AW DataStore] save %d: %v\n", o.DataID, err)
		}
	}
}

func (ds *dataStore) remove(o *dsObject) {
	delete(ds.objects, o.DataID)
	if o.PersistentSlot >= 0 {
		delete(ds.persistence, dsPersistenceTarget{o.Owner, uint16(o.PersistentSlot)})
	}
	_ = os.Remove(filepath.Join(datastoreDir, strconv.FormatUint(o.DataID, 10)+".json"))
}

// find resolves a dataId or, when it is 0, a persistence target; called with ds.mu held.
func (ds *dataStore) find(dataID uint64, target dsPersistenceTarget) *dsObject {
	if dataID == 0 {
		id, ok := ds.persistence[target]
		if !ok {
			return nil
		}
		dataID = id
	}
	return ds.objects[dataID]
}

// readable applies the object's access permission to a reader. Friend and specified-friend
// objects are open to everyone: the friend graph lives in nextendo-account, not here.
func readable(o *dsObject, pid uint64) bool {
	if o.Owner == pid {
		return true
	}
	switch o.Permission.Permission {
	case permPrivate:
		return false
	case 2: // specified
		for _, r := range o.Permission.Recipients {
			if r == pid {
				return true
			}
		}
		return false
	}
	return true
}

func (ds *dataStore) handler() nex.RMCHandler {
	return func(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		switch req.Method {
		case dsChangeMeta:
			return ds.changeMeta(c, req)
		case dsPostMetaBinary:
			return ds.postMetaBinary(c, req)
		case dsGetMeta:
			return ds.getMeta(c, req)
		case dsGetMetasMultipleParam:
			return ds.getMetasMultipleParam(c, req)
		case dsSearchObject, dsSearchObjectLight:
			return ds.searchObject(c, req)
		case dsGetPersistenceInfo:
			return ds.getPersistenceInfo(c, req)
		case dsDeleteObject:
			return ds.deleteObject(c, req)
		}
		fmt.Printf("[AW DataStore] UNHANDLED pid=%d method=%d call=%d body=%x\n", c.PID, req.Method, req.CallID, req.Body)
		return nex.NewRMCError(c.Settings, protocolDataStore, req.CallID, nex.ResultCoreNotImplemented)
	}
}

func dsError(c *nex.Connection, req *nex.RMCMessage, code uint32) *nex.RMCMessage {
	return nex.NewRMCError(c.Settings, protocolDataStore, req.CallID, code)
}

func dsOK(c *nex.Connection, req *nex.RMCMessage, body []byte) *nex.RMCMessage {
	return nex.NewRMCSuccess(c.Settings, protocolDataStore, req.Method, req.CallID, body)
}

// changeMeta answers ChangeMeta (38). The game never posts its ID tag first: it changes the
// meta of persistence slot 0 directly, so a missing persistent object owned by the caller is
// created from the param (what the game asked to be stored is exactly what gets stored).
func (ds *dataStore) changeMeta(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	var (
		dataID, updatePassword uint64
		flags                  uint32
		name                   string
		perm, delPerm          dsPermission
		period                 uint16
		metaBinary             []byte
		tags                   []string
		referredCnt            uint32
		dataType               uint16
		status                 uint8
		target                 dsPersistenceTarget
	)
	in := nex.NewStreamIn(req.Body, c.Settings)
	in.Extract(loadOnly(func(s *nex.StreamIn) {
		dataID = s.U64()
		flags = s.U32()
		name = s.String()
		s.Extract(&perm)
		s.Extract(&delPerm)
		period = s.U16()
		metaBinary = append([]byte(nil), s.QBuffer()...)
		tags = nex.ReadList(s, func(i *nex.StreamIn) string { return i.String() })
		updatePassword = s.U64()
		referredCnt = s.U32()
		dataType = s.U16()
		status = s.U8()
		s.Extract(loadOnly(func(*nex.StreamIn) {})) // compareParam: not applied
		s.Extract(&target)
	}))
	if in.Err() != nil {
		fmt.Printf("[AW DataStore] ChangeMeta pid=%d unreadable: %v body=%x\n", c.PID, in.Err(), req.Body)
		return dsError(c, req, resultDataStoreInvalidArgument)
	}
	_ = updatePassword

	ds.mu.Lock()
	defer ds.mu.Unlock()
	o := ds.find(dataID, target)
	created := false
	if o == nil {
		if dataID != 0 || target.Owner != c.PID {
			fmt.Printf("[AW DataStore] ChangeMeta pid=%d dataId=%d target=%d/%d: not found\n", c.PID, dataID, target.Owner, target.Slot)
			return dsError(c, req, resultDataStoreNotFound)
		}
		now := time.Now()
		o = &dsObject{
			DataID: ds.nextID, Owner: c.PID, Created: now, Period: 90,
			Permission: dsPermission{Permission: permFriend}, DelPermission: dsPermission{Permission: permPrivate},
			PersistentSlot: int32(target.Slot),
		}
		ds.nextID++
		ds.objects[o.DataID] = o
		ds.persistence[target] = o.DataID
		created = true
	} else if o.Owner != c.PID {
		fmt.Printf("[AW DataStore] ChangeMeta pid=%d on %d owned by %d: denied\n", c.PID, o.DataID, o.Owner)
		return dsError(c, req, resultDataStorePermissionDenied)
	}

	if flags&modName != 0 {
		o.Name = name
	}
	if flags&modPermission != 0 {
		o.Permission = perm
	}
	if flags&modDelPermission != 0 {
		o.DelPermission = delPerm
	}
	if flags&modPeriod != 0 {
		o.Period = period
	}
	if flags&modMetaBinary != 0 {
		o.MetaBinary = metaBinary
	}
	if flags&modTags != 0 {
		o.Tags = tags
	}
	if flags&modReferredCnt != 0 {
		o.ReferredCnt = referredCnt
	}
	if flags&modDataType != 0 {
		o.DataType = dataType
	}
	if flags&modStatus != 0 {
		o.Status = status
	}
	o.Updated = time.Now()
	ds.save(o)
	fmt.Printf("[AW DataStore] ChangeMeta pid=%d dataId=%d slot=%d flags=%#x created=%v name=%q type=%d meta=%q\n",
		c.PID, o.DataID, o.PersistentSlot, flags, created, o.Name, o.DataType, o.MetaBinary)
	return dsOK(c, req, nil)
}

// postMetaBinary answers PostMetaBinary (21): a meta-only object, no upload.
func (ds *dataStore) postMetaBinary(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	o := &dsObject{Owner: c.PID, PersistentSlot: -1}
	var slot uint16
	var deleteLast, persistent bool
	in := nex.NewStreamIn(req.Body, c.Settings)
	in.Extract(loadOnly(func(s *nex.StreamIn) {
		o.Size = s.U32()
		o.Name = s.String()
		o.DataType = s.U16()
		o.MetaBinary = append([]byte(nil), s.QBuffer()...)
		s.Extract(&o.Permission)
		s.Extract(&o.DelPermission)
		o.Flag = s.U32()
		o.Period = s.U16()
		o.ReferDataID = s.U32()
		o.Tags = nex.ReadList(s, func(i *nex.StreamIn) string { return i.String() })
		// ratingInitParams: a list of structures; ID tags and maps carry none.
		if n := s.U32(); n != 0 {
			fmt.Printf("[AW DataStore] PostMetaBinary pid=%d has %d rating init param(s), ignored\n", c.PID, n)
			s.ReadAll()
			return
		}
		s.Extract(loadOnly(func(p *nex.StreamIn) {
			slot = p.U16()
			deleteLast = p.Bool()
		}))
		// persistenceSlotId 0xFFFF means "not persistent".
		persistent = slot != 0xFFFF
	}))
	if in.Err() != nil {
		fmt.Printf("[AW DataStore] PostMetaBinary pid=%d unreadable: %v body=%x\n", c.PID, in.Err(), req.Body)
		return dsError(c, req, resultDataStoreInvalidArgument)
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()
	now := time.Now()
	o.DataID, o.Created, o.Updated = ds.nextID, now, now
	ds.nextID++
	if persistent {
		target := dsPersistenceTarget{c.PID, slot}
		if old := ds.find(0, target); old != nil && deleteLast {
			ds.remove(old)
		}
		o.PersistentSlot = int32(slot)
		ds.persistence[target] = o.DataID
	}
	ds.objects[o.DataID] = o
	ds.save(o)
	fmt.Printf("[AW DataStore] PostMetaBinary pid=%d dataId=%d slot=%d name=%q type=%d meta=%q\n",
		c.PID, o.DataID, o.PersistentSlot, o.Name, o.DataType, o.MetaBinary)
	out := nex.NewStreamOut(c.Settings)
	out.U64(o.DataID)
	return dsOK(c, req, out.Bytes())
}

// getMetaParam reads DataStoreGetMetaParam.
func getMetaParam(s *nex.StreamIn) (dataID uint64, target dsPersistenceTarget, option uint8) {
	s.Extract(loadOnly(func(p *nex.StreamIn) {
		dataID = p.U64()
		p.Extract(&target)
		option = p.U8()
		_ = p.U64() // accessPassword
	}))
	return
}

// lookup resolves one GetMeta param for pid, returning the meta or a result code.
func (ds *dataStore) lookup(pid, dataID uint64, target dsPersistenceTarget) (*dsObject, uint32) {
	o := ds.find(dataID, target)
	if o == nil {
		return nil, resultDataStoreNotFound
	}
	if !readable(o, pid) {
		return nil, resultDataStorePermissionDenied
	}
	return o, 0
}

func (ds *dataStore) getMeta(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	in := nex.NewStreamIn(req.Body, c.Settings)
	dataID, target, option := getMetaParam(in)
	ds.mu.Lock()
	defer ds.mu.Unlock()
	o, code := ds.lookup(c.PID, dataID, target)
	fmt.Printf("[AW DataStore] GetMeta pid=%d dataId=%d target=%d/%d -> %#x\n", c.PID, dataID, target.Owner, target.Slot, code)
	if code != 0 {
		return dsError(c, req, code)
	}
	out := nex.NewStreamOut(c.Settings)
	out.Add(o.metaInfo(option&resultOptionMetaBinary != 0))
	return dsOK(c, req, out.Bytes())
}

func (ds *dataStore) getMetasMultipleParam(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	type param struct {
		dataID uint64
		target dsPersistenceTarget
		option uint8
	}
	in := nex.NewStreamIn(req.Body, c.Settings)
	params := nex.ReadList(in, func(s *nex.StreamIn) param {
		var p param
		p.dataID, p.target, p.option = getMetaParam(s)
		return p
	})
	ds.mu.Lock()
	defer ds.mu.Unlock()
	metas := make([]nex.Structure, len(params))
	results := make([]uint32, len(params))
	found := 0
	for i, p := range params {
		o, code := ds.lookup(c.PID, p.dataID, p.target)
		if code != 0 {
			metas[i], results[i] = emptyMetaInfo(), code|nex.ResultErrorMask
			continue
		}
		metas[i], results[i] = o.metaInfo(p.option&resultOptionMetaBinary != 0), resultSuccess
		found++
	}
	fmt.Printf("[AW DataStore] GetMetasMultipleParam pid=%d %d param(s) -> %d found\n", c.PID, len(params), found)
	out := nex.NewStreamOut(c.Settings)
	nex.WriteList(out, metas, func(s *nex.StreamOut, m nex.Structure) { s.Add(m) })
	nex.WriteList(out, results, func(s *nex.StreamOut, r uint32) { s.Result(r) })
	return dsOK(c, req, out.Bytes())
}

// resultSuccess is Core::Success as a NEX Result (the success bit pattern the core uses).
const resultSuccess uint32 = 0x00010001

// searchObject answers SearchObject (12) and SearchObjectLight (46): filter by owners and data
// types, newest update first, then the result range.
func (ds *dataStore) searchObject(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	var (
		owners    []uint64
		dataType  uint16
		dataTypes []uint16
		tags      []string
		rr        nex.ResultRange
		option    uint8
	)
	in := nex.NewStreamIn(req.Body, c.Settings)
	in.Extract(loadOnly(func(s *nex.StreamIn) {
		_ = s.U8() // searchTarget
		owners = nex.ReadList(s, func(i *nex.StreamIn) uint64 { return i.PID() })
		_ = s.U8()                                                           // ownerType
		_ = nex.ReadList(s, func(i *nex.StreamIn) uint64 { return i.U64() }) // destinationIds
		dataType = s.U16()
		for i := 0; i < 4; i++ {
			_ = s.DateTime() // created/updated after/before
		}
		_ = s.U32() // referDataId
		tags = nex.ReadList(s, func(i *nex.StreamIn) string { return i.String() })
		_ = s.U8() // resultOrderColumn
		_ = s.U8() // resultOrder
		s.Extract(&rr)
		option = s.U8()
		_ = s.U32()  // minimalRatingFrequency
		_ = s.Bool() // useCache
		if !s.EOF() {
			_ = s.Bool() // totalCountEnabled
			dataTypes = nex.ReadList(s, func(i *nex.StreamIn) uint16 { return i.U16() })
		}
	}))
	if in.Err() != nil {
		fmt.Printf("[AW DataStore] SearchObject pid=%d unreadable: %v body=%x\n", c.PID, in.Err(), req.Body)
		return dsError(c, req, resultDataStoreInvalidArgument)
	}

	wantOwner := map[uint64]bool{}
	for _, o := range owners {
		wantOwner[o] = true
	}
	wantType := map[uint16]bool{}
	for _, t := range dataTypes {
		wantType[t] = true
	}
	if dataType != 0xFFFF && len(dataTypes) == 0 {
		wantType[dataType] = true
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()
	var hits []*dsObject
	for _, o := range ds.objects {
		if len(wantOwner) > 0 && !wantOwner[o.Owner] {
			continue
		}
		if len(wantType) > 0 && !wantType[o.DataType] {
			continue
		}
		if !hasTags(o.Tags, tags) || !readable(o, c.PID) {
			continue
		}
		hits = append(hits, o)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Updated.After(hits[j].Updated) })
	total := len(hits)
	start := int(rr.Offset)
	if start > len(hits) {
		start = len(hits)
	}
	hits = hits[start:]
	if rr.Size != 0 && int(rr.Size) < len(hits) {
		hits = hits[:rr.Size]
	}
	fmt.Printf("[AW DataStore] SearchObject pid=%d owners=%v type=%d types=%v tags=%v range=%d+%d -> %d/%d\n",
		c.PID, owners, dataType, dataTypes, tags, rr.Offset, rr.Size, len(hits), total)

	out := nex.NewStreamOut(c.Settings)
	out.Add(structure(func(s *nex.StreamOut) {
		s.U32(uint32(total))
		nex.WriteList(s, hits, func(w *nex.StreamOut, o *dsObject) { w.Add(o.metaInfo(option&resultOptionMetaBinary != 0)) })
		s.U8(0) // totalCountType
	}))
	return dsOK(c, req, out.Bytes())
}

func hasTags(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (ds *dataStore) getPersistenceInfo(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	in := nex.NewStreamIn(req.Body, c.Settings)
	target := dsPersistenceTarget{Owner: in.PID(), Slot: in.U16()}
	ds.mu.Lock()
	id, ok := ds.persistence[target]
	ds.mu.Unlock()
	fmt.Printf("[AW DataStore] GetPersistenceInfo pid=%d target=%d/%d -> dataId=%d\n", c.PID, target.Owner, target.Slot, id)
	if !ok {
		return dsError(c, req, resultDataStoreNotFound)
	}
	out := nex.NewStreamOut(c.Settings)
	out.Add(structure(func(s *nex.StreamOut) { s.PID(target.Owner); s.U16(target.Slot); s.U64(id) }))
	return dsOK(c, req, out.Bytes())
}

func (ds *dataStore) deleteObject(c *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
	var dataID uint64
	in := nex.NewStreamIn(req.Body, c.Settings)
	in.Extract(loadOnly(func(s *nex.StreamIn) { dataID = s.U64(); _ = s.U64() }))
	ds.mu.Lock()
	defer ds.mu.Unlock()
	o := ds.objects[dataID]
	fmt.Printf("[AW DataStore] DeleteObject pid=%d dataId=%d found=%v\n", c.PID, dataID, o != nil)
	if o == nil {
		return dsError(c, req, resultDataStoreNotFound)
	}
	if o.Owner != c.PID {
		return dsError(c, req, resultDataStorePermissionDenied)
	}
	ds.remove(o)
	return dsOK(c, req, nil)
}

// loadOnly adapts a single-level load function to nex.Structure for StreamIn.Extract.
type loadOnly func(*nex.StreamIn)

func (f loadOnly) Levels() []nex.Level { return []nex.Level{{Load: f}} }
