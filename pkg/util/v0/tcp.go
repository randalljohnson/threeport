package v0

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/go-logr/logr"
)

const (
	// TCPDialTimeout is the timeout for each TCP connection attempt.
	TCPDialTimeout = 2 * time.Second

	// TCPRetryInterval is the sleep duration between TCP connection attempts.
	TCPRetryInterval = 2 * time.Second

	// TCPDefaultMaxRetries is the default maximum number of TCP connection
	// retries before the process exits.
	TCPDefaultMaxRetries = 60

	// TCPDefaultPort is the default port used for the threeport API service.
	TCPDefaultPort = 443
)

// WaitForTCP waits for a TCP endpoint to become reachable. It retries up to
// maxRetries times with a 2-second dial timeout and 2-second sleep between
// attempts. If the endpoint is not reachable after all retries, the process
// exits with code 1 to trigger a pod restart.
func WaitForTCP(host string, port int, log logr.Logger, maxRetries int) {
	addr := fmt.Sprintf("%s:%d", host, port)
	log.Info("waiting for TCP endpoint", "address", addr, "maxRetries", maxRetries)

	for i := 0; i < maxRetries; i++ {
		conn, err := net.DialTimeout("tcp", addr, TCPDialTimeout)
		if err == nil {
			conn.Close()
			log.Info("TCP endpoint is reachable", "address", addr)
			return
		}
		time.Sleep(TCPRetryInterval)
	}

	log.Error(
		fmt.Errorf("TCP endpoint not reachable after %d retries", maxRetries),
		"failed to connect to TCP endpoint",
		"address", addr,
	)
	os.Exit(1)
}
