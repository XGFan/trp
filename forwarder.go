package trp

import (
	"net"
	"sync"
	"time"
)

// Forwarder create bridge between local port connection net.Conn and Channel
//
// one connection, one shared channel, one dedicated channel, and two goroutine
//
// functions: read conn, write to inputChan. read dedicatedReadChan, write to conn
type Forwarder struct {
	Id                string //forwarder's identifier
	conn              net.Conn
	muxChan           chan<- Frame
	dedicatedReadChan chan []byte
	destroyLock       sync.Once
}

func (w *Forwarder) Run() {
	go w.conn2Chan()
	w.chan2Conn()
}

// Destroy just destroy worker and resource
func (w *Forwarder) destroy(propagate bool) {
	w.destroyLock.Do(func() {
		if propagate {
			//propagate: local connection closed, should notify remote to close same id worker
			CommonLogger.Printf("[%s] local conn closed, destroy and propagate", w.Id)
			w.muxChan <- Frame{Id: w.Id, Type: CLOSE}
			//remove worker from workers
			//and consume all unsent msg, discard it
			for {
				select {
				case <-w.dedicatedReadChan:
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
		byteSlice := make([]byte, BufferSize)
		readLen, err := w.conn.Read(byteSlice)
		if err == nil {
			byteSlice = byteSlice[:readLen]
			w.muxChan <- Frame{
				Id:   w.Id,
				Data: byteSlice,
			}
			LogBytes(Conn2ChanLogger, byteSlice)
		} else {
			w.destroy(true)
			return
		}
	}
}

// Chan2Conn read data from dedicated chan, then write to connection
func (w *Forwarder) chan2Conn() {
	for byteSlice := range w.dedicatedReadChan {
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
