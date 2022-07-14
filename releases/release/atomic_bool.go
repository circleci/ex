package release

import "sync/atomic"

type atomicBool struct {
	flag int32
}

func (b *atomicBool) Set(value bool) {
	var i int32
	if value {
		i = 1
	}
	atomic.StoreInt32(&(b.flag), i)
}

func (b *atomicBool) Get() bool {
	return atomic.LoadInt32(&(b.flag)) != 0
}
