package internal

import (
	"crypto/rand"

	qp2p "github.com/BrownNPC/QuicP2P"
)

func SixCharRoomID() qp2p.RoomId {
	return qp2p.RoomId(rand.Text()[:6])
}

func GenerateUniqueRoomID(isUnique func(roomId qp2p.RoomId) bool) qp2p.RoomId {
	id := SixCharRoomID()
	for !isUnique(id) {
		id = SixCharRoomID()
	}
	return id
}
