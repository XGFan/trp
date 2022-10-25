package trp

import (
	"net"
	"sync"
	"time"
)

type CommonWorkerGroup struct {
	muxChan   chan Frame
	workers   sync.Map
	getWorker func(id string) *Forwarder
}

func (s *CommonWorkerGroup) Create(id string, conn net.Conn) *Forwarder {
	CommonLogger.Printf("[%s] worker created", id)
	newChan := make(chan []byte, 1)
	worker := &Forwarder{
		Id:                id,
		muxChan:           s.muxChan,
		dedicatedReadChan: newChan,
		conn:              conn,
		alive:             true,
		destroyLock:       sync.Once{},
	}
	s.workers.Store(worker.Id, worker)
	return worker
}

func (s *CommonWorkerGroup) Forward(id string, data []byte) {
	worker := s.getWorker(id)
	if worker == nil {
		return
	}
	if worker.alive {
		LogBytes(PBConn2ChanLogger, data)
		worker.dedicatedReadChan <- data
	} else {
		s.workers.Delete(id)
	}
}

func (s *CommonWorkerGroup) Close(id string) {
	worker, ok := s.workers.Load(id)
	if ok {
		worker.(*Forwarder).Destroy(false)
	}
}

func (s *CommonWorkerGroup) Destroy() {
	close(s.muxChan)
	s.workers.Range(func(key, value any) bool {
		value.(*Forwarder).Destroy(false) //propagate to forwarder
		return true
	})
}

func NewServerWorkerGroup(muxChan chan Frame) WorkerGroup {
	cwg := &CommonWorkerGroup{
		muxChan: muxChan,
		workers: sync.Map{},
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

func NewClientWorkerGroup(muxChan chan Frame, connectFunc func() net.Conn) WorkerGroup {
	cwg := &CommonWorkerGroup{
		muxChan: muxChan,
		workers: sync.Map{},
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

// WorkerGroup manage a group of worker
type WorkerGroup interface {
	Create(id string, conn net.Conn) *Forwarder
	Forward(id string, data []byte)
	Close(id string)
	Destroy()
}
