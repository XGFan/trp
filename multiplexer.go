package trp

import (
	"net"
	"sync"
)

type App struct {
	*Multiplexer
	WorkerGroup
}

func NewServer(conn net.Conn) *App {
	muxChan := make(chan Frame, 10)
	wg := NewServerWorkerGroup(muxChan)
	multiplexer := NewMultiplexer(muxChan, conn, wg)
	return &App{
		Multiplexer: multiplexer,
		WorkerGroup: wg,
	}
}

func NewClient(conn net.Conn, connectFunc func() net.Conn) *App {
	muxChan := make(chan Frame, 10)
	wg := NewClientWorkerGroup(muxChan, connectFunc)
	multiplexer := NewMultiplexer(muxChan, conn, wg)
	return &App{
		Multiplexer: multiplexer,
		WorkerGroup: wg,
	}
}

// Multiplexer create bridge between C/S net.Conn and WorkerGroup
//
// one connection, one shared channel, a group of workers, two goroutine
//
// functions: read conn, write to dedicated worker's channel. read muxChan, write to conn
type Multiplexer struct {
	conn        net.Conn
	workers     WorkerGroup
	muxChan     <-chan Frame
	destroyLock sync.Once
}

func NewMultiplexer(muxChan chan Frame, conn net.Conn, wg WorkerGroup) *Multiplexer {
	return &Multiplexer{
		conn:    conn,
		muxChan: muxChan,
		workers: wg,
	}
}

func (mp *Multiplexer) Run() {
	go mp.Conn2Chan()
	mp.Chan2Conn()
}

// Conn2Chan read data from connection, then write it to dedicated channel
func (mp *Multiplexer) Conn2Chan() {
	buf := make([]byte, 0, BufferSize)
	for {
		byteSlice := make([]byte, BufferSize+32)
		readLen, err := mp.conn.Read(byteSlice)
		if err != nil {
			PBConn2ChanLogger.Printf("read from conn fail: %v, close Multiplexer", err)
			break
		}
		frames, remain := ParseAll(&LinkSlice[byte]{
			Head: buf,
			Tail: byteSlice[:readLen],
		})
		for _, frame := range frames {
			switch frame.Type {
			case DATA:
				mp.workers.Forward(frame.Id, frame.Data)
			case CLOSE:
				mp.workers.Close(frame.Id)
			}
		}
		buf = remain
	}
	mp.Destroy(false) //when read error from connection, just close all
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
	//who has the power to close muxChan ?
	mp.Destroy(false) //muxChan has been closed, just close all
}

func (mp *Multiplexer) Destroy(propagate bool) {
	mp.destroyLock.Do(func() {
		PBConn2ChanLogger.Print("remote closed connection, clean up")
		_ = mp.conn.Close()
		mp.workers.Destroy() //propagate to forwarders
	})
}
