package trp

import (
	"net"
	"sync"
	"time"
)

type DefaultWorkerGroup struct {
	ttlCache *TTLCache
	muxChan  chan Frame
	workers  sync.Map
}

func (s *DefaultWorkerGroup) getWorker(id string) *Forwarder {
	worker, exist := s.workers.Load(id)
	if exist {
		return worker.(*Forwarder)
	} else {
		return nil
	}
}

func (s *DefaultWorkerGroup) Forward(id string, data []byte) {
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

func (s *DefaultWorkerGroup) Close(id string) {
	worker, ok := s.workers.Load(id)
	if ok {
		worker.(*Forwarder).Destroy(false)
	}
}

func (s *DefaultWorkerGroup) Destroy() {
	close(s.muxChan)
	s.workers.Range(func(key, value any) bool {
		value.(*Forwarder).Destroy(false) //propagate to forwarder
		return true
	})
}

func (s *DefaultWorkerGroup) Create(id string, conn net.Conn) *Forwarder {
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
	worker.Run()
	return worker
}

type ClientWorkerGroup struct {
	DefaultWorkerGroup
	connectFunc func() net.Conn
}

func (s *ClientWorkerGroup) autoCreate(id string) {
	if !s.ttlCache.Filter(id) {
		return
	}
	_, exist := s.workers.Load(id)
	if exist {
		return
	}
	conn := s.connectFunc()
	if conn != nil {
		s.DefaultWorkerGroup.Create(id, conn)
	}
}

func (s *ClientWorkerGroup) Forward(id string, data []byte) {
	s.autoCreate(id)
	s.DefaultWorkerGroup.Forward(id, data)
}

func NewServerWorkerGroup(muxChan chan Frame) WorkerGroup {
	return &DefaultWorkerGroup{
		ttlCache: NewTTLCache(time.Second * 10),
		muxChan:  muxChan,
		workers:  sync.Map{},
	}
}

func NewClientWorkerGroup(muxChan chan Frame, connectFunc func() net.Conn) WorkerGroup {
	return &ClientWorkerGroup{
		DefaultWorkerGroup: DefaultWorkerGroup{
			ttlCache: NewTTLCache(time.Second * 10),
			muxChan:  muxChan,
			workers:  sync.Map{},
		},
		connectFunc: connectFunc,
	}
}

// WorkerGroup manage a group of worker
type WorkerGroup interface {
	Create(id string, conn net.Conn) *Forwarder
	Forward(id string, data []byte)
	Close(id string)
	Destroy()
	getWorker(id string) *Forwarder
}
