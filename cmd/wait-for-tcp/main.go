package main

import (
	"net"
	"os"
	"time"
)

func main() {
	for {
		conn, err := net.DialTimeout("tcp", os.Args[1], 2*time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(2 * time.Second)
	}
}
