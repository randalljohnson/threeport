package v0

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/go-logr/logr"
)

const (
	// tcpDialTimeout is the timeout for each TCP connection attempt.
	tcpDialTimeout = 2 * time.Second

	// tcpRetryInterval is the sleep duration between TCP connection attempts.
	tcpRetryInterval = 2 * time.Second

	// tcpMaxRetries is the maximum number of TCP connection retries before
	// the process exits.
	tcpMaxRetries = 60
)

// tcpAPIPorts lists the ports the threeport API service may expose. The auth
// enabled deployment exposes 443; the auth disabled deployment exposes 80.
var tcpAPIPorts = []int{443, 80}

// WaitForAPI waits for the threeport API server to become reachable on any
// of the known API ports. It retries up to 60 times with a 2-second dial
// timeout and 2-second sleep between attempts (~4 minutes total). If none of
// the endpoints are reachable after all retries, the process exits with code
// 1 to trigger a pod restart.
func WaitForAPI(host string, log logr.Logger) {
	addrs := make([]string, 0, len(tcpAPIPorts))
	for _, port := range tcpAPIPorts {
		addrs = append(addrs, fmt.Sprintf("%s:%d", host, port))
	}
	log.Info("waiting for API server", "addresses", addrs)

	for i := 0; i < tcpMaxRetries; i++ {
		for _, addr := range addrs {
			conn, err := net.DialTimeout("tcp", addr, tcpDialTimeout)
			if err == nil {
				conn.Close()
				log.Info("API server is reachable", "address", addr)
				return
			}
		}
		time.Sleep(tcpRetryInterval)
	}

	log.Error(
		fmt.Errorf("API server not reachable after %d retries", tcpMaxRetries),
		"failed to connect to API server",
		"addresses", addrs,
	)
	os.Exit(1)
}
