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
	supervisor := trp.NewServerSupervisor()
	go ListenLocal(supervisor)
	ListenClient(supervisor)
}

func ListenClient(sv *trp.Supervisor) {
	clientListener, _ := net.Listen("tcp", serverAddr)
	for {
		conn, err := clientListener.Accept()
		if err != nil {
			serverLogger.Printf("read from client fail: %v", err)
			continue
		}
		serverLogger.Printf("client connected")
		trpBinding := trp.NewPortBinding(conn, sv)
		go trpBinding.Conn2Chan()
		go trpBinding.Chan2Conn()
	}
}

func ListenLocal(sv *trp.Supervisor) {
	forwardListener, _ := net.Listen("tcp", localAddr)
	for {
		conn, _ := forwardListener.Accept()
		worker := sv.AllocateWorkerByConn(conn)
		go worker.Chan2Conn()
		go worker.Conn2Chan()
	}
}
