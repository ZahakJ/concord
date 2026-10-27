package meshtastic

import (
	"errors"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// The handful of Meshtastic client-API messages this link speaks, encoded by
// hand with protowire against meshtastic/protobufs mesh.proto. Field numbers
// below are the upstream ones; anything not listed is skipped on decode, so a
// newer firmware adding fields changes nothing here.

const (
	// BroadcastAddr is NODENUM_BROADCAST: every node on the channel.
	BroadcastAddr = 0xFFFFFFFF
	// PortPrivateApp is PortNum.PRIVATE_APP, the port upstream reserves for
	// applications that are not in its registry.
	PortPrivateApp = 256
	// DataPayloadLen is Constants.DATA_PAYLOAD_LEN: the most a Data.payload
	// may carry.
	DataPayloadLen = 233
	// maxProto bounds any message on the stream (MAX_TO_FROM_RADIO_SIZE).
	maxProto = 512
)

// ToRadio fields.
const (
	toPacket       = 1
	toWantConfigID = 3
	toDisconnect   = 4
	toHeartbeat    = 7
)

// FromRadio fields.
const (
	fromID             = 1
	fromPacket         = 2
	fromMyInfo         = 3
	fromNodeInfo       = 4
	fromConfigComplete = 7
	fromRebooted       = 8
	fromQueueStatus    = 11
)

// MeshPacket fields.
const (
	mpFrom     = 1
	mpTo       = 2
	mpChannel  = 3
	mpDecoded  = 4
	mpID       = 6
	mpRxSNR    = 8
	mpHopLimit = 9
	mpWantAck  = 10
	mpRxRSSI   = 12
	mpHopStart = 15
)

// Data fields.
const (
	dataPortnum = 1
	dataPayload = 2
)

// MeshPacket is the subset of meshtastic.MeshPacket Concord uses. Decoded is
// false for a packet the node could not decrypt (MeshPacket.encrypted), which
// carries nothing for us.
type MeshPacket struct {
	From, To uint32
	Channel  uint32
	ID       uint32
	HopLimit uint32
	HopStart uint32
	WantAck  bool
	RxSNR    float32
	RxRSSI   int32
	Decoded  bool
	PortNum  uint32
	Payload  []byte
}

// NodeInfo is meshtastic.NodeInfo{num, user.is_licensed, user.long_name}.
type NodeInfo struct {
	Num        uint32
	LongName   string
	IsLicensed bool
}

// QueueStatus is the node's transmit-queue report after each packet.
type QueueStatus struct {
	Res      int32
	Free     uint32
	MaxLen   uint32
	PacketID uint32
}

// ToRadio is one client→node message; exactly one field is meaningful.
type ToRadio struct {
	Packet       *MeshPacket
	WantConfigID uint32
	Disconnect   bool
	Heartbeat    bool
}

// FromRadio is one node→client message.
type FromRadio struct {
	ID               uint32
	Packet           *MeshPacket
	MyNodeNum        uint32 // set when the message was my_info
	HasMyInfo        bool
	NodeInfo         *NodeInfo
	ConfigCompleteID uint32
	HasConfigDone    bool
	Rebooted         bool
	QueueStatus      *QueueStatus
}

var errProto = errors.New("meshtastic: malformed protobuf")

func appendVarintField(b []byte, num protowire.Number, v uint64) []byte {
	b = protowire.AppendTag(b, num, protowire.VarintType)
	return protowire.AppendVarint(b, v)
}

func appendFixed32Field(b []byte, num protowire.Number, v uint32) []byte {
	b = protowire.AppendTag(b, num, protowire.Fixed32Type)
	return protowire.AppendFixed32(b, v)
}

func appendBytesField(b []byte, num protowire.Number, v []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, v)
}

func boolU(v bool) uint64 {
	if v {
		return 1
	}
	return 0
}

// Marshal encodes a MeshPacket. Zero values are omitted as proto3 does.
func (p *MeshPacket) Marshal() []byte {
	var b []byte
	if p.From != 0 {
		b = appendFixed32Field(b, mpFrom, p.From)
	}
	if p.To != 0 {
		b = appendFixed32Field(b, mpTo, p.To)
	}
	if p.Channel != 0 {
		b = appendVarintField(b, mpChannel, uint64(p.Channel))
	}
	if p.Decoded {
		var d []byte
		if p.PortNum != 0 {
			d = appendVarintField(d, dataPortnum, uint64(p.PortNum))
		}
		if len(p.Payload) > 0 {
			d = appendBytesField(d, dataPayload, p.Payload)
		}
		b = appendBytesField(b, mpDecoded, d)
	}
	if p.ID != 0 {
		b = appendFixed32Field(b, mpID, p.ID)
	}
	if p.RxSNR != 0 {
		b = appendFixed32Field(b, mpRxSNR, math.Float32bits(p.RxSNR))
	}
	if p.HopLimit != 0 {
		b = appendVarintField(b, mpHopLimit, uint64(p.HopLimit))
	}
	if p.WantAck {
		b = appendVarintField(b, mpWantAck, 1)
	}
	if p.RxRSSI != 0 {
		b = appendVarintField(b, mpRxRSSI, uint64(int64(p.RxRSSI)))
	}
	if p.HopStart != 0 {
		b = appendVarintField(b, mpHopStart, uint64(p.HopStart))
	}
	return b
}

// Marshal encodes a ToRadio.
func (t *ToRadio) Marshal() []byte {
	var b []byte
	switch {
	case t.Packet != nil:
		b = appendBytesField(b, toPacket, t.Packet.Marshal())
	case t.WantConfigID != 0:
		b = appendVarintField(b, toWantConfigID, uint64(t.WantConfigID))
	case t.Disconnect:
		b = appendVarintField(b, toDisconnect, 1)
	case t.Heartbeat:
		b = appendBytesField(b, toHeartbeat, nil)
	}
	return b
}

// Marshal encodes a FromRadio (the simulator's side of the wire).
func (f *FromRadio) Marshal() []byte {
	var b []byte
	if f.ID != 0 {
		b = appendVarintField(b, fromID, uint64(f.ID))
	}
	switch {
	case f.Packet != nil:
		b = appendBytesField(b, fromPacket, f.Packet.Marshal())
	case f.HasMyInfo:
		b = appendBytesField(b, fromMyInfo, appendVarintField(nil, 1, uint64(f.MyNodeNum)))
	case f.NodeInfo != nil:
		var u []byte
		if f.NodeInfo.LongName != "" {
			u = appendBytesField(u, 2, []byte(f.NodeInfo.LongName))
		}
		if f.NodeInfo.IsLicensed {
			u = appendVarintField(u, 6, 1)
		}
		n := appendVarintField(nil, 1, uint64(f.NodeInfo.Num))
		n = appendBytesField(n, 2, u)
		b = appendBytesField(b, fromNodeInfo, n)
	case f.HasConfigDone:
		b = appendVarintField(b, fromConfigComplete, uint64(f.ConfigCompleteID))
	case f.Rebooted:
		b = appendVarintField(b, fromRebooted, 1)
	case f.QueueStatus != nil:
		q := f.QueueStatus
		var qb []byte
		if q.Res != 0 {
			qb = appendVarintField(qb, 1, uint64(int64(q.Res)))
		}
		qb = appendVarintField(qb, 2, uint64(q.Free))
		qb = appendVarintField(qb, 3, uint64(q.MaxLen))
		if q.PacketID != 0 {
			qb = appendVarintField(qb, 4, uint64(q.PacketID))
		}
		b = appendBytesField(b, fromQueueStatus, qb)
	}
	return b
}

// field is one decoded protobuf field.
type field struct {
	num protowire.Number
	typ protowire.Type
	u   uint64 // varint / fixed32 / fixed64 value
	b   []byte // bytes value (aliases the input)
}

// fields walks a message, calling fn for every field. Groups and malformed
// input are errors; unknown field numbers are passed through for fn to ignore.
func fields(b []byte, fn func(field) error) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return errProto
		}
		b = b[n:]
		f := field{num: num, typ: typ}
		switch typ {
		case protowire.VarintType:
			f.u, n = protowire.ConsumeVarint(b)
		case protowire.Fixed32Type:
			var v uint32
			v, n = protowire.ConsumeFixed32(b)
			f.u = uint64(v)
		case protowire.Fixed64Type:
			f.u, n = protowire.ConsumeFixed64(b)
		case protowire.BytesType:
			f.b, n = protowire.ConsumeBytes(b)
		default:
			return errProto
		}
		if n < 0 {
			return errProto
		}
		b = b[n:]
		if err := fn(f); err != nil {
			return err
		}
	}
	return nil
}

// want returns errProto when a known field arrives with the wrong wire type —
// the cheapest way to refuse garbage that happens to parse.
func want(f field, t protowire.Type) error {
	if f.typ != t {
		return errProto
	}
	return nil
}

// UnmarshalMeshPacket decodes a MeshPacket.
func UnmarshalMeshPacket(b []byte) (*MeshPacket, error) {
	p := &MeshPacket{}
	err := fields(b, func(f field) error {
		switch f.num {
		case mpFrom, mpTo, mpID:
			if err := want(f, protowire.Fixed32Type); err != nil {
				return err
			}
			v := uint32(f.u)
			switch f.num {
			case mpFrom:
				p.From = v
			case mpTo:
				p.To = v
			default:
				p.ID = v
			}
		case mpChannel:
			p.Channel = uint32(f.u)
		case mpHopLimit:
			p.HopLimit = uint32(f.u)
		case mpHopStart:
			p.HopStart = uint32(f.u)
		case mpWantAck:
			p.WantAck = f.u != 0
		case mpRxRSSI:
			p.RxRSSI = int32(f.u)
		case mpRxSNR:
			if err := want(f, protowire.Fixed32Type); err != nil {
				return err
			}
			p.RxSNR = math.Float32frombits(uint32(f.u))
		case mpDecoded:
			if err := want(f, protowire.BytesType); err != nil {
				return err
			}
			p.Decoded = true
			return fields(f.b, func(d field) error {
				switch d.num {
				case dataPortnum:
					p.PortNum = uint32(d.u)
				case dataPayload:
					if err := want(d, protowire.BytesType); err != nil {
						return err
					}
					p.Payload = append([]byte(nil), d.b...)
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// UnmarshalToRadio decodes a ToRadio (the simulator's side of the wire).
func UnmarshalToRadio(b []byte) (*ToRadio, error) {
	t := &ToRadio{}
	err := fields(b, func(f field) error {
		switch f.num {
		case toPacket:
			if err := want(f, protowire.BytesType); err != nil {
				return err
			}
			p, err := UnmarshalMeshPacket(f.b)
			if err != nil {
				return err
			}
			t.Packet = p
		case toWantConfigID:
			t.WantConfigID = uint32(f.u)
		case toDisconnect:
			t.Disconnect = f.u != 0
		case toHeartbeat:
			t.Heartbeat = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// UnmarshalFromRadio decodes a FromRadio.
func UnmarshalFromRadio(b []byte) (*FromRadio, error) {
	r := &FromRadio{}
	err := fields(b, func(f field) error {
		switch f.num {
		case fromID:
			r.ID = uint32(f.u)
		case fromPacket:
			if err := want(f, protowire.BytesType); err != nil {
				return err
			}
			p, err := UnmarshalMeshPacket(f.b)
			if err != nil {
				return err
			}
			r.Packet = p
		case fromMyInfo:
			if err := want(f, protowire.BytesType); err != nil {
				return err
			}
			r.HasMyInfo = true
			return fields(f.b, func(m field) error {
				if m.num == 1 {
					r.MyNodeNum = uint32(m.u)
				}
				return nil
			})
		case fromNodeInfo:
			if err := want(f, protowire.BytesType); err != nil {
				return err
			}
			ni := &NodeInfo{}
			r.NodeInfo = ni
			return fields(f.b, func(n field) error {
				switch n.num {
				case 1:
					ni.Num = uint32(n.u)
				case 2:
					if err := want(n, protowire.BytesType); err != nil {
						return err
					}
					return fields(n.b, func(u field) error {
						switch u.num {
						case 2:
							ni.LongName = string(u.b)
						case 6:
							ni.IsLicensed = u.u != 0
						}
						return nil
					})
				}
				return nil
			})
		case fromConfigComplete:
			r.HasConfigDone = true
			r.ConfigCompleteID = uint32(f.u)
		case fromRebooted:
			r.Rebooted = f.u != 0
		case fromQueueStatus:
			if err := want(f, protowire.BytesType); err != nil {
				return err
			}
			q := &QueueStatus{}
			r.QueueStatus = q
			return fields(f.b, func(s field) error {
				switch s.num {
				case 1:
					q.Res = int32(s.u)
				case 2:
					q.Free = uint32(s.u)
				case 3:
					q.MaxLen = uint32(s.u)
				case 4:
					q.PacketID = uint32(s.u)
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}
