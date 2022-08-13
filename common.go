package trp

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var logLevel = 1
var bufferSize = 4096
var id int32 = 0

func init() {
	flag.IntVar(&logLevel, "d", 1, "debug")
}

var pbChan2ConnLogger = log.New(os.Stdout, "[PB][Chan ---> Conn] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var pbConn2ChanLogger = log.New(os.Stdout, "[PB][Conn ---> Chan] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var conn2ChanLogger = log.New(os.Stdout, "[LF][Conn ---> Chan] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var chan2ConnLogger = log.New(os.Stdout, "[LF][Chan ---> Conn] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
var commonLogger = log.New(os.Stdout, "[Common] ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)

func LogBytes(logger *log.Logger, bytes []byte) {
	switch logLevel {
	case 1:
		_ = logger.Output(2, fmt.Sprintf("Data size: %d", len(bytes)))
	case 2:
		for _, s := range strings.Split(string(bytes), "\n") {
			_ = logger.Output(2, s)
		}
	default:

	}
}

type FrameType int

const (
	DATA FrameType = iota
	CLOSE_CONNECTION
	BIND
)

func Assemble(frame *Frame) []byte {
	switch frame.Type {
	case DATA:
		newBytes := make([]byte, 32+len(frame.Data))
		copy(newBytes, frame.Id)
		newBytes[17] = 0x0
		copy(newBytes[18:26], Int64ToBytes(int64(len(newBytes))))
		copy(newBytes[32:], frame.Data)
		return newBytes
	case CLOSE_CONNECTION:
		newBytes := make([]byte, 32)
		copy(newBytes, frame.Id)
		newBytes[17] = 0x1
		return newBytes
	case BIND:
		newBytes := make([]byte, 32)
		copy(newBytes, frame.Id)
		newBytes[17] = 0x2
		copy(newBytes[18:20], frame.Data[:2])
		return newBytes
	}
	return nil
}

func ParseAll(buf []byte) ([]Frame, []byte) {
	ret := make([]Frame, 0)
	remain := Parse(buf, &ret)
	return ret, remain
}

//Parse data structure: id,identity,size
func Parse(buf []byte, frames *[]Frame) []byte {
	if len(buf) < 32 {
		return buf
	}
	id := string(buf[:16])
	switch buf[17] {
	case 0x0:
		totalLen := BytesToInt64(buf[18:26])
		if totalLen > int64(len(buf)) {
			return buf
		}
		*frames = append(*frames, Frame{
			Id:   id,
			Type: DATA,
			Data: buf[32:totalLen],
		})
		return Parse(buf[totalLen:], frames)
	case 0x1:
		*frames = append(*frames, Frame{
			Id:   id,
			Type: CLOSE_CONNECTION,
		})
		return Parse(buf[32:], frames)
	case 0x2:
		*frames = append(*frames, Frame{
			Id:   id,
			Type: BIND,
			Data: buf[18:20],
		})
		return Parse(buf[32:], frames)
	}
	return nil
}

type Frame struct {
	Id   string
	Type FrameType
	Data []byte
}

// Worker functions: read Conn, write to shared channel, read dedicated channel write to Conn
type Worker struct {
	Id        string
	Chain     *DuplexChain
	Conn      net.Conn
	CloseFunc func()
	o         sync.Once
}

// Destroy just destroy worker and resource
func (w *Worker) Destroy(propagate bool) {
	w.o.Do(func() {
		if propagate {
			//propagate: local connection closed, should notify remote to close same id worker
			commonLogger.Printf("[%s] local Conn closed, destroy and propagate", w.Id)
			w.Chain.Conn2Chan <- Frame{Id: w.Id}
			defer func() {
				//remove worker from workers
				//and receive all msg, discard it
				//close chan
			loop:
				for {
					select {
					case <-w.Chain.Chan2Conn:
					default:
						commonLogger.Printf("[%s] peacefully destroyed", w.Id)
						close(w.Chain.Chan2Conn)
						break loop
					}
				}
			}()
		} else {
			//receive remote chan close signal, to close local connection
			commonLogger.Printf("[%s] remote chan closed, destroy", w.Id)
			_ = w.Conn.Close()
		}
		w.CloseFunc()
	})
}

//Conn2Chan read data from connection. then write to shard channel.
func (w *Worker) Conn2Chan() {
	for {
		byteSlice := make([]byte, bufferSize)
		readLen, err := w.Conn.Read(byteSlice)
		if err == nil {
			byteSlice = byteSlice[:readLen]
			w.Chain.Conn2Chan <- Frame{
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
func (w *Worker) Chan2Conn() {
	for byteSlice := range w.Chain.Chan2Conn {
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

type DuplexChain struct {
	Chan2Conn chan []byte  //dedicated,read
	Conn2Chan chan<- Frame //shared,write
}

type PortBinding struct {
	net.Conn
	forwarder Forwarder
	notify    chan struct{}
}

func NewPortBinding(conn net.Conn, sv *Supervisor) *PortBinding {
	return &PortBinding{
		Conn:      conn,
		forwarder: sv,
		notify:    make(chan struct{}, 0),
	}
}

// Conn2Chan read data from connection, then write it to dedicated channel
func (pb *PortBinding) Conn2Chan() {
	buf := new(bytes.Buffer)
	for {
		byteSlice := make([]byte, bufferSize)
		readLen, err := pb.Read(byteSlice)
		if err != nil {
			pbConn2ChanLogger.Printf("read from Conn fail: %v", err)
			break
		}
		byteSlice = byteSlice[:readLen]
		newBytes := make([]byte, len(byteSlice)+buf.Len())
		copy(newBytes, buf.Bytes())
		copy(newBytes[buf.Len():], byteSlice)
		buf.Reset()
		frames, remain := ParseAll(newBytes)
		for _, frame := range frames {
			pb.forwarder.WriteToWorker(&frame)
		}
		buf.Write(remain)
	}
	pbConn2ChanLogger.Print("remote closed connection, clean up")
	pb.notify <- struct{}{}
	pb.forwarder.Destroy()
}

// Chan2Conn read data from shared channel, then write it to connection
func (pb *PortBinding) Chan2Conn() {
loop:
	for {
		select {
		case frame := <-pb.forwarder.ReadFromWorker():
			newBytes := Assemble(&frame)
			size, err := pb.Write(newBytes)
			if err != nil {
				pbChan2ConnLogger.Printf("write to Conn fail %d: %v", size, err)
				break loop
			} else {
				LogBytes(pbChan2ConnLogger, frame.Data)
			}
		case <-pb.notify:
			pbChan2ConnLogger.Printf("get notification from Conn2Chan, close goroutine")
			break loop
		}
	}
}

type Supervisor struct {
	connFunc   func() net.Conn
	ttlCache   *TTLCache
	autoCreate bool
	address    string
	Conn2Chan  chan Frame
	workers    sync.Map
	CloseFunc  func()
}

func NewServerSupervisor() *Supervisor {
	return &Supervisor{
		ttlCache:  NewTTLCache(time.Second * 10),
		Conn2Chan: make(chan Frame, 0),
		workers:   sync.Map{},
	}
}

func NewClientSupervisor(f func() net.Conn) *Supervisor {
	return &Supervisor{
		connFunc:   f,
		ttlCache:   NewTTLCache(time.Second * 10),
		autoCreate: true,
		Conn2Chan:  make(chan Frame, 0),
		workers:    sync.Map{},
	}
}

func (s *Supervisor) Destroy() {
	close(s.Conn2Chan)
	s.workers.Range(func(key, value any) bool {
		value.(*Worker).Destroy(false)
		return true
	})
	if s.connFunc != nil {
		s.CloseFunc()
	}
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
		go worker.(*Worker).Chan2Conn()
		go worker.(*Worker).Conn2Chan()
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
		Id: id,
		Chain: &DuplexChain{
			Chan2Conn: newChan,
			Conn2Chan: s.Conn2Chan,
		},
		Conn: conn,
		CloseFunc: func() {
			s.workers.Delete(id)
		},
		o: sync.Once{},
	}
	s.workers.Store(worker.Id, worker)
	return worker
}

func (s *Supervisor) ReadFromWorker() chan Frame {
	return s.Conn2Chan
}

func (s *Supervisor) CloseWorker(id string) {
	if s.ttlCache.Filter(id) { //only notify remote once to close connection
		pbConn2ChanLogger.Printf("worker [%s] not exist", id)
		go func() {
			s.Conn2Chan <- Frame{Id: id, Type: CLOSE_CONNECTION}
		}()
	}
}

func (s *Supervisor) WriteToWorker(frame *Frame) {
	id := frame.Id
	switch frame.Type {
	case CLOSE_CONNECTION:
		worker, ok := s.workers.Load(id)
		if ok {
			worker.(*Worker).Destroy(false)
		}
	case DATA:
		if s.autoCreate {
			s.autoCreateWorker(id)
		}
		worker, exist := s.workers.Load(id)
		if exist {
			LogBytes(pbConn2ChanLogger, frame.Data)
			worker.(*Worker).Chain.Chan2Conn <- frame.Data
		} else {
			if s.ttlCache.Filter(id) { //only notify remote once to close connection
				pbConn2ChanLogger.Printf("worker [%s] not exist", id)
				go func() {
					s.Conn2Chan <- Frame{Id: id, Type: CLOSE_CONNECTION}
				}()
			}
		}
	}
}

type Forwarder interface {
	ReadFromWorker() chan Frame
	WriteToWorker(frame *Frame)
	Destroy()
}
