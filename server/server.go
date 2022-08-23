package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"sync/atomic"
	"trp"
)

var serverLogger = log.New(os.Stdout, "[Server] ", log.Ldate|log.Ltime|log.Lshortfile|log.Lmsgprefix)

var serverAddr string
var id int32 = 1 //TODO where should you go?

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
		go pmg.Register(port).CreateMultiplexer(conn)
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
	chains *trp.Circle[*trp.Multiplexer]
}

func NewPortMapping(port int) *PortMapping {
	pm := &PortMapping{
		Port:   port,
		chains: &trp.Circle[*trp.Multiplexer]{},
	}
	pm.Run()
	return pm
}

// CreateMultiplexer when client dial in, create a new multiplexer, add it to chains. when it is closed or stopped, remove it.
func (pm *PortMapping) CreateMultiplexer(conn net.Conn) {
	multiplexer := trp.NewServerMultiplexer(conn)
	pm.chains.Add(multiplexer)
	defer pm.chains.Remove(multiplexer)
	go multiplexer.Chan2Conn()
	multiplexer.Conn2Chan()
}

// UsingMultiplexer when local port dial in, find a multiplexer, pass to it.
func (pm *PortMapping) UsingMultiplexer(conn net.Conn) {
	mux := pm.chains.Next()
	newId := atomic.AddInt32(&id, 1)
	go mux.WorkerGroup.CreateWorker(fmt.Sprintf("%016d", newId), conn).Run()
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
			pm.UsingMultiplexer(conn)
		}
	}()
}
