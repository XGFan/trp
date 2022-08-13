package main

import (
	"flag"
	"log"
	"net"
	"os"
	"time"
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
	supervisor := trp.NewClientSupervisor(func() net.Conn {
		conn, _ := net.Dial("tcp", localAddr)
		return conn
	})
	for {
		clientDialer, err := net.Dial("tcp", serverAddr)
		if err != nil {
			clientLogger.Printf("dail to server fail: %v", err)
			time.Sleep(time.Second * 5)
			continue
		}
		clientLogger.Printf("connected to server")
		trpBinding := trp.NewPortBinding(clientDialer, supervisor)
		go trpBinding.Conn2Chan()
		trpBinding.Chan2Conn()
	}
}
