package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"trp"
)

var serverLogger = log.New(os.Stdout, "[Server] ", log.Ldate|log.Ltime|log.Lshortfile|log.Lmsgprefix)

var serverAddr string

func init() {
	flag.StringVar(&serverAddr, "s", "127.0.0.1:2345", "server listen addr")
}

func main() {
	flag.Parse()
	serverLogger.Printf("listen on %s", serverAddr)
	ListenClient()
}

func ListenClient() {
	var pmg PortMappingGroup = make(map[int]*PortMapping, 0)
	clientListener, _ := net.Listen("tcp", serverAddr)
	for {
		conn, err := clientListener.Accept()
		if err != nil {
			serverLogger.Printf("read from client fail: %v", err)
			_ = conn.Close()
			continue
		}
		port, err := InitConn(conn)
		serverLogger.Printf("client connected")
		if err != nil {
			serverLogger.Printf("init with client fail: %v", err)
			_ = conn.Close()
			continue
		}
		pmg.Register(port).AcceptClient(conn)
	}
}

func InitConn(conn net.Conn) (int, error) {
	initCmd := make([]byte, 32)
	_, err := conn.Read(initCmd)
	if err != nil {
		return 0, err
	}
	all, _ := trp.ParseAll(&trp.SliceLink[byte]{Head: initCmd})
	if len(all) == 1 && all[0].Type == trp.BIND {
		port := all[0].ParsePort()
		return port, nil
	} else {
		return 0, errors.New("wrong init data")
	}
}

type PortMappingGroup map[int]*PortMapping

func (pmg *PortMappingGroup) Register(port int) *PortMapping {
	pm, exist := (*pmg)[port]
	if !exist {
		pm = NewPortMapping(port)
		(*pmg)[port] = pm
	}
	return pm
}

type PortMapping struct {
	Port   int
	chains *trp.Circle[*PortMappingChain]
	wg     sync.WaitGroup
}

func NewPortMapping(port int) *PortMapping {
	pm := &PortMapping{
		Port:   port,
		chains: &trp.Circle[*PortMappingChain]{},
	}
	pm.Run()
	return pm
}

func (pm *PortMapping) Start() {
	pm.wg.Wait()
}

func (pm *PortMapping) AcceptClient(conn net.Conn) {
	pm.wg.Add(1)
	supervisor := trp.NewServerSupervisor()
	multiplexer := trp.NewMultiplexer(conn, supervisor)

	pmc := &PortMappingChain{
		Multiplexer: multiplexer,
		Supervisor:  supervisor,
		wg:          &pm.wg,
	}
	multiplexer.DestroyHook = func() {
		pm.chains.Remove(pmc)
		pmc.wg.Done()
	}
	pmc.Start()
	pm.chains.Add(pmc)
}

func (pm *PortMapping) Accept(conn net.Conn) {
	pm.chains.Next().AcceptConn(conn)
}

func (pm *PortMapping) Run() {
	go func() {
		forwardListener, err := net.Listen("tcp", fmt.Sprintf(":%d", pm.Port))
		serverLogger.Printf("listen %d", pm.Port)
		if err != nil {
			return
		}
		for {
			conn, _ := forwardListener.Accept()
			pm.Accept(conn)
		}
	}()
}

type PortMappingChain struct {
	Multiplexer *trp.Multiplexer
	Supervisor  *trp.Supervisor
	wg          *sync.WaitGroup
}

func (pmc *PortMappingChain) Start() {
	pmc.Multiplexer.Run()
}

func (pmc *PortMappingChain) AcceptConn(conn net.Conn) {
	go pmc.Supervisor.NewWorker(conn).Run()
}
