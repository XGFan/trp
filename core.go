package trp

import (
	"log"
	"net"
	"os"
	"sync"
	"time"
)

var logLevel = 1
var bufferSize = 4096

var pbChan2ConnLogger = log.New(os.Stdout, "[PB][Chan ---> Conn] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var pbConn2ChanLogger = log.New(os.Stdout, "[PB][Conn ---> Chan] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var conn2ChanLogger = log.New(os.Stdout, "[LF][Conn ---> Chan] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var chan2ConnLogger = log.New(os.Stdout, "[LF][Chan ---> Conn] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var commonLogger = log.New(os.Stdout, "[Common] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)

// Forwarder create bridge between local port connection net.Conn and Channel
//
// one connection, one shared channel, one dedicated channel, and two goroutine
//
// functions: read Conn, write to shared channel, read dedicated channel write to Conn
type Forwarder struct {
	Id                string
	Conn              net.Conn
	SharedWriteChan   chan<- Frame
	DedicatedReadChan chan []byte
	Alive             bool
	destroyLock       sync.Once
}

func (w *Forwarder) Run() {
	go w.Conn2Chan()
	go w.Chan2Conn()
}

// Destroy just destroy worker and resource
func (w *Forwarder) Destroy(propagate bool) {
	w.destroyLock.Do(func() {
		w.Alive = false
		if propagate {
			//propagate: local connection closed, should notify remote to close same id worker
			commonLogger.Printf("[%s] local Conn closed, destroy and propagate", w.Id)
			w.SharedWriteChan <- Frame{Id: w.Id, Type: CLOSE}
			//remove worker from workers
			//and consume all unsent msg, discard it
			for {
				select {
				case <-w.DedicatedReadChan:
				default:
					return
				}
			}
		} else {
			//receive remote chan close signal, to close local connection
			commonLogger.Printf("[%s] remote chan closed, destroy", w.Id)
			_ = w.Conn.Close()
		}
	})
}

// Conn2Chan read data from connection. then write to shard channel.
func (w *Forwarder) Conn2Chan() {
	for {
		byteSlice := make([]byte, bufferSize)
		readLen, err := w.Conn.Read(byteSlice)
		if err == nil {
			byteSlice = byteSlice[:readLen]
			w.SharedWriteChan <- Frame{
				Id:   w.Id,
				Data: byteSlice,
			}
			LogBytes(conn2ChanLogger, byteSlice)
		} else {
			w.Destroy(true)
			return
		}
	}
}

// Chan2Conn read data from dedicated chan, then write to connection
func (w *Forwarder) Chan2Conn() {
	for byteSlice := range w.DedicatedReadChan {
		_ = w.Conn.SetWriteDeadline(time.Now().Add(time.Second * 2))
		_, err := w.Conn.Write(byteSlice)
		if err != nil {
			//connection error or timeout
			w.Destroy(true)
			return
		} else {
			LogBytes(chan2ConnLogger, byteSlice)
		}
	}
	//remote chan close
	w.Destroy(false)
}

// Multiplexer create bridge between C/S net.Conn and WorkerGroup
//
// one connection, one shared channel, a group of workers, two goroutine
//
// functions: read Conn, write to dedicated worker's channel, read shared channel write to Conn
type Multiplexer struct {
	Conn        net.Conn
	Forwarders  WorkerGroup
	MuxChan     <-chan Frame
	destroyLock sync.Once
	DestroyHook func()
}

func NewClientMultiplexer(conn net.Conn, f func() net.Conn) *Multiplexer {
	muxChan := make(chan Frame, 10)
	return &Multiplexer{
		Conn:       conn,
		MuxChan:    muxChan,
		Forwarders: NewClientWorkerGroup(muxChan, f),
	}
}

func NewServerMultiplexer(conn net.Conn) *Multiplexer {
	muxChan := make(chan Frame, 10)
	return &Multiplexer{
		Conn:       conn,
		MuxChan:    muxChan,
		Forwarders: NewServerWorkerGroup(muxChan),
	}
}

// Conn2Chan read data from connection, then write it to dedicated channel
func (mp *Multiplexer) Conn2Chan() {
	buf := make([]byte, 0, bufferSize)
	for {
		byteSlice := make([]byte, bufferSize+32)
		readLen, err := mp.Conn.Read(byteSlice)
		if err != nil {
			pbConn2ChanLogger.Printf("read from Conn fail: %v", err)
			break
		}
		byteSlice = byteSlice[:readLen]
		frames, remain := ParseAll(&LinkSlice[byte]{
			Head: buf,
			Tail: byteSlice,
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
	for frame := range mp.MuxChan {
		newBytes := Assemble(&frame)
		size, err := mp.Conn.Write(newBytes)
		if frame.Data != nil {
			size, err = mp.Conn.Write(frame.Data)
		}
		if err != nil {
			pbChan2ConnLogger.Printf("write to Conn fail %d: %v", size, err)
			return
		} else {
			LogBytes(pbChan2ConnLogger, frame.Data)
		}
	}
	mp.Destroy()
}

func (mp *Multiplexer) Destroy() {
	mp.destroyLock.Do(func() {
		pbConn2ChanLogger.Print("remote closed connection, clean up")
		_ = mp.Conn.Close()
		mp.Forwarders.Destroy()
	})
}

type Supervisor struct {
	ttlCache *TTLCache
	muxChan  chan Frame
	workers  sync.Map
}

func (s *Supervisor) Forward(id string, data []byte) {
	worker, exist := s.workers.Load(id)
	if exist {
		if worker.(*Forwarder).Alive {
			LogBytes(pbConn2ChanLogger, data)
			worker.(*Forwarder).DedicatedReadChan <- data
		} else {
			s.workers.Delete(id)
		}
	} else {
		//if s.ttlCache.Filter(id) { //only notify remote once to close connection
		//	pbConn2ChanLogger.Printf("worker [%s] not exist", id)
		//	go func() {
		//		s.MuxChan <- Frame{Id: id, Type: CLOSE}
		//	}()
		//}
	}
}

func (s *Supervisor) Close(id string) {
	worker, ok := s.workers.Load(id)
	if ok {
		worker.(*Forwarder).Destroy(false)
	}
}

func (s *Supervisor) Destroy() {
	close(s.muxChan)
	s.workers.Range(func(key, value any) bool {
		value.(*Forwarder).Destroy(false)
		return true
	})
}

func (s *Supervisor) Create(id string, conn net.Conn) {
	commonLogger.Printf("[%s] worker created", id)
	newChan := make(chan []byte, 1)
	worker := &Forwarder{
		Id:                id,
		SharedWriteChan:   s.muxChan,
		DedicatedReadChan: newChan,
		Conn:              conn,
		Alive:             true,
		destroyLock:       sync.Once{},
	}
	s.workers.Store(worker.Id, worker)
	worker.Run()
}

type ClientSupervisor struct {
	Supervisor
	connectFunc func() net.Conn
}

func NewServerWorkerGroup(muxChan chan Frame) WorkerGroup {
	return &Supervisor{
		ttlCache: NewTTLCache(time.Second * 10),
		muxChan:  muxChan,
		workers:  sync.Map{},
	}
}

func NewClientWorkerGroup(muxChan chan Frame, connectFunc func() net.Conn) WorkerGroup {
	return &ClientSupervisor{
		Supervisor: Supervisor{
			ttlCache: NewTTLCache(time.Second * 10),
			muxChan:  muxChan,
			workers:  sync.Map{},
		},
		connectFunc: connectFunc,
	}
}

func (s *ClientSupervisor) autoCreateWorker(id string) {
	if !s.ttlCache.Filter(id) {
		return
	}
	_, exist := s.workers.Load(id)
	if exist {
		return
	}
	conn := s.connectFunc()
	if conn != nil {
		s.Create(id, conn)
	}
}

func (s *ClientSupervisor) Forward(id string, data []byte) {
	s.autoCreateWorker(id)
	s.Supervisor.Forward(id, data)
}

// WorkerGroup manage a group of worker
type WorkerGroup interface {
	Create(id string, conn net.Conn)
	Forward(id string, data []byte)
	Close(id string)
	Destroy()
}
