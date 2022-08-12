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
	"time"
)

var logLevel = 1

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
	ERROR FrameType = -1
	DATA  FrameType = iota
	CLOSE
)

func Assemble(id string, byteSlice []byte) []byte {
	newBytes := make([]byte, 32+len(byteSlice))
	copy(newBytes, id)
	if byteSlice == nil || len(byteSlice) == 0 {
		newBytes[16] = 1
	} else {
		newBytes[16] = 0
	}
	copy(newBytes[17:25], Int64ToBytes(int64(len(newBytes))))
	copy(newBytes[32:], byteSlice)
	return newBytes
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
	frameType := FrameType(int(buf[16]))
	totalLen := BytesToInt64(buf[17:25])
	if totalLen > int64(len(buf)) {
		return buf
	}
	*frames = append(*frames, Frame{
		Id:   id,
		Type: frameType,
		Data: buf[32:totalLen],
	})
	return Parse(buf[totalLen:], frames)
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
			w.Chain.Conn2Chan <- DataPackage{
				Id:   w.Id,
				Data: nil,
			}
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
		byteSlice := make([]byte, 4096)
		readLen, err := w.Conn.Read(byteSlice)
		if err == nil {
			byteSlice = byteSlice[:readLen]
			w.Chain.Conn2Chan <- DataPackage{
				Id:   w.Id,
				Data: byteSlice,
			}
			LogBytes(conn2ChanLogger, byteSlice)
		} else {
			break
		}
	}
	w.Destroy(true)
}

// Chan2Conn read data from dedicated chan, then write to connection
func (w *Worker) Chan2Conn() {
	for byteSlice := range w.Chain.Chan2Conn {
		_, err := w.Conn.Write(byteSlice)
		if err != nil {
			//connection error
			w.Destroy(true)
			return
		} else {
			LogBytes(chan2ConnLogger, byteSlice)
		}
	}
	//remote chan close
	w.Destroy(false)
}

type DataPackage struct {
	Id   string
	Data []byte
}

type DuplexChain struct {
	Chan2Conn chan []byte        //dedicated,read
	Conn2Chan chan<- DataPackage //shared,write
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
		byteSlice := make([]byte, 4096)
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
			pb.forwarder.WriteToWorker(frame.Id, frame.Data)
		}
		buf.Write(remain)
	}
	pbConn2ChanLogger.Print("remote closed connection, close all chan")
	//simply close all chan
	//for _, c := range pb.forwarder.Chan2Conn {
	//	close(c)
	//}
	pb.notify <- struct{}{}
}

// Chan2Conn read data from shared channel, then write it to connection
func (pb *PortBinding) Chan2Conn() {
loop:
	for {
		select {
		case dataPackage := <-pb.forwarder.ReadFromWorker():
			newBytes := Assemble(dataPackage.Id, dataPackage.Data)
			size, err := pb.Write(newBytes)
			if err != nil {
				pbChan2ConnLogger.Printf("write to Conn fail %d: %v", size, err)
				break loop
			} else {
				LogBytes(pbChan2ConnLogger, dataPackage.Data)
			}
		case <-pb.notify:
			pbChan2ConnLogger.Printf("get notification from Conn2Chan, close goroutine")
			break loop
		}
	}
}

type Supervisor struct {
	autoCreate bool
	address    string
	Conn2Chan  chan DataPackage
	workers    sync.Map
}

func NewServerSupervisor() *Supervisor {
	return &Supervisor{
		Conn2Chan: make(chan DataPackage, 0),
		workers:   sync.Map{},
	}
}

func NewClientSupervisor(address string) *Supervisor {
	return &Supervisor{
		autoCreate: true,
		address:    address,
		Conn2Chan:  make(chan DataPackage, 0),
		workers:    sync.Map{},
	}
}

func (s *Supervisor) AutoCreateWorker(id string) {
	worker, exist := s.workers.Load(id)
	if exist {
		return
	}
	conn, _ := net.Dial("tcp", s.address)
	worker = s.CreateWorker(id, conn)
	s.workers.Store(id, worker)
	go worker.(*Worker).Chan2Conn()
	go worker.(*Worker).Conn2Chan()
}

func (s *Supervisor) AllocateWorkerByConn(conn net.Conn) *Worker {
	worker := s.NewWorker(conn)
	s.workers.Store(worker.Id, worker)
	return worker
}

func (s *Supervisor) NewWorker(conn net.Conn) *Worker {
	var id string
	for {
		id = RandomString(16)
		_, exist := s.workers.Load(id)
		if !exist {
			break
		}
	}
	return s.CreateWorker(id, conn)
}

func (s *Supervisor) CreateWorker(id string, conn net.Conn) *Worker {
	commonLogger.Printf("[%s] worker created", id)
	newChan := make(chan []byte, 0)
	return &Worker{
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
}

func (s *Supervisor) ReadFromWorker() chan DataPackage {
	return s.Conn2Chan
}

func (s *Supervisor) WriteToWorker(id string, data []byte) {
	if s.autoCreate {
		s.AutoCreateWorker(id)
	}
	worker, exist := s.workers.Load(id)
	if exist {
		//LogBytes(pbConn2ChanLogger, frame.Data)
		if data != nil && len(data) > 0 {
			timer := time.NewTimer(time.Second)
			select {
			case worker.(*Worker).Chain.Chan2Conn <- data:

			case <-timer.C:
				commonLogger.Printf("[%s] worker should be busy or dead", id)
				s.workers.Delete(id)
			}
		} else {
			pbConn2ChanLogger.Printf("get notify from remote, close worker %s", id)
			close(worker.(*Worker).Chain.Chan2Conn)
		}
	} else {
		pbConn2ChanLogger.Printf("worker [%s] not exist", id)
	}
}

type Forwarder interface {
	ReadFromWorker() chan DataPackage
	WriteToWorker(id string, byteSlices []byte)
}
