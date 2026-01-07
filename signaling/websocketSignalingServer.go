package signaling

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	qp2p "github.com/BrownNPC/QuicP2P"
	"github.com/BrownNPC/QuicP2P/internal"
	"github.com/coder/websocket"
	"github.com/go4org/hashtriemap"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// Serverside implementation of the Websocket Signaling Server that supports Trickle ICE.
type guestConn = *websocket.Conn
type hostConn = *websocket.Conn
type WebsocketSignalingServer struct {
	opts websocket.AcceptOptions
	// map Room Id to host connection. Allowing guests to send messages.
	hosts hashtriemap.HashTrieMap[qp2p.RoomId, hostConn]
	// Map from Guest's ID to connection. Allowing Host to lookup.
	guests hashtriemap.HashTrieMap[qp2p.GuestID, guestConn]
	Mux    *http.ServeMux
	log    *slog.Logger
}

// Uses Default logger if logger is nil.
// RoomIdGen can be nil. It will use the default Id generator.
func NewWebsocketSignalingServer(log *slog.Logger, opts websocket.AcceptOptions) *WebsocketSignalingServer {
	if log == nil {
		log = slog.Default()
	}
	s := new(WebsocketSignalingServer)
	s.log = log
	s.opts = opts
	s.Mux = new(http.ServeMux)
	s.Mux.HandleFunc("POST /host", s.host)
	s.Mux.HandleFunc("POST /join/{roomId}", s.host)
	return s
}

// POST /join/{roomId}
func (s *WebsocketSignalingServer) join(w http.ResponseWriter, r *http.Request) {
	const timeout = time.Second * 5 // Close if writes take longer than this

	// roomId is passed from path /join/{roomId}
	roomId := qp2p.RoomId(r.PathValue("roomId"))
	// close connection if room does not exist.
	hConn, ok := s.hosts.Load(roomId)
	if !ok {
		return
	}

	// accept guest websocket.
	gConn, err := websocket.Accept(w, r, &s.opts)
	if err != nil {
		s.log.Debug("Failed to accept host", "error", err)
		return
	}
	// incase it leaks somehow
	defer gConn.CloseNow()

	// randomly generated guest id
	var guestId qp2p.GuestID = uuid.New()
	// loaded from GuestAuth message.
	var guestUfrag, guestPwd string

	// expect guest to send GuestAuth message right after it connects.
	authMsg, err := ReadMsg(gConn, timeout)

	// check for errors before reading message.
	if err != nil { // error while reading message.
		gConn.Close(websocket.StatusInvalidFramePayloadData, "failed to read message")
		s.log.Debug("join: Failed to read GuestAuth message", "error", err)
		return
		//if invalid message type
	} else if authMsg.Type != GuestAuth {
		gConn.Close(websocket.StatusPolicyViolation, fmt.Sprintf("Expected GuestAuth message. Got %s", authMsg.Type))
		return
	}

	// Load ufrag and pwd from GuestAuth msg.
	guestUfrag = authMsg.Ufrag
	guestPwd = authMsg.Pwd

	// Tell the host that a guest has joined.
	err = msgGuestJoined(hConn, timeout, guestId, guestUfrag, guestPwd)
	if err != nil {
		s.log.Debug("Failed to write Msg Guest Joined", "error", err)
		gConn.Close(websocket.StatusInternalError, "failed to write message")
		return
	}
	// Ping loop
	go func() {
		for {
			time.Sleep(timeout / 5 * 2) // 2/5 of timeout
			ctx, cancel := context.WithTimeout(context.Background(), timeout/5*3 /*3/5 of timeout*/)
			err := gConn.Ping(ctx)
			cancel()
			if err != nil {
				return
			}
		}
	}()
	// connected to room. map guest id to connetion. So host can access.
	s.guests.Store(guestId, gConn)
	defer s.guests.Delete(guestId)
	// tell the host that the guest has disconnected from the signaling server.
	defer msgGuestDisconnected(hConn, timeout, guestId)
	lim := rate.NewLimiter(10, 20)
	for {
		if !lim.Allow() {
			gConn.Close(websocket.StatusPolicyViolation, "rate limit")
			return
		}
		msg, err := ReadMsg(gConn, timeout)
		if err != nil {
			return
		}
		if msg.Type == IceCandidate {
			WriteMsg(hConn, msg, timeout)
		}
	}
}

// POST /host
func (s *WebsocketSignalingServer) host(w http.ResponseWriter, r *http.Request) {
	const timeout = time.Second * 5 // Close if writes take longer than this

	hConn, err := websocket.Accept(w, r, &s.opts)
	if err != nil {
		s.log.Debug("Failed to accept host", "error", err)
		return
	}

	roomId := internal.GenerateUniqueRoomID(s.isUnique)
	s.hosts.Store(roomId, hConn)

	// Tell the host that room has been created.
	if err = msgRoomCreated(hConn, timeout, roomId); err != nil {
		hConn.Close(websocket.StatusInternalError, "Failed to write RoomCreated message")
		s.log.Debug("failed to send msg RoomCreated", "error", err)
		return
	}

	// TODO: disconnect guests.
	defer s.hosts.Delete(roomId) // delete after connection closed.

	// Ping loop
	go func() {
		for {
			time.Sleep(timeout / 5 * 2) // 2/5 of timeout
			ctx, cancel := context.WithTimeout(context.Background(), timeout/5*3 /*3/5 of timeout*/)
			err := hConn.Ping(ctx)
			cancel()
			if err != nil {
				return
			}
		}
	}()
	connectedGuests := make([]qp2p.GuestID, 0)
	defer func() { // kick connected guests.
		for _, guestId := range connectedGuests {
			gConn, ok := s.guests.Load(guestId)
			if !ok {
				continue
			}
			MsgKickGuest(gConn, timeout/5, guestId, "Host is offline.")
			gConn.Close(websocket.StatusGoingAway, "Host is offline")
		}
	}()
	lim := rate.NewLimiter(5, 20)
	for {
		if !lim.Allow() {

		}
		msg, err := ReadMsg(hConn, timeout)
		if err != nil {
			break
		}
		// forward to guest
		if msg.Type == HostAuth {
			gConn, ok := s.guests.Load(msg.GuestId)
			if !ok {
				s.log.Debug("HostAuth message invalid guest id, guest not found", "id", msg.GuestId)
				continue
			}
			connectedGuests = append(connectedGuests, msg.GuestId)
			// 5 messages per second per guest
			lim.SetLimit(rate.Limit(len(connectedGuests) * 5))
			lim.SetBurst(int(lim.Limit()) * 2)

			go WriteMsg(gConn, msg, timeout)
			// forward ICE candidate to Guest
		} else if msg.Type == IceCandidate {
			gConn, ok := s.guests.Load(msg.GuestId)
			if !ok {
				s.log.Debug("IceCandidate message invalid guest id, guest not found", "id", msg.GuestId)
				continue
			}
			go WriteMsg(gConn, msg, timeout)
		}
	}
}

// Returns false if host with roomId exists.
func (s *WebsocketSignalingServer) isUnique(roomId qp2p.RoomId) bool {
	if _, ok := s.hosts.Load(roomId); ok { // roomId is used?
		return false // not unique.
	}
	return true // is unique.
}
