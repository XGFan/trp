package main

import (
	"flag"
	"log"
	"net"
	"os"
	"trp"
)

var serverLogger = log.New(os.Stdout, "[Server] ", log.Ldate|log.Ltime|log.Lshortfile|log.Lmsgprefix)

var serverAddr string
var localAddr string

func init() {
	flag.StringVar(&serverAddr, "s", "127.0.0.1:2345", "server listen addr")
	flag.StringVar(&localAddr, "l", "127.0.0.1:3456", "forward addr")
}

func main() {
	flag.Parse()
	serverLogger.Printf("listen on %s", serverAddr)
	serverLogger.Printf("requests to %s will forward to client", localAddr)
	ss := &trp.Circle[*trp.Supervisor]{}
	go ListenClient(ss)
	ListenLocal(ss)
}

func ListenClient(ss *trp.Circle[*trp.Supervisor]) {
	clientListener, _ := net.Listen("tcp", serverAddr)
	for {
		conn, err := clientListener.Accept()
		if err != nil {
			serverLogger.Printf("read from client fail: %v", err)
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
		}
	}
}

func ListenLocal(ss *trp.Circle[*trp.Supervisor]) {
	forwardListener, _ := net.Listen("tcp", localAddr)
	for {
		conn, _ := forwardListener.Accept()
		supervisor := ss.Next()
		worker := supervisor.NewWorker(conn)
		go worker.Chan2Conn()
		go worker.Conn2Chan()
	}
}
