// Package database exercises the database behavior the rest of the suites
// cannot reach. The handler tests run on sqlite and the api tests run gorm in
// dry-run mode, so anything that depends on how CockroachDB itself answers is
// unreachable from either: dry run builds a statement but never reads a result
// back, and sqlite rejects the CockroachDB grammar outright.
//
// The suite needs docker and does not need a control plane, which is why it
// lives outside test/integration. `mage test:unit` runs only ./pkg, ./internal,
// ./cmd, and ./magefiles, so nothing here runs during a unit pass.
package database

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	installer "github.com/threeport/threeport/pkg/threeport-installer/v0"
)

// databaseName is the database the control plane persists to, created here by
// hand because the deployed cluster gets it from an init container this suite
// does not run.
const databaseName = "threeport_api"

// startTimeout bounds the wait for the container to accept connections. A cold
// image pull happens before the container starts and is not on this clock.
const startTimeout = 60 * time.Second

// testDb is the handle every test in this package shares. The schema is built
// once because building it is slower than any test that uses it, and each test
// isolates itself by writing values no other test writes.
var testDb *gorm.DB

// TestMain starts one CockroachDB container for the package, builds the schema
// in it, and removes it afterwards. It skips the whole package rather than
// failing when docker is missing, so the suite stays runnable on a machine set
// up only to reach a control plane.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Println("docker not available; skipping the database tests")
		os.Exit(0)
	}

	container, port, err := startCockroach()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start cockroachdb: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := exec.Command("docker", "rm", "--force", container).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to remove the cockroachdb container: %v\n", err)
		}
	}()

	testDb, err = openSchema(port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build the schema: %v\n", err)
		// the deferred removal does not run on os.Exit, so remove here too
		exec.Command("docker", "rm", "--force", container).Run()
		os.Exit(1)
	}

	code := m.Run()

	exec.Command("docker", "rm", "--force", container).Run()
	os.Exit(code)
}

// startCockroach runs a single-node CockroachDB on a host port the kernel
// picks, so a run does not collide with a local control plane's database or
// with a second run of this suite. It returns the container id and that port.
//
// The image tag is the one the installer deploys, so the suite answers for the
// version the control plane actually runs against.
//
// The listen address is left at its default. An insecure server refuses an
// explicit one whose hostname is not loopback, and a loopback listener is
// unreachable from the published port, so naming the address at all is what
// breaks it.
func startCockroach() (string, string, error) {
	run := exec.Command(
		"docker", "run", "--detach",
		"--publish", "26257",
		fmt.Sprintf("cockroachdb/cockroach:%s", installer.DatabaseImageTag),
		"start-single-node", "--insecure",
	)
	out, err := run.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("failed to run the container: %w: %s", err, out)
	}
	container := strings.TrimSpace(string(out))

	// the published port is chosen at start, so read it back rather than
	// assuming one
	out, err = exec.Command("docker", "port", container, "26257/tcp").CombinedOutput()
	if err != nil {
		return container, "", fmt.Errorf("failed to read the published port: %w: %s", err, out)
	}
	mapping := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	index := strings.LastIndex(mapping, ":")
	if index < 0 {
		return container, "", fmt.Errorf("published port not found in %q", mapping)
	}

	return container, mapping[index+1:], nil
}

// openSchema waits for the server to accept connections, creates the control
// plane's database, and builds the tables and indexes from the same struct
// tags the deployed schema is built from. It returns a handle on that database.
func openSchema(port string) (*gorm.DB, error) {
	// the server listens before it can serve, so retry until a statement runs
	// rather than until the port opens
	var root *gorm.DB
	deadline := time.Now().Add(startTimeout)
	for {
		var err error
		root, err = open(port, "defaultdb")
		if err == nil {
			err = root.Exec("SELECT 1").Error
		}
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("cockroachdb did not accept connections within %s: %w", startTimeout, err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := root.Exec(fmt.Sprintf("CREATE DATABASE %s", databaseName)).Error; err != nil {
		return nil, fmt.Errorf("failed to create the database: %w", err)
	}

	db, err := open(port, databaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to open the database: %w", err)
	}

	// the deployed schema is built by the initial migration's AutoMigrate over
	// the persisted models, so building it the same way here keeps the indexes
	// under test identical to the deployed ones
	if err := db.AutoMigrate(
		&api_v0.AttachedObjectReference{},
		&api_v0.ModuleApi{},
	); err != nil {
		return nil, fmt.Errorf("failed to build the schema: %w", err)
	}

	return db, nil
}

// open returns a gorm handle on one database in the container. The connection
// is insecure because the container runs without certificates; the deployed
// server takes client certificates instead, which changes how a client
// authenticates and not how the server answers a rejected write.
func open(port, name string) (*gorm.DB, error) {
	return gorm.Open(
		postgres.New(postgres.Config{
			DSN: fmt.Sprintf(
				"postgres://root@127.0.0.1:%s/%s?sslmode=disable",
				port, name,
			),
		}),
		// the suite provokes failures on purpose, so gorm's own error logging
		// would fill the output with expected errors
		&gorm.Config{Logger: logger.Discard},
	)
}
