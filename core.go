package trp

import (
	"log"
	"net"
	"os"
	"sync"
	"time"
)

var LogLevel = 1
var BufferSize = 4096

var PBChan2ConnLogger = log.New(os.Stdout, "[PB][Chan ---> Conn] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var PBConn2ChanLogger = log.New(os.Stdout, "[PB][Conn ---> Chan] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var Conn2ChanLogger = log.New(os.Stdout, "[LF][Conn ---> Chan] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var Chan2ConnLogger = log.New(os.Stdout, "[LF][Chan ---> Conn] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var CommonLogger = log.New(os.Stdout, "[Common] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)

type App struct {
	*Multiplexer
	WorkerGroup WorkerGroup
}

func (a *App) Run() {
	a.Multiplexer.Run()
}

func NewServer(conn net.Conn) *App {
	local2Remote := make(chan []byte, 10)
	wg := &ServerWorkerGroup{
		CommonWorkerGroup{workers: map[string]*Forwarder{}, muxChan: local2Remote},
	}
	multiplexer := NewMultiplexer(local2Remote, wg, conn)
	return &App{
		Multiplexer: multiplexer,
		WorkerGroup: wg,
	}
}

func NewClient(conn net.Conn, connectFunc func() net.Conn) *App {
	local2Remote := make(chan []byte, 10)
	wg := &ClientWorkerGroup{
		CommonWorkerGroup{workers: map[string]*Forwarder{}, muxChan: local2Remote},
		connectFunc,
	}
	multiplexer := NewMultiplexer(local2Remote, wg, conn)
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
	wg          WorkerGroup
	destroyLock sync.Once
}

func NewMultiplexer(muxChan <-chan []byte, wg WorkerGroup, conn net.Conn) *Multiplexer {
	return &Multiplexer{
		conn:      conn,
		inputChan: muxChan,
		wg:        wg,
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
			mp.wg.Forward(frame.Id, frame.Type, frame.Data)
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
		mp.wg.Destroy() // propagate to bridge
	})
}

// Forwarder create bridge between local port connection net.Conn and Channel
//
// one connection, one shared channel, one dedicated channel, and two goroutine
//
// functions: read conn, write to fromMux. read dedicatedReadChan, write to conn
type Forwarder struct {
	Id            string //forwarder's identifier
	conn          net.Conn
	muxChan       chan<- []byte
	dedicatedChan chan []byte
	destroyLock   sync.Once
	alive         bool
}

func (w *Forwarder) Run() {
	go w.conn2Chan()
	w.chan2Conn()
}

// Destroy just destroy worker and resource
func (w *Forwarder) destroy(propagate bool) {
	w.destroyLock.Do(func() {
		w.alive = false
		if propagate {
			//propagate: local connection closed, should notify remote to close same id worker
			CommonLogger.Printf("[%s] local conn closed, destroy and propagate", w.Id)
			newBytes := make([]byte, 32)
			copy(newBytes, w.Id)
			newBytes[16] = 0x1
			w.muxChan <- newBytes
			//how to remove worker from workers?
			//and consume all unsent msg, discard it
			for {
				select {
				case <-w.dedicatedChan:
				default:
					return
				}
			}
		} else {
			//receive remote chan close signal, to close local connection
			CommonLogger.Printf("[%s] remote chan closed, destroy", w.Id)
			_ = w.conn.Close()
		}
	})
}

// Conn2Chan read data from connection. then write to shard channel.
func (w *Forwarder) conn2Chan() {
	for {
		byteSlice := make([]byte, BufferSize+32)
		readLen, err := w.conn.Read(byteSlice[32:])
		if err == nil {
			byteSlice = byteSlice[:32+readLen]
			copy(byteSlice, w.Id)
			byteSlice[16] = 0x0
			WriteIntTo8Bytes(byteSlice[17:25], len(byteSlice))
			w.muxChan <- byteSlice
			LogBytes(Conn2ChanLogger, byteSlice)
		} else {
			w.destroy(true)
			return
		}
	}
}

// Chan2Conn read data from dedicated chan, then write to connection
func (w *Forwarder) chan2Conn() {
	for byteSlice := range w.dedicatedChan {
		_ = w.conn.SetWriteDeadline(time.Now().Add(time.Second * 2))
		_, err := w.conn.Write(byteSlice)
		if err != nil {
			//connection error or timeout
			w.destroy(true)
			return
		} else {
			LogBytes(Chan2ConnLogger, byteSlice)
		}
	}
	//remote chan close
	w.destroy(false)
}

type WorkerGroup interface {
	Forward(id string, fType FrameType, data []byte)
	Destroy()
}

type CommonWorkerGroup struct {
	muxChan chan []byte
	workers map[string]*Forwarder
}

func (wg *CommonWorkerGroup) Create(id string, conn net.Conn) *Forwarder {
	CommonLogger.Printf("[%s] worker created", id)
	newChan := make(chan []byte, 10)
	worker := &Forwarder{
		Id:            id,
		muxChan:       wg.muxChan,
		dedicatedChan: newChan,
		conn:          conn,
		destroyLock:   sync.Once{},
		alive:         true,
	}
	wg.workers[worker.Id] = worker
	return worker
}

func (wg *CommonWorkerGroup) Destroy() {
	old := wg.workers
	wg.workers = make(map[string]*Forwarder)
	for _, worker := range old {
		if worker.alive {
			close(worker.dedicatedChan) //close
		}
	}
}

type ServerWorkerGroup struct {
	CommonWorkerGroup
}

func (swg *ServerWorkerGroup) Forward(id string, fType FrameType, data []byte) {
	switch fType {
	case CLOSE:
		worker, exist := swg.workers[id]
		delete(swg.workers, id)
		if exist {
			close(worker.dedicatedChan) //close
		}
	case DATA:
		worker := swg.workers[id]
		if worker == nil {
			return
		}
		if worker.alive {
			LogBytes(PBConn2ChanLogger, data)
			worker.dedicatedChan <- data
		} else {
			delete(swg.workers, id)
		}
	}
}

type ClientWorkerGroup struct {
	CommonWorkerGroup
	connectFunc func() net.Conn
}

func (cwg *ClientWorkerGroup) Forward(id string, fType FrameType, data []byte) {
	switch fType {
	case CLOSE:
		worker, exist := cwg.workers[id]
		delete(cwg.workers, id)
		if exist {
			close(worker.dedicatedChan) //close
		}
	case DATA:
		worker := cwg.workers[id]
		if worker == nil {
			worker = cwg.Create(id, cwg.connectFunc())
			go worker.Run()
		}
		if worker.alive {
			LogBytes(PBConn2ChanLogger, data)
			worker.dedicatedChan <- data
		} else {
			delete(cwg.workers, id)
		}
	}
}
