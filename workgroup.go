package trp

import (
	"net"
	"sync"
	"time"
)

// WorkerGroup manage a group of worker
type WorkerGroup struct {
	fromMux   <-chan Frame
	toMux     chan []byte //TODO just for new worker, should use a new way
	workers   map[string]*Forwarder
	getWorker func(id string) *Forwarder
}

func (s *WorkerGroup) Run() {
	for frame := range s.fromMux {
		id, fType, data := frame.Id, frame.Type, frame.Data
		switch fType {
		case CLOSE:
			worker, ok := s.workers[id]
			delete(s.workers, id)
			if ok {
				close(worker.dedicatedChan) //close
			}
		case DATA:
			worker := s.getWorker(id)
			if worker == nil {
				continue
			}
			if worker.alive {
				LogBytes(PBConn2ChanLogger, data)
				worker.dedicatedChan <- data
			} else {
				delete(s.workers, id)
			}
		}
	}
	s.Destroy()
}

func (s *WorkerGroup) Create(id string, conn net.Conn) *Forwarder {
	CommonLogger.Printf("[%s] worker created", id)
	newChan := make(chan []byte, 10)
	worker := &Forwarder{
		Id:            id,
		muxChan:       s.toMux,
		dedicatedChan: newChan,
		conn:          conn,
		destroyLock:   sync.Once{},
		alive:         true,
	}
	s.workers[worker.Id] = worker
	return worker
}

func (s *WorkerGroup) Destroy() {
	for _, worker := range s.workers {
		close(worker.dedicatedChan) //propagate to forwarder
	}
}

func NewServerWorkerGroup(muxChan chan []byte, remote2Local chan Frame) *WorkerGroup {
	m := make(map[string]*Forwarder)
	cwg := &WorkerGroup{
		fromMux: remote2Local,
		toMux:   muxChan,
		workers: m,
	}
	cwg.getWorker = func(id string) *Forwarder {
		worker, exist := cwg.workers[id]
		if exist {
			return worker
		} else {
			return nil
		}
	}
	return cwg
}

func NewClientWorkerGroup(muxChan chan []byte, remote2Local chan Frame, connectFunc func() net.Conn) *WorkerGroup {
	m := make(map[string]*Forwarder)
	cwg := &WorkerGroup{
		fromMux: remote2Local,
		toMux:   muxChan,
		workers: m,
	}
	ttlCache := NewTTLCache(time.Second * 10)
	cwg.getWorker = func(id string) *Forwarder {
		worker, exist := cwg.workers[id]
		if exist {
			return worker
		}
		if !ttlCache.Filter(id) {
			return nil
		}
		conn := connectFunc()
		if conn != nil {
			newWorker := cwg.Create(id, conn)
			go newWorker.Run()
			return newWorker
		}
		return nil
	}
	return cwg
}
