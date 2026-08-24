package machinetest

import (
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	"github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// NewEncryptionKey returns a fresh base64-encoded AES-256 key for use in
// tests that need to encrypt SSH credentials or env values.
func NewEncryptionKey(t *testing.T) string {
	t.Helper()
	key, err := encryption.GenerateKey()
	require.NoError(t, err)
	return key
}

// encryptOrFail encrypts plaintext with key and fails the test on error.
// Unexported because only MRIFromAddr in this package consumes it.
func encryptOrFail(t *testing.T, key, plaintext string) string {
	t.Helper()
	ct, err := encryption.Encrypt(key, plaintext)
	require.NoError(t, err)
	return ct
}

// MRIFromAddr builds a *v0.MachineRuntimeInstance pointing at addr (a
// "host:port" string from StartSSHServer) with the given user + password.
// The password is encrypted with key. Reconciled defaults to true and
// HostKey is left nil so GetClient runs in capture mode.
func MRIFromAddr(
	t *testing.T,
	id uint,
	name, addr, user, password, key string,
) *v0.MachineRuntimeInstance {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return &v0.MachineRuntimeInstance{
		Common: v0.Common{ID: util.Ptr(id)},
		Reconciliation: v0.Reconciliation{
			Reconciled: util.Ptr(true),
		},
		Instance: v0.Instance{Named: v0.Named{Name: util.Ptr(name)}},
		Hostname: util.Ptr(host),
		Port:     util.Ptr(port),
		SSHUser:  util.Ptr(user),
		// SSHPassword is stored encrypted at rest; GetClient decrypts it
		// before building the ssh.AuthMethod.
		SSHPassword: util.Ptr(encryptOrFail(t, key, password)),
	}
}
