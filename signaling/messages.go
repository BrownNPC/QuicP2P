package signaling

import (
	"context"
	"fmt"
	"time"

	qp2p "github.com/BrownNPC/QuicP2P"
	"github.com/coder/websocket"
	"github.com/shamaton/msgpack/v2"
)

//go:generate stringer -type=MsgType
type MsgType int

const (
	Invalid MsgType = iota
	// Server -> Host Msg{RoomCreated: RoomId)
	//
	// This message is sent by the server right after the socket is opened.
	//
	// It contains the RoomId.
	RoomCreated
	// Guest -> Server Msg{GuestAuth: Ufrag,Pwd}
	//
	// This message is sent by the guest to the server right after the socket is opened.
	//
	// It contains Ufrag & Pwd (ICE credentials of the guest).
	GuestAuth
	// Server -> Host Msg{GuestJoined: GuestId,Ufrag,Pwd}
	//
	// A GuestJoined message is sent to the Host the first time a Guest joins the room.
	//
	// It contains the GuestId, Ufrag & Pwd (ICE credentials of the guest).
	GuestJoined
	// Host -> Server -> Guest Msg{HostAuth: GuestId,Ufrag,Pwd}
	//
	// This message is sent by the Host to the server after receiving the GuestAuth message.
	//
	// The server forwards the message to the Guest.
	//
	// It contains GuestId, Ufrag & Pwd (ICE credentials of the host).
	HostAuth
	// Guest -> Server Msg{IceCandidate: Candidate}
	//
	// Host  -> Server Msg{IceCandidate: GuestId,Candidate}
	//
	// The Guest or Host trickle their ICE Candidates to the server.
	//
	// The server forwards them to the recipient
	IceCandidate
	// Server -> Host Msg{GuestDisconnected: GuestId}
	//
	// This message is sent by the Server to the Host after the Guest has disconnected from the signaling server.
	//
	// It contains GuestId.
	GuestDisconnected
	// Host -> Server -> Guest Msg{KickGuest: GuestId,Reason "Kicked by host"}
	// Server -> Guest Msg{KickGuest: GuestId, Reason "Host is offline"}
	//
	// This message is sent by the Server to the Guest after the Host disconnects from the signaling server.
	//
	// It could also be sent by the Host to the Server and forwarded to the Guest if the Host decides to kick the Guest.
	//
	// It contains GuestId, and Reason (for the Kick).
	KickGuest
)

// Host -> Server POST /host
//
// Server -> Host Msg{RoomCreated: RoomId)
//
// Guest -> Server POST /join/{roomId}
//
// Guest -> Server Msg{GuestAuth: Ufrag,Pwd}
//
// Server -> Host Msg{GuestJoined: GuestId,Ufrag,Pwd}
//
// Host -> Server -> Guest Msg{HostAuth: GuestId,Ufrag,Pwd}
type Msg struct {
	Type       MsgType
	RoomId     qp2p.RoomId
	GuestId    qp2p.GuestID
	Ufrag, Pwd string
	Candidate  string
	Reason     string
}

// Server -> Host Msg{RoomCreated: RoomId)
//
// This message is sent by the server right after the socket is opened.
//
// It contains the RoomId.
func msgRoomCreated(conn hostConn, timeout time.Duration, roomId qp2p.RoomId) error {
	msg := Msg{
		Type:   RoomCreated,
		RoomId: roomId,
	}
	return WriteMsg(conn, msg, timeout)
}

// Guest -> Server Msg{GuestAuth: Ufrag,Pwd}
//
// This message is sent by the guest to the server right after the socket is opened.
//
// It contains Ufrag & Pwd (ICE credentials of the guest).
func MsgGuestAuth(conn guestConn, timeout time.Duration, ufrag, pwd string) error {
	msg := Msg{
		Type:  GuestAuth,
		Ufrag: ufrag,
		Pwd:   pwd,
	}
	return WriteMsg(conn, msg, timeout)
}

// Server -> Host Msg{GuestJoined: GuestId,Ufrag,Pwd}
//
// A GuestJoined message is sent to the Host the first time a Guest joins the room.
//
// It contains the GuestId, Ufrag & Pwd (ICE credentials of the guest).
func msgGuestJoined(conn hostConn, timeout time.Duration, id qp2p.GuestID, ufrag, pwd string) error {
	msg := Msg{
		Type:    GuestJoined,
		GuestId: id,
		Ufrag:   ufrag,
		Pwd:     pwd,
	}
	return WriteMsg(conn, msg, timeout)
}

// Host -> Server -> Guest Msg{HostAuth: GuestId,Ufrag,Pwd}
//
// This message is sent by the Host to the server after receiving the GuestAuth message.
//
// The server forwards the message to the Guest.
//
// It contains GuestId, Ufrag & Pwd (ICE credentials of the host).
func MsgHostAuth(conn hostConn, timeout time.Duration, GuestId qp2p.GuestID, ufrag, pwd string) error {
	msg := Msg{
		Type:    HostAuth,
		Ufrag:   ufrag,
		Pwd:     pwd,
		GuestId: GuestId,
	}
	return WriteMsg(conn, msg, timeout)
}

// Guest -> Server Msg{IceCandidate: Candidate}
//
// Host  -> Server Msg{IceCandidate: GuestId,Candidate}
//
// The Guest or Host trickle their ICE Candidates to the server.
//
// # The server forwards them to the recipient
//
// GuestId is ignored when Guest -> Server
func msgIceCandidate(conn *websocket.Conn, timeout time.Duration, GuestId qp2p.GuestID, Candidate string) error {
	msg := Msg{
		Type:      IceCandidate,
		Candidate: Candidate,
		GuestId:   GuestId,
	}
	return WriteMsg(conn, msg, timeout)
}

// Server -> Host Msg{GuestDisconnected: GuestId}
//
// This message is sent by the Server to the Host after the Guest has disconnected from the signaling server.
//
// It contains GuestId.
func msgGuestDisconnected(conn hostConn, timeout time.Duration, GuestId qp2p.GuestID) error {
	msg := Msg{
		Type:    GuestDisconnected,
		GuestId: GuestId,
	}
	return WriteMsg(conn, msg, timeout)
}

// Host -> Server -> Guest Msg{KickGuest: GuestId,Reason "Kicked by host"}
// Server -> Guest Msg{KickGuest: GuestId, Reason "Host is offline"}
//
// This message is sent by the Server to the Guest after the Host disconnects from the signaling server.
//
// It could also be sent by the Host to the Server and forwarded to the Guest if the Host decides to kick the Guest.
//
// It contains GuestId, and Reason (for the Kick).
func MsgKickGuest(conn hostConn, timeout time.Duration, GuestId qp2p.GuestID, Reason string) error {
	msg := Msg{
		Type:    KickGuest,
		GuestId: GuestId,
		Reason:  Reason,
	}
	return WriteMsg(conn, msg, timeout)
}

// Marshal Msg as array and write to Conn.
// Error if marshal or write fails.
func WriteMsg(conn *websocket.Conn, msg Msg, timeout time.Duration) error {
	// marshal Msg
	b, err := msgpack.MarshalAsArray(msg)
	if err != nil {
		return fmt.Errorf("signaling.writeMsg: failed to marshal %T %v", msg, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// write to socket, return if error or timeout.
	err = conn.Write(ctx, websocket.MessageBinary, b)
	if err != nil {
		return fmt.Errorf("signaling.writeMsg: failed to write %T %v", msg, err)
	}
	return nil
}

// Marshal Msg as array and write to Conn.
// Error if marshal or write fails.
func ReadMsg(conn *websocket.Conn, timeout time.Duration) (Msg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// read
	t, b, err := conn.Read(ctx)
	if err != nil {
		return Msg{}, fmt.Errorf("signaling.readMsg: %v", err)
	}
	// return error if message is not binary payload.
	if t != websocket.MessageBinary {
		return Msg{}, fmt.Errorf("signaling.readMsg: message type is not binary", err)
	}
	// unmarshal binary payload
	msg := new(Msg)
	err = msgpack.UnmarshalAsArray(b, msg)
	if err != nil {
		return Msg{}, fmt.Errorf("signaling.readMsg: failed to unmarshal message as array")
	}

	return *msg, nil
}
