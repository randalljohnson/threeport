package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

// TestDownCmdMetadata asserts DownCmd's Use, Short, Long, Example, and behavior flags match the documented values.
func TestDownCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if DownCmd.Use != "down" {
		t.Errorf("DownCmd.Use = %q, want %q", DownCmd.Use, "down")
	}
	// verify Short description matches the documented help string
	if DownCmd.Short != "Spin down a deployment of the Threeport control plane" {
		t.Errorf("DownCmd.Short = %q, want documented short description", DownCmd.Short)
	}
	// verify Long description is set for cobra help output
	if DownCmd.Long == "" {
		t.Errorf("DownCmd.Long is empty, want non-empty description")
	}
	// verify Example demonstrates the required --name flag
	if !strings.Contains(DownCmd.Example, "--name") {
		t.Errorf("DownCmd.Example = %q, want mention of --name", DownCmd.Example)
	}
	// verify usage is silenced on error so failures don't dump the help block
	if !DownCmd.SilenceUsage {
		t.Errorf("DownCmd.SilenceUsage = false, want true")
	}
	// verify PreRun hook wired so config/context init runs before Run
	if DownCmd.PreRun == nil {
		t.Errorf("DownCmd.PreRun = nil, want a function")
	}
	// verify Run hook wired so cobra dispatch has a target
	if DownCmd.Run == nil {
		t.Errorf("DownCmd.Run = nil, want a function")
	}
}

// TestDownCmdRegisteredOnRoot asserts DownCmd is a subcommand of rootCmd via init().
func TestDownCmdRegisteredOnRoot(t *testing.T) {
	// verify subcommand registration by init() so the top-level `tptctl down` resolves
	if !hasSubcommand(rootCmd, DownCmd) {
		t.Errorf("DownCmd not registered under rootCmd")
	}
}

// TestDownCmdFlags asserts DownCmd registers each documented flag with the expected default.
func TestDownCmdFlags(t *testing.T) {
	// verify every documented flag exists on the command
	assertFlags(t, DownCmd, []string{"name", "control-plane-only", "infra-only", "aws-config-env"})

	// verify defaults for boolean flags land at false so behavior only changes on explicit opt-in
	cases := []struct {
		flag string
		want string
	}{
		{"name", ""},
		{"control-plane-only", "false"},
		{"infra-only", "false"},
		{"aws-config-env", "false"},
	}
	for _, tc := range cases {
		f := DownCmd.Flags().Lookup(tc.flag)
		if f == nil {
			t.Fatalf("flag %q missing on DownCmd", tc.flag)
		}
		if f.DefValue != tc.want {
			t.Errorf("flag %q default = %q, want %q", tc.flag, f.DefValue, tc.want)
		}
	}
}

// TestDownCmdNameRequired asserts the --name flag is marked required so cobra rejects invocations that omit it.
func TestDownCmdNameRequired(t *testing.T) {
	// verify name flag has the required annotation applied via MarkFlagRequired
	name := DownCmd.Flags().Lookup("name")
	if name == nil {
		t.Fatalf("name flag missing on DownCmd")
	}
	req, ok := name.Annotations[cobra.BashCompOneRequiredFlag]
	if !ok || len(req) == 0 || req[0] != "true" {
		t.Errorf("name flag not marked required on DownCmd (annotations=%v)", name.Annotations)
	}
}

// TestDownCmdNameShorthand asserts the --name flag exposes a -n shorthand.
func TestDownCmdNameShorthand(t *testing.T) {
	// verify shorthand so `tptctl down -n foo` resolves same as `--name foo`
	name := DownCmd.Flags().Lookup("name")
	if name == nil {
		t.Fatalf("name flag missing on DownCmd")
	}
	if name.Shorthand != "n" {
		t.Errorf("name flag shorthand = %q, want %q", name.Shorthand, "n")
	}
}

// TestDownCmdRunAbortOnNegativeConfirmation covers both prompt branches (infra + control-plane-only)
// and asserts Run prints "Aborted." and returns without calling os.Exit when the user answers non-y.
func TestDownCmdRunAbortOnNegativeConfirmation(t *testing.T) {
	cases := []struct {
		name             string
		controlPlaneOnly bool
		reply            string
		wantPromptSubstr string
	}{
		{
			// user rejects a full teardown; prompt mentions infrastructure
			name:             "reject full teardown prints infra prompt then aborts",
			controlPlaneOnly: false,
			reply:            "n\n",
			wantPromptSubstr: "underlying infrastructure",
		},
		{
			// user rejects a control-plane-only teardown; prompt notes infra is left intact
			name:             "reject control-plane-only teardown prints intact prompt then aborts",
			controlPlaneOnly: true,
			reply:            "n\n",
			wantPromptSubstr: "infrastructure will be left intact",
		},
		{
			// empty response (just newline) is treated as no; Run must abort
			name:             "empty response defaults to abort",
			controlPlaneOnly: false,
			reply:            "\n",
			wantPromptSubstr: "underlying infrastructure",
		},
		{
			// any non-y response aborts; verify a stray token still aborts
			name:             "arbitrary token aborts",
			controlPlaneOnly: false,
			reply:            "no\n",
			wantPromptSubstr: "underlying infrastructure",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// arrange: set the shared cliArgs used by Run and restore afterward
			prevName := cliArgs.ControlPlaneName
			prevOnly := cliArgs.ControlPlaneOnly
			cliArgs.ControlPlaneName = "unit-test-cp"
			cliArgs.ControlPlaneOnly = tc.controlPlaneOnly
			t.Cleanup(func() {
				cliArgs.ControlPlaneName = prevName
				cliArgs.ControlPlaneOnly = prevOnly
			})

			// arrange: pipe the canned reply into os.Stdin so bufio.NewReader sees it
			stdin, err := redirectStdin(tc.reply)
			if err != nil {
				t.Fatalf("redirectStdin: %v", err)
			}
			t.Cleanup(stdin.restore)

			// arrange: capture os.Stdout to inspect the prompt and abort message
			stdout, err := redirectStdout()
			if err != nil {
				t.Fatalf("redirectStdout: %v", err)
			}

			// act: invoke Run directly; a non-y reply must return before any os.Exit call
			DownCmd.Run(DownCmd, []string{})

			// assert: prompt reached stdout and the abort branch printed "Aborted."
			out := stdout.stop()
			if !strings.Contains(out, "unit-test-cp") {
				t.Errorf("stdout missing control plane name; got:\n%s", out)
			}
			if !strings.Contains(out, tc.wantPromptSubstr) {
				t.Errorf("stdout missing prompt %q; got:\n%s", tc.wantPromptSubstr, out)
			}
			if !strings.Contains(out, "Aborted.") {
				t.Errorf("stdout missing abort message; got:\n%s", out)
			}
		})
	}
}

// stdinRestorer captures the previous os.Stdin so a test can restore it after piping a canned reply.
type stdinRestorer struct {
	prev  *os.File
	rPipe *os.File
	wPipe *os.File
}

// restore rebinds os.Stdin to the pre-test file and closes the pipe.
func (s *stdinRestorer) restore() {
	os.Stdin = s.prev
	if s.rPipe != nil {
		s.rPipe.Close()
	}
}

// redirectStdin swaps os.Stdin for a pipe pre-loaded with reply so Run's bufio.NewReader sees it.
func redirectStdin(reply string) (*stdinRestorer, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(w, reply); err != nil {
		r.Close()
		w.Close()
		return nil, err
	}
	// close the writer so ReadString returns io.EOF once the reply drains
	w.Close()
	s := &stdinRestorer{prev: os.Stdin, rPipe: r, wPipe: w}
	os.Stdin = r
	return s, nil
}

// stdoutCapture wraps a pipe redirect on os.Stdout so a test can read what Run printed.
type stdoutCapture struct {
	prev *os.File
	r    *os.File
	w    *os.File
	buf  *bytes.Buffer
	wg   *sync.WaitGroup
}

// stop rebinds os.Stdout to the pre-test file and returns everything Run wrote.
func (c *stdoutCapture) stop() string {
	c.w.Close()
	c.wg.Wait()
	os.Stdout = c.prev
	c.r.Close()
	return c.buf.String()
}

// redirectStdout swaps os.Stdout for a pipe and drains it into a buffer on a goroutine.
func redirectStdout() (*stdoutCapture, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	c := &stdoutCapture{
		prev: os.Stdout,
		r:    r,
		w:    w,
		buf:  &bytes.Buffer{},
		wg:   &sync.WaitGroup{},
	}
	os.Stdout = w
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		io.Copy(c.buf, r)
	}()
	return c, nil
}
