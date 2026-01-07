package qp2p

import "github.com/google/uuid"

type RoomId string
type GuestID = uuid.UUID

// A SignalingServerConnection is used for trickle ICE
type SignalingServerConnection interface {
	CreateRoom() (RoomId, GuestID)
	JoinRoom(RoomId) GuestID
}

