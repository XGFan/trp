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
var remotePort int
var connections int

func init() {
	flag.StringVar(&serverAddr, "s", "127.0.0.1:2345", "server addr")
	flag.IntVar(&remotePort, "rp", 3456, "remote port")
	flag.StringVar(&localAddr, "f", "127.0.0.1:2019", "forward addr")
	flag.IntVar(&connections, "c", 1, "connections")
}

func main() {
	flag.Parse()
	clientLogger.Printf("connect to %s", serverAddr)
	clientLogger.Printf("requests from remote %d will forward to %s", remotePort, localAddr)
	for i := 0; i < connections; i++ {
		go connectServer()
	}
	select {}
}

func connectServer() {
	for {
		conn, err := net.Dial("tcp", serverAddr)
		if err != nil {
			clientLogger.Printf("dail to server fail: %v", err)
			time.Sleep(time.Second * 5)
			continue
		}
		err = InitConn(conn, remotePort)
		if err != nil {
			clientLogger.Printf("init with server fail: %v", err)
			time.Sleep(time.Second * 5)
			continue
		}
		clientLogger.Printf("connected to server")
		client := trp.NewClient(conn, func() net.Conn {
			conn, _ := net.Dial("tcp", localAddr)
			return conn
		})
		client.Run()
	}
}

func InitConn(conn net.Conn, port int) error {
	_, err := conn.Write(trp.AssembleHeader(&trp.Frame{Id: " JUST HAVE FUN! ", Type: trp.BIND, Data: trp.IntTo2Bytes(port)}))
	return err
}
