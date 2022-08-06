package main

import (
	"flag"
	"log"
	"net"
	"os"
	"sync"
	"trp"
)

var clientLogger = log.New(os.Stdout, "[Client] ", log.Ldate|log.Ltime|log.Lshortfile|log.Lmsgprefix)

var serverAddr string
var localAddr string

func init() {
	flag.StringVar(&serverAddr, "s", "127.0.0.1:2345", "server addr")
	flag.StringVar(&localAddr, "l", "127.0.0.1:2019", "forward addr")
}

func main() {
	flag.Parse()
	clientLogger.Printf("connect to %s", serverAddr)
	clientLogger.Printf("requests from remote will forward to %s", localAddr)
	dataChain := trp.NewDataChain()
	supervisor := NewSupervisor(localAddr, dataChain)
	for {
		clientDialer, err := net.Dial("tcp", serverAddr)
		if err != nil {
			clientLogger.Printf("dail to server fail: %v", err)
			continue
		}
		clientLogger.Printf("connected to server")
		trpBinding := trp.NewPortBinding(clientDialer, dataChain)
		trpBinding.CreateChannelFunc = supervisor.AllocateWorker
		wg := sync.WaitGroup{}
		wg.Add(2)
		go func() {
			trpBinding.Conn2Chan()
			wg.Done()
		}()
		go func() {
			trpBinding.Chan2Conn()
			wg.Done()
		}()
		wg.Wait()
	}
}

type Supervisor struct {
	address string
	wg      *trp.DataChain
	workers map[string]*trp.Worker
}

func NewSupervisor(address string, wg *trp.DataChain) *Supervisor {
	return &Supervisor{
		address: address,
		wg:      wg,
		workers: make(map[string]*trp.Worker, 0),
	}
}

func (s Supervisor) AllocateWorker(id string) {
	worker, exist := s.workers[id]
	if exist {
		return
	}
	conn, _ := net.Dial("tcp", s.address)
	worker = s.wg.CreateWorker(id, conn)
	s.workers[id] = worker
	go worker.Chan2Conn()
	go worker.Conn2Chan()
}
