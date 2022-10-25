package trp

import (
	"net"
	"sync"
	"time"
)

// WorkerGroup manage a group of worker
type WorkerGroup struct {
	inputChan <-chan Frame
	muxChan   chan Frame //TODO just for new worker, should use a new way
	workers   sync.Map
	getWorker func(id string) *Forwarder
}

func (s *WorkerGroup) Run() {
	for frame := range s.inputChan {
		id, fType, data := frame.Id, frame.Type, frame.Data
		switch fType {
		case CLOSE:
			worker, ok := s.workers.Load(id)
			s.workers.Delete(id)
			if ok {
				close(worker.(*Forwarder).dedicatedReadChan) //close
			}
		case DATA:
			worker := s.getWorker(id)
			if worker == nil {
				continue
			}
			LogBytes(PBConn2ChanLogger, data)
			worker.dedicatedReadChan <- data
		}
	}
	s.Destroy()
}

func (s *WorkerGroup) Create(id string, conn net.Conn) *Forwarder {
	CommonLogger.Printf("[%s] worker created", id)
	newChan := make(chan []byte, 0)
	worker := &Forwarder{
		Id:                id,
		muxChan:           s.muxChan,
		dedicatedReadChan: newChan,
		conn:              conn,
		destroyLock:       sync.Once{},
	}
	s.workers.Store(worker.Id, worker)
	return worker
}

func (s *WorkerGroup) Destroy() {
	s.workers.Range(func(key, worker any) bool {
		close(worker.(*Forwarder).dedicatedReadChan) //propagate to forwarder
		return true
	})
}

func NewServerWorkerGroup(muxChan chan Frame, remote2Local chan Frame) *WorkerGroup {
	cwg := &WorkerGroup{
		inputChan: remote2Local,
		muxChan:   muxChan,
		workers:   sync.Map{},
	}
	cwg.getWorker = func(id string) *Forwarder {
		worker, exist := cwg.workers.Load(id)
		if exist {
			return worker.(*Forwarder)
		} else {
			return nil
		}
	}
	return cwg
}

func NewClientWorkerGroup(muxChan chan Frame, remote2Local chan Frame, connectFunc func() net.Conn) *WorkerGroup {
	cwg := &WorkerGroup{
		inputChan: remote2Local,
		muxChan:   muxChan,
		workers:   sync.Map{},
	}
	ttlCache := NewTTLCache(time.Second * 10)
	cwg.getWorker = func(id string) *Forwarder {
		worker, exist := cwg.workers.Load(id)
		if exist {
			return worker.(*Forwarder)
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
