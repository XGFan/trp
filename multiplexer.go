package trp

import (
	"net"
	"sync"
)

// Multiplexer create bridge between C/S net.Conn and WorkerGroup
//
// one connection, one shared channel, a group of workers, two goroutine
//
// functions: read conn, write to dedicated worker's channel. read muxChan, write to conn
type Multiplexer struct {
	conn        net.Conn
	Forwarders  WorkerGroup //TODO, could be private?
	muxChan     <-chan Frame
	destroyLock sync.Once
}

func NewClientMultiplexer(conn net.Conn, f func() net.Conn) *Multiplexer {
	muxChan := make(chan Frame, 10)
	return &Multiplexer{
		conn:       conn,
		muxChan:    muxChan,
		Forwarders: NewClientWorkerGroup(muxChan, f),
	}
}

func NewServerMultiplexer(conn net.Conn) *Multiplexer {
	muxChan := make(chan Frame, 10)
	return &Multiplexer{
		conn:       conn,
		muxChan:    muxChan,
		Forwarders: NewServerWorkerGroup(muxChan),
	}
}

// Conn2Chan read data from connection, then write it to dedicated channel
func (mp *Multiplexer) Conn2Chan() {
	buf := make([]byte, 0, BufferSize)
	for {
		byteSlice := make([]byte, BufferSize+32)
		readLen, err := mp.conn.Read(byteSlice)
		if err != nil {
			PBConn2ChanLogger.Printf("read from conn fail: %v", err)
			break
		}
		frames, remain := ParseAll(&LinkSlice[byte]{
			Head: buf,
			Tail: byteSlice[:readLen],
		})
		for _, frame := range frames {
			switch frame.Type {
			case DATA:
				mp.Forwarders.Forward(frame.Id, frame.Data)
			case CLOSE:
				mp.Forwarders.Close(frame.Id)
			}
		}
		buf = remain
	}
	mp.Destroy()
}

// Chan2Conn read data from shared channel, then write it to connection
func (mp *Multiplexer) Chan2Conn() {
	for frame := range mp.muxChan {
		header := AssembleHeader(&frame)
		size, err := mp.conn.Write(header)
		if frame.Data != nil {
			size, err = mp.conn.Write(frame.Data)
		}
		if err != nil {
			PBChan2ConnLogger.Printf("write to conn fail %d: %v", size, err)
			return
		} else {
			LogBytes(PBChan2ConnLogger, frame.Data)
		}
	}
	mp.Destroy()
}

func (mp *Multiplexer) Destroy() {
	mp.destroyLock.Do(func() {
		PBConn2ChanLogger.Print("remote closed connection, clean up")
		_ = mp.conn.Close()
		mp.Forwarders.Destroy() //propagate to forwarders
	})
}
