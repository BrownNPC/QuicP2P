package qp2p

import (
	"github.com/google/uuid"
)

type RoomId string
type GuestID = uuid.UUID

type SignalingClientType bool

const (
	ClientTypeHost  SignalingClientType = true
	ClientTypeGuest SignalingClientType = false
)

