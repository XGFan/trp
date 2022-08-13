package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
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
	clientListener, _ := net.Listen("tcp", serverAddr)
	for {
		conn, err := clientListener.Accept()
		if err != nil {
			serverLogger.Printf("read from client fail: %v", err)
			_ = conn.Close()
			continue
		}
		ss, err := InitConn(conn)
		if err != nil {
			serverLogger.Printf("init with client fail: %v", err)
			_ = conn.Close()
			continue
		}
		supervisor := trp.NewServerSupervisor()
		serverLogger.Printf("client connected")
		trpBinding := trp.NewPortBinding(conn, supervisor)
		go trpBinding.Conn2Chan()
		go trpBinding.Chan2Conn()
		ss.Add(supervisor)
		supervisor.CloseFunc = func() {
			ss.Remove(supervisor)
			_ = conn.Close()
		}
	}
}

var LocalListeners = make(map[int]*trp.Circle[*trp.Supervisor], 0)

func InitConn(conn net.Conn) (*trp.Circle[*trp.Supervisor], error) {
	initCmd := make([]byte, 32)
	_, err := conn.Read(initCmd)
	if err != nil {
		return nil, err
	}
	all, _ := trp.ParseAll(initCmd)
	if len(all) == 1 && all[0].Type == trp.BIND {
		port := trp.BytesToInt16(all[0].Data)
		ss, exist := LocalListeners[port]
		if exist {
			return ss, nil
		} else {
			ss = &trp.Circle[*trp.Supervisor]{}
			LocalListeners[port] = ss
			go ListenLocal(ss, port)
			return ss, nil
		}
	} else {
		return nil, errors.New("wrong init data")
	}
}

func ListenLocal(ss *trp.Circle[*trp.Supervisor], port int) {
	forwardListener, _ := net.Listen("tcp", fmt.Sprintf(":%d", port))
	for {
		conn, _ := forwardListener.Accept()
		supervisor := ss.Next()
		worker := supervisor.NewWorker(conn)
		go worker.Chan2Conn()
		go worker.Conn2Chan()
	}
}
