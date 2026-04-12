// Package v0 provides client helpers for connecting to and executing scripts
// on threeport machine runtime instances.
package v0

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	"github.com/threeport/threeport/pkg/encryption/v0"
)

const (
	// defaultSSHPort is the default TCP port used for SSH connections when
	// MachineRuntimeInstance.Port is not set.
	defaultSSHPort = 22

	// connectTimeout is the SSH dial timeout.
	connectTimeout = 10 * time.Second
)

// GetClient establishes an authenticated SSH connection to the given machine
// runtime instance.  SSHKey and SSHPassword are decrypted using the provided
// encryption key and used to build auth methods (key preferred, password
// fallback).  Port defaults to 22 if not set on the instance.
//
// TODO: replace ssh.InsecureIgnoreHostKey() with a known_hosts-backed callback
// so we can verify the remote host's identity.
func GetClient(mri *v0.MachineRuntimeInstance, encryptionKey string) (*ssh.Client, error) {
	// decrypt ssh credentials (at least one is guaranteed by BeforeCreate hook)
	var decryptedKey, decryptedPassword string
	if mri.SSHKey != nil {
		dk, err := encryption.Decrypt(encryptionKey, *mri.SSHKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt ssh key: %w", err)
		}
		decryptedKey = dk
	}
	if mri.SSHPassword != nil {
		dp, err := encryption.Decrypt(encryptionKey, *mri.SSHPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt ssh password: %w", err)
		}
		decryptedPassword = dp
	}

	// build auth methods
	authMethods, err := buildAuthMethods(decryptedKey, decryptedPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to build ssh auth methods: %w", err)
	}

	// resolve port (gorm default:22 means this will normally be populated from
	// the DB, but fall back just in case)
	port := defaultSSHPort
	if mri.Port != nil {
		port = *mri.Port
	}

	// build client config
	config := &ssh.ClientConfig{
		User:            *mri.SSHUser,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         connectTimeout,
	}

	// dial the remote host
	addr := net.JoinHostPort(*mri.Hostname, strconv.Itoa(port))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("failed to dial ssh %s: %w", addr, err)
	}

	return client, nil
}

// Ping runs `true` on the remote over the existing SSH client to verify the
// connection is usable.
func Ping(client *ssh.Client) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create ssh session: %w", err)
	}
	defer session.Close()

	if err := session.Run("true"); err != nil {
		return fmt.Errorf("failed to run ping command: %w", err)
	}

	return nil
}

// RunScript executes the given script on the remote over the existing SSH
// client.  It wraps the script with optional `cd <workingDir>` and env var
// exports, then pipes the full script to `<shell> -s` via stdin.  Timeout is
// applied via context.WithTimeout - when the timeout expires the session is
// signaled and closed, and timedOut is set to true.
//
// Returns captured stdout, stderr, exit code (-1 on signal/timeout/transport
// error), a timedOut flag, and any transport error encountered.  A non-zero
// exit code is not a transport error - err will be nil.
func RunScript(
	client *ssh.Client,
	script string,
	shell string,
	workingDir string,
	env []string,
	timeout *int,
) (stdout string, stderr string, exitCode int, timedOut bool, err error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", -1, false, fmt.Errorf("failed to create ssh session: %w", err)
	}
	defer session.Close()

	// assemble the full script: cd + env exports + user script
	fullScript := buildScript(script, workingDir, env)

	// attach stdout/stderr buffers
	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// attach stdin pipe for the script body
	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return "", "", -1, false, fmt.Errorf("failed to open ssh stdin pipe: %w", err)
	}

	// build context for timeout handling
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout != nil && *timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*timeout)*time.Second)
		defer cancel()
	}

	// start `<shell> -s` which reads the script from stdin
	startCmd := fmt.Sprintf("%s -s", shell)
	if err := session.Start(startCmd); err != nil {
		return "", "", -1, false, fmt.Errorf("failed to start ssh command: %w", err)
	}

	// write script body and close stdin to signal EOF
	if _, werr := stdinPipe.Write([]byte(fullScript)); werr != nil {
		return stdoutBuf.String(), stderrBuf.String(), -1, false, fmt.Errorf("failed to write script to ssh stdin: %w", werr)
	}
	if cerr := stdinPipe.Close(); cerr != nil {
		return stdoutBuf.String(), stderrBuf.String(), -1, false, fmt.Errorf("failed to close ssh stdin: %w", cerr)
	}

	// wait for command to complete, honoring timeout via context
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- session.Wait()
	}()

	select {
	case werr := <-waitErr:
		// command finished on its own
		stdout = stdoutBuf.String()
		stderr = stderrBuf.String()
		if werr == nil {
			return stdout, stderr, 0, false, nil
		}
		// if the error is an ExitError, extract the exit code
		var exitErr *ssh.ExitError
		if errors.As(werr, &exitErr) {
			return stdout, stderr, exitErr.ExitStatus(), false, nil
		}
		// other transport error
		return stdout, stderr, -1, false, fmt.Errorf("ssh command failed: %w", werr)
	case <-ctx.Done():
		// timeout expired - kill the session
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		// drain the wait channel to avoid leaking the goroutine
		<-waitErr
		return stdoutBuf.String(), stderrBuf.String(), -1, true, nil
	}
}

// MergeEnv merges two env slices in KEY=VALUE format, with entries from b
// taking precedence over entries in a on duplicate keys.  Returns a new slice
// containing the merged result.
func MergeEnv(a []string, b []string) []string {
	seen := make(map[string]int)
	var merged []string

	add := func(entries []string) {
		for _, e := range entries {
			if e == "" {
				continue
			}
			key := e
			if idx := strings.Index(e, "="); idx >= 0 {
				key = e[:idx]
			}
			if pos, ok := seen[key]; ok {
				merged[pos] = e
				continue
			}
			seen[key] = len(merged)
			merged = append(merged, e)
		}
	}

	add(a)
	add(b)

	return merged
}

// buildAuthMethods returns a slice of ssh.AuthMethod built from the optional
// private key PEM and password.  Key is preferred when present; password is
// added as a fallback.
func buildAuthMethods(key string, password string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if key != "" {
		signer, err := ssh.ParsePrivateKey([]byte(key))
		if err != nil {
			return nil, fmt.Errorf("failed to parse ssh private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if password != "" {
		methods = append(methods, ssh.Password(password))
	}

	return methods, nil
}

// buildScript assembles the full script to pipe to the remote shell, prefixing
// with `cd <workingDir>` when set and `export KEY=VALUE` statements for each
// entry in env.  The user's script is appended verbatim at the end.
func buildScript(script string, workingDir string, env []string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	if workingDir != "" {
		fmt.Fprintf(&b, "cd %s\n", shellQuote(workingDir))
	}
	for _, e := range env {
		if e == "" {
			continue
		}
		// env entries are KEY=VALUE - split on the first '=' to quote only the value
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			// no '=' - export the bare name (unusual but harmless)
			fmt.Fprintf(&b, "export %s\n", parts[0])
			continue
		}
		fmt.Fprintf(&b, "export %s=%s\n", parts[0], shellQuote(parts[1]))
	}
	b.WriteString(script)
	if !strings.HasSuffix(script, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// shellQuote wraps a string in single quotes, escaping any embedded single
// quotes so the value is safely interpolated into a shell command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
