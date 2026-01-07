package signaling

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"time"

	qp2p "github.com/BrownNPC/QuicP2P"
	"github.com/coder/websocket"
	"github.com/go4org/hashtriemap"
	"github.com/pion/ice/v4"
)

type signalingClientGuest struct {
}
type iceConn struct {
	*ice.Conn
	*ice.Agent
}
type signalingClientHost struct {
	opts   websocket.DialOptions
	guests hashtriemap.HashTrieMap[qp2p.GuestID, iceConn]
	log    *slog.Logger
	mux    ice.UDPMux
	hConn  hostConn
}

// WebsocketScheme is the websocket scheme (ws:// or wss://)
type WebsocketScheme string

const (
	// Websocket (non-secure)
	SchemeWs WebsocketScheme = "ws://"
	// Websocket secure
	SchemeWss WebsocketScheme = "wss://"
)
// host is the url address of the signaling server.
// 
// a nil log will use slog.Default().
func NewSignalingClientHost(host string, sceme WebsocketScheme, log *slog.Logger, opts websocket.DialOptions) (*signalingClientHost, error) {
	if log == nil {
		log = slog.Default()
	}

	const timeout = time.Second * 5
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	u := url.URL{
		Host:   host,
		Scheme: string(sceme),
		Path:   "host",
	}
	hConn, _, err := websocket.Dial(ctx, u.String(), &opts)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %v %v", u.String(), err)
	}

	pconn, err := net.ListenPacket("udp4", "0.0.0.0:")
	if err != nil {
		panic(err)
	}
	return &signalingClientHost{
		opts:   opts,
		guests: hashtriemap.HashTrieMap[qp2p.GuestID, iceConn]{},
		log:    log,
		mux:    ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: pconn}),
		hConn:  hConn,
	}, nil
}

// Listen blocks the thread
func (s *signalingClientHost) Listen(onConnection func(qp2p.GuestID, iceConn)) {
	const timeout = time.Second * 5
	defer s.hConn.Close(websocket.StatusGoingAway, "disconnecting")
	for {
		// Read message
		msg, err := ReadMsg(s.hConn, timeout)
		if err != nil {
			// unmarshalling error
			if !errors.Is(err, context.DeadlineExceeded) {
				s.log.Error("Failed to unmarshal message", "error", err)
				continue
			}
			s.log.Error("Read timed out. Server offline.", "error", err)
			return
		}
		switch msg.Type {
		case GuestJoined:
			// Guest has joined. Send Local credentials.
			// ice agent is used to get ice local credentials.
			agent, err := ice.NewAgentWithOptions(
				ice.WithUDPMux(s.mux),
				ice.WithNetworkTypes([]ice.NetworkType{ice.NetworkTypeUDP4}),
			)
			if err != nil {
				s.log.Error("Failed to create ice agent", "error", err)
				return
			}
			// set recieved remote credentials
			err = agent.SetRemoteCredentials(msg.Ufrag, msg.Pwd)
			if err != nil {
				s.log.Error("Failed to set remote credentials", "error", err)
				return
			}
			// generate local credentials.
			localUfrag, localPwd, err := agent.GetLocalUserCredentials()
			if err != nil {
				s.log.Error("Failed to get local user credentials", "error", err)
			}
			// send candidates to remote
			err = agent.OnCandidate(s.OnCandidate(msg.GuestId))
			if err != nil {
				panic(err)
			}
			// send local credentials to guest
			go MsgHostAuth(s.hConn, timeout, msg.GuestId, localUfrag, localPwd)
			err = agent.GatherCandidates()
			if err != nil {
				s.log.Error("failed to gather ice candidates", "erorr", err)
			}
			// store guest connection
			s.guests.Store(msg.GuestId, iceConn{Agent: agent})
			// dial concurrently
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
				defer cancel()

				conn, err := agent.Dial(ctx, msg.Ufrag, msg.Pwd)
				// dial failed. Kick guest from signaling server.
				if err != nil {
					s.log.Error("failed to open conn", "error", err)
					MsgKickGuest(s.hConn, timeout, msg.GuestId, "Connection failed")
					s.guests.Delete(msg.GuestId)
					return
				}
				iceConnection := iceConn{conn, agent}
				s.guests.Store(msg.GuestId, iceConnection)
				onConnection(msg.GuestId, iceConnection)
			}()
		case IceCandidate:
			iconn, ok := s.guests.Load(msg.GuestId)
			if !ok {
				s.log.Debug("invalid guest id for ice candidate", "id", msg.GuestId)
				continue
			}
			cand, err := ice.UnmarshalCandidate(msg.Candidate)
			if err != nil {
				s.log.Error("failed to unmarshall ice candidate", "error", err)
				continue
			}
			err = iconn.AddRemoteCandidate(cand)
			if err != nil {
				s.log.Error("failed to add remote candidate", "error", err)
			}
		case GuestDisconnected:
			iceConnection, existed := s.guests.LoadAndDelete(msg.GuestId)
			if !existed {
				continue
			}
			if iceConnection.Conn != nil {
				iceConnection.Conn.Close()
			}
		}
	}
}

func (s *signalingClientHost) SendIceCandidate(candidate string)
func (s *signalingClientHost) OnCandidate(guestId qp2p.GuestID) func(c ice.Candidate) {
	return func(c ice.Candidate) {
		const timeout = time.Second
		if c == nil {
			return
		}
		msgIceCandidate(s.hConn, timeout, guestId, c.Marshal())
	}
}

func (s *signalingClientGuest) SendAuth(ufrag, pwd string)
func (s *signalingClientGuest) OnRemoteAuth(func(ufrag, pwd string))
func (s *signalingClientGuest) SendIceCandidate(candidate string)
func (s *signalingClientGuest) SetOnIceCandidateRecieve(func(c ice.Candidate))
