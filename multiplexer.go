package trp

import (
	"net"
	"sync"
)

type App struct {
	*Multiplexer
	*WorkerGroup
}

func (a *App) Run() {
	go a.Multiplexer.Run()
	a.WorkerGroup.Run()
}

func NewServer(conn net.Conn) *App {
	local2Remote := make(chan []byte, 10)
	remote2Local := make(chan Frame, 10)
	multiplexer := NewMultiplexer(local2Remote, remote2Local, conn)
	wg := NewServerWorkerGroup(local2Remote, remote2Local)
	return &App{
		Multiplexer: multiplexer,
		WorkerGroup: wg,
	}
}

func NewClient(conn net.Conn, connectFunc func() net.Conn) *App {
	local2Remote := make(chan []byte, 10)
	remote2Local := make(chan Frame, 10)
	multiplexer := NewMultiplexer(local2Remote, remote2Local, conn)
	wg := NewClientWorkerGroup(local2Remote, remote2Local, connectFunc)
	return &App{
		Multiplexer: multiplexer,
		WorkerGroup: wg,
	}
}

// Multiplexer create bridge between C/S net.Conn and WorkerGroup
//
// one connection, one shared channel, a group of workers, two goroutine
//
// functions: read conn, write to dedicated worker's channel. read fromMux, write to conn
type Multiplexer struct {
	conn        net.Conn
	inputChan   <-chan []byte
	outputChan  chan<- Frame
	destroyLock sync.Once
}

func NewMultiplexer(muxChan <-chan []byte, outputChan chan<- Frame, conn net.Conn) *Multiplexer {
	return &Multiplexer{
		conn:       conn,
		inputChan:  muxChan,
		outputChan: outputChan,
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
			mp.outputChan <- frame
		}
		buf = remain
	}
	mp.Destroy() //when read error from connection, just close all
}

// Chan2Conn read data from shared channel, then write it to connection
func (mp *Multiplexer) Chan2Conn() {
	for data := range mp.inputChan {
		size, err := mp.conn.Write(data)
		if err != nil {
			PBChan2ConnLogger.Printf("write to conn fail %d: %v", size, err)
			return
		} else {
			LogBytes(PBChan2ConnLogger, data)
		}
	}
	//who has the power to close fromMux ?
	mp.Destroy() //fromMux has been closed, just close all
}

func (mp *Multiplexer) Destroy() {
	mp.destroyLock.Do(func() {
		PBConn2ChanLogger.Print("remote closed connection, clean up")
		_ = mp.conn.Close()
		close(mp.outputChan) // propagate to bridge
	})
}
