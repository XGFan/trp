package trp

import (
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var logLevel = 1
var bufferSize = 4096
var id int32 = 0

var pbChan2ConnLogger = log.New(os.Stdout, "[PB][Chan ---> Conn] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var pbConn2ChanLogger = log.New(os.Stdout, "[PB][Conn ---> Chan] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var conn2ChanLogger = log.New(os.Stdout, "[LF][Conn ---> Chan] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var chan2ConnLogger = log.New(os.Stdout, "[LF][Chan ---> Conn] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var commonLogger = log.New(os.Stdout, "[Common] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)

// Worker create bridge between local port connection net.Conn and Channel
//
// one connection, one shared channel, one dedicated channel, and two goroutine
//
// functions: read Conn, write to shared channel, read dedicated channel write to Conn
type Worker struct {
	Id                string
	Conn              net.Conn
	SharedWriteChan   chan<- Frame
	DedicatedReadChan chan []byte
	Alive             bool
	destroyLock       sync.Once
}

func (w *Worker) Run() {
	go w.forwardConnection2Channel()
	go w.forwardChannel2Connection()
}

// Destroy just destroy worker and resource
func (w *Worker) Destroy(propagate bool) {
	w.destroyLock.Do(func() {
		w.Alive = false
		if propagate {
			//propagate: local connection closed, should notify remote to close same id worker
			commonLogger.Printf("[%s] local Conn closed, destroy and propagate", w.Id)
			w.SharedWriteChan <- Frame{Id: w.Id}
			//remove worker from workers
			//and consume all unsent msg, discard it
			for {
				select {
				case <-w.DedicatedReadChan:
				default:
					commonLogger.Printf("[%s] peacefully destroyed", w.Id)
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

//forwardConnection2Channel read data from connection. then write to shard channel.
func (w *Worker) forwardConnection2Channel() {
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

// forwardChannel2Connection read data from dedicated chan, then write to connection
func (w *Worker) forwardChannel2Connection() {
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

// Multiplexer create bridge between C/S net.Conn and ChannelGroup
//
// one connection, one shared channel, a group of workers, two goroutine
//
// functions: read Conn, write to dedicated worker's channel, read shared channel write to Conn
type Multiplexer struct {
	net.Conn
	ChannelGroup ChannelGroup
	destroyLock  sync.Once
	DestroyHook  func()
}

func NewMultiplexer(conn net.Conn, sv *Supervisor) *Multiplexer {
	return &Multiplexer{
		Conn:         conn,
		ChannelGroup: sv,
	}
}

func (mp *Multiplexer) Run() {
	go mp.Conn2Chan()
	go mp.Chan2Conn()
}

// Conn2Chan read data from connection, then write it to dedicated channel
func (mp *Multiplexer) Conn2Chan() {
	buf := make([]byte, 0, bufferSize)
	for {
		byteSlice := make([]byte, bufferSize+32)
		readLen, err := mp.Read(byteSlice)
		if err != nil {
			pbConn2ChanLogger.Printf("read from Conn fail: %v", err)
			break
		}
		byteSlice = byteSlice[:readLen]
		frames, remain := ParseAll(&SliceLink[byte]{
			Head: buf,
			Tail: byteSlice,
		})
		for _, frame := range frames {
			switch frame.Type {
			case DATA:
				mp.ChannelGroup.WriteChannel(frame.Id, frame.Data)
			case CLOSE:
				mp.ChannelGroup.CloseChannel(frame.Id)
			}
		}
		buf = remain
	}
	mp.Destroy()
}

// Chan2Conn read data from shared channel, then write it to connection
func (mp *Multiplexer) Chan2Conn() {
	for frame := range mp.ChannelGroup.GetChannel() {
		newBytes := Assemble(&frame)
		size, err := mp.Write(newBytes)
		if frame.Data != nil {
			size, err = mp.Write(frame.Data)
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
		mp.ChannelGroup.Destroy()
	})
}

// Supervisor manage a group of worker
type Supervisor struct {
	connFunc   func() net.Conn
	ttlCache   *TTLCache
	autoCreate bool
	sharedChan chan Frame
	workers    sync.Map
}

func NewServerSupervisor() *Supervisor {
	return &Supervisor{
		ttlCache:   NewTTLCache(time.Second * 10),
		sharedChan: make(chan Frame, 10),
		workers:    sync.Map{},
	}
}

func NewClientSupervisor(f func() net.Conn) *Supervisor {
	return &Supervisor{
		connFunc:   f,
		ttlCache:   NewTTLCache(time.Second * 10),
		autoCreate: true,
		sharedChan: make(chan Frame, 10),
		workers:    sync.Map{},
	}
}

func (s *Supervisor) Destroy() {
	close(s.sharedChan)
	s.workers.Range(func(key, value any) bool {
		value.(*Worker).Destroy(false)
		return true
	})
}

func (s *Supervisor) autoCreateWorker(id string) {
	if !s.ttlCache.Filter(id) {
		return
	}
	worker, exist := s.workers.Load(id)
	if exist {
		return
	}
	conn := s.connFunc()
	if conn != nil {
		worker = s.CreateWorker(id, conn)
		go worker.(*Worker).Run()
	}
}

func (s *Supervisor) NewWorker(conn net.Conn) *Worker {
	newId := atomic.AddInt32(&id, 1)
	worker := s.CreateWorker(fmt.Sprintf("%016d", newId), conn)
	return worker
}

func (s *Supervisor) CreateWorker(id string, conn net.Conn) *Worker {
	commonLogger.Printf("[%s] worker created", id)
	newChan := make(chan []byte, 1)
	worker := &Worker{
		Id:                id,
		SharedWriteChan:   s.sharedChan,
		DedicatedReadChan: newChan,
		Conn:              conn,
		Alive:             true,
		destroyLock:       sync.Once{},
	}
	s.workers.Store(worker.Id, worker)
	return worker
}

func (s *Supervisor) GetChannel() chan Frame {
	return s.sharedChan
}

func (s *Supervisor) CloseWorker(id string) {
	worker, ok := s.workers.Load(id)
	if ok {
		s.workers.Delete(id)
		worker.(*Worker).Destroy(false)
	}
}

func (s *Supervisor) WriteChannel(id string, data []byte) {
	if s.autoCreate {
		s.autoCreateWorker(id)
	}
	worker, exist := s.workers.Load(id)
	if exist {
		if worker.(*Worker).Alive {
			LogBytes(pbConn2ChanLogger, data)
			worker.(*Worker).DedicatedReadChan <- data
		} else {
			s.workers.Delete(id)
		}
	} else {
		//if s.ttlCache.Filter(id) { //only notify remote once to close connection
		//	pbConn2ChanLogger.Printf("worker [%s] not exist", id)
		//	go func() {
		//		s.sharedChan <- Frame{Id: id, Type: CLOSE}
		//	}()
		//}
	}
}

func (s *Supervisor) CloseChannel(id string) {
	worker, ok := s.workers.Load(id)
	if ok {
		worker.(*Worker).Destroy(false)
	}
}

type ChannelGroup interface {
	GetChannel() chan Frame
	WriteChannel(id string, data []byte)
	CloseChannel(id string)
	Destroy()
}
