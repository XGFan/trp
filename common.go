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
)

var debug = false

func init() {
	flag.BoolVar(&debug, "d", false, "debug")
}

var pbChan2ConnLogger = log.New(os.Stdout, "[PB][Chan ---> Conn] ", log.Ldate|log.Ltime|log.Lshortfile|log.Lmsgprefix)
var pbConn2ChanLogger = log.New(os.Stdout, "[PB][Conn ---> Chan] ", log.Ldate|log.Ltime|log.Lshortfile|log.Lmsgprefix)
var conn2ChanLogger = log.New(os.Stdout, "[LF][Conn ---> Chan] ", log.Ldate|log.Ltime|log.Lshortfile|log.Lmsgprefix)
var chan2ConnLogger = log.New(os.Stdout, "[LF][Chan ---> Conn] ", log.Ldate|log.Ltime|log.Lshortfile|log.Lmsgprefix)
var commonLogger = log.New(os.Stdout, "[Common] ", log.Ldate|log.Ltime|log.Lshortfile|log.Lmsgprefix)

func LogBytes(logger *log.Logger, bytes []byte) {
	if debug {
		for _, s := range strings.Split(string(bytes), "\n") {
			_ = logger.Output(2, s)
		}
	} else {
		_ = logger.Output(2, fmt.Sprintf("Data size: %d", len(bytes)))
	}
	//logger.Output(2, string(bytes))
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

type Worker struct {
	id        string
	chain     *DuplexChain
	conn      net.Conn
	closeFunc func()
	o         sync.Once
}

// Destroy just destroy worker and resource
func (w *Worker) Destroy(propagate bool) {
	w.o.Do(func() {
		commonLogger.Printf("destroy worker %s", w.id)
		if propagate {
			w.chain.Conn2Chan <- DataPackage{
				Id:   w.id,
				Data: []byte{},
			}
		} else {

		}
		_ = w.conn.Close()
		w.closeFunc()
	})
}

//Conn2Chan read data from connection. then write to shard channel.
func (w *Worker) Conn2Chan() {
	for {
		byteSlice := make([]byte, 4096)
		readLen, err := w.conn.Read(byteSlice)
		if err == nil {
			byteSlice = byteSlice[:readLen]
			w.chain.Conn2Chan <- DataPackage{
				Id:   w.id,
				Data: byteSlice,
			}
			LogBytes(conn2ChanLogger, byteSlice)
		} else {
			break
		}
	}
	commonLogger.Printf("local connection closed,so try to close worker %s", w.id)
	w.Destroy(true)
}

// Chan2Conn read data from dedicated chan, then write to connection
func (w *Worker) Chan2Conn() {
	for byteSlice := range w.chain.Chan2Conn {
		_, err := w.conn.Write(byteSlice)
		if err != nil {
			commonLogger.Println(err)
			break
		} else {
			LogBytes(chan2ConnLogger, byteSlice)
		}
	}
	commonLogger.Printf("remote chan closed,so try to close worker %s", w.id)
	//remote close
	w.Destroy(false)
}

type DataChain struct {
	Chan2Conn map[string]chan []byte //dedicated,read
	Conn2Chan chan DataPackage       //shared,write
}

func NewDataChain() *DataChain {
	return &DataChain{
		Chan2Conn: make(map[string]chan []byte, 0),
		Conn2Chan: make(chan DataPackage, 0),
	}
}

func (wg *DataChain) NewWorker(conn net.Conn) *Worker {
	var id string
	for {
		id = RandomString(16)
		_, exist := wg.Chan2Conn[id]
		if !exist {
			break
		}
	}
	return wg.CreateWorker(id, conn)
}

func (wg *DataChain) CreateWorker(id string, conn net.Conn) *Worker {
	commonLogger.Printf("create worker id: %s, total: %d", id, len(wg.Chan2Conn))
	newChan := make(chan []byte, 0)
	wg.Chan2Conn[id] = newChan
	return &Worker{
		id: id,
		chain: &DuplexChain{
			Chan2Conn: newChan,
			Conn2Chan: wg.Conn2Chan,
		},
		conn: conn,
		closeFunc: func() {
			wg.RemoveChan(id)
		},
		o: sync.Once{},
	}
}

func (wg *DataChain) RemoveChan(id string) {
	_, exist := wg.Chan2Conn[id]
	if exist {
		delete(wg.Chan2Conn, id)
	}
}

type DataPackage struct {
	Id   string
	Data []byte
}

type DuplexChain struct {
	Chan2Conn <-chan []byte      //dedicated,read
	Conn2Chan chan<- DataPackage //shared,write
}

type PortBinding struct {
	net.Conn
	forwardDataChain  *DataChain
	notify            chan struct{}
	CreateChannelFunc func(id string)
}

func NewPortBinding(conn net.Conn, dc *DataChain) *PortBinding {
	return &PortBinding{
		Conn:             conn,
		forwardDataChain: dc,
		notify:           make(chan struct{}, 0),
		CreateChannelFunc: func(id string) {

		},
	}
}

// Conn2Chan read data from connection, then write it to dedicated channel
func (pb *PortBinding) Conn2Chan() {
	buf := new(bytes.Buffer)
	for {
		pbConn2ChanLogger.Printf("try to read from conn")
		byteSlice := make([]byte, 1024)
		readLen, err := pb.Read(byteSlice)
		pbConn2ChanLogger.Printf("read from conn success: %d", readLen)
		if err != nil {
			pbConn2ChanLogger.Printf("read from conn fail: %v", err)
			break
		}
		byteSlice = byteSlice[:readLen]
		newBytes := make([]byte, len(byteSlice)+buf.Len())
		copy(newBytes, buf.Bytes())
		copy(newBytes[buf.Len():], byteSlice)
		buf.Reset()
		frames, remain := ParseAll(newBytes)
		for _, frame := range frames {
			pb.CreateChannelFunc(frame.Id)
			channel := pb.forwardDataChain.Chan2Conn[frame.Id]
			if channel != nil {
				LogBytes(pbConn2ChanLogger, frame.Data)
				if len(frame.Data) > 0 {
					channel <- frame.Data
				} else {
					close(channel)
				}
			} else {
				pbConn2ChanLogger.Printf("channel id not exist: %s", frame.Id)
				break
			}
		}
		buf.Write(remain)
	}
	pbConn2ChanLogger.Print("remote closed connection, close all chan")
	//simply close all chan
	for _, c := range pb.forwardDataChain.Chan2Conn {
		close(c)
	}
	pb.notify <- struct{}{}

}

// Chan2Conn read data from shared channel, then write it to connection
func (pb *PortBinding) Chan2Conn() {
loop:
	for {
		select {
		case dataPackage := <-pb.forwardDataChain.Conn2Chan:
			id, byteSlice := dataPackage.Id, dataPackage.Data
			pbChan2ConnLogger.Println("selected data from chan success")
			newBytes := Assemble(id, byteSlice)
			//pb.SetWriteDeadline(time.Now().Add(3 * time.Second))
			size, err := pb.Write(newBytes)
			if err != nil {
				pbChan2ConnLogger.Printf("write to conn fail %d: %v", size, err)
				break loop
			} else {
				LogBytes(pbChan2ConnLogger, byteSlice)
			}
		case <-pb.notify:
			pbChan2ConnLogger.Printf("get notification from Conn2Chan, close goroutine")
			break loop
		}
	}
}
