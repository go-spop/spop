package message

import (
	"sync"

	"github.com/go-spop/spop/payload/kv"
)

var messagePool = sync.Pool{
	New: func() any {
		return newMessage()
	},
}

type Message struct {
	Name string
	KV   *kv.KV
}

func newMessage() *Message {
	m := &Message{
		KV: kv.AcquireKV(),
	}

	return m
}

func AcquireMessage() *Message {
	m := messagePool.Get()
	if m == nil {
		return newMessage()
	}

	return m.(*Message)
}

func ReleaseMessage(m *Message) {
	m.Reset()
	messagePool.Put(m)
}

func (m *Message) Reset() {
	m.Name = ""

	kv.ReleaseKV(m.KV)
	m.KV = kv.AcquireKV()
}
