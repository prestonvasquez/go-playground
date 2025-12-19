package examplepkg

import (
	"unsafe"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type ClientSession struct {
	SnapshotTime bson.Timestamp
}

type Session struct {
	clientSession *ClientSession
}

func NewSession() *Session {
	return &Session{
		clientSession: &ClientSession{
			SnapshotTime: bson.Timestamp{T: 1},
		},
	}
}

func (s *Session) ClientSessionPtr() *ClientSession {
	return s.clientSession
}

func (s *Session) ClientSession() ClientSession {
	return *s.clientSession
}

func (s *Session) ClientSessionMemoryAddr() uintptr {
	return uintptr((uintptr)(unsafe.Pointer(s.clientSession)))
}
