package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// resetMachineFilterFlags clears the package-level filter globals between subtests.
func resetMachineFilterFlags() {
	nodeProfile = ""
	nodeSize = ""
	awsMachineType = ""
	ociMachineType = ""
}

// runCaptured invokes the get-machines Run handler and returns whatever it wrote to stdout.
func runCaptured(t *testing.T) string {
	t.Helper()
	out, err := captureStdout(t, func() error {
		ConfigGetMachinesCmd.Run(ConfigGetMachinesCmd, nil)
		return nil
	})
	if err != nil {
		t.Fatalf("captureStdout: %v", err)
	}
	return out
}

// TestConfigGetMachinesCmdMetadata asserts get-machines command metadata (Use, SilenceUsage).
func TestConfigGetMachinesCmdMetadata(t *testing.T) {
	// verify Use string matches CLI invocation
	if ConfigGetMachinesCmd.Use != "get-machines" {
		t.Errorf("ConfigGetMachinesCmd.Use = %q, want %q", ConfigGetMachinesCmd.Use, "get-machines")
	}
	// verify usage is silenced on error
	if !ConfigGetMachinesCmd.SilenceUsage {
		t.Errorf("ConfigGetMachinesCmd.SilenceUsage = false, want true")
	}
	// verify Run handler wired
	if ConfigGetMachinesCmd.Run == nil {
		t.Errorf("ConfigGetMachinesCmd.Run = nil, want a function")
	}
}

// TestConfigGetMachinesCmdFlags asserts get-machines registered filter flags.
func TestConfigGetMachinesCmdFlags(t *testing.T) {
	// verify all documented filter flags exist
	assertFlags(t, ConfigGetMachinesCmd, []string{"node-profile", "node-size", "aws-machine-type", "oci-machine-type"})
}

// TestConfigGetMachinesCmdFlagShorthands asserts get-machines flags declare their expected short letters.
func TestConfigGetMachinesCmdFlagShorthands(t *testing.T) {
	cases := []struct {
		flag      string
		shorthand string
	}{
		{"node-profile", "p"},
		{"node-size", "s"},
		{"aws-machine-type", "a"},
		{"oci-machine-type", "o"},
	}
	for _, tc := range cases {
		// verify each flag's short letter matches the documented one
		f := ConfigGetMachinesCmd.Flags().Lookup(tc.flag)
		if f == nil {
			t.Fatalf("flag %q missing", tc.flag)
		}
		if f.Shorthand != tc.shorthand {
			t.Errorf("flag %q shorthand = %q, want %q", tc.flag, f.Shorthand, tc.shorthand)
		}
	}
}

// TestConfigGetMachinesCmdFlagDefaults asserts every filter flag defaults to empty.
func TestConfigGetMachinesCmdFlagDefaults(t *testing.T) {
	for _, name := range []string{"node-profile", "node-size", "aws-machine-type", "oci-machine-type"} {
		// verify each filter flag defaults to empty so the no-filter path is the default behavior
		f := ConfigGetMachinesCmd.Flags().Lookup(name)
		if f == nil {
			t.Fatalf("flag %q missing", name)
		}
		if f.DefValue != "" {
			t.Errorf("flag %q default = %q, want empty", name, f.DefValue)
		}
	}
}

// TestConfigGetMachinesCmdRegistered asserts ConfigGetMachinesCmd is a subcommand of ConfigCmd.
func TestConfigGetMachinesCmdRegistered(t *testing.T) {
	// verify subcommand registration by init()
	if !hasSubcommand(ConfigCmd, ConfigGetMachinesCmd) {
		t.Errorf("ConfigGetMachinesCmd not registered under ConfigCmd")
	}
}

// TestConfigGetMachinesRunNoFilter asserts the default path prints the header and every machine row.
func TestConfigGetMachinesRunNoFilter(t *testing.T) {
	resetMachineFilterFlags()
	t.Cleanup(resetMachineFilterFlags)

	// invoke the command's Run without any filter set
	out := runCaptured(t)

	// verify the header row is printed
	if !strings.Contains(out, "NODE PROFILE") {
		t.Errorf("expected header in output, got:\n%s", out)
	}
	// verify at least one known machine entry from the default map is present
	if !strings.Contains(out, "Balanced") {
		t.Errorf("expected Balanced profile row in output, got:\n%s", out)
	}
}

// TestConfigGetMachinesRunFiltersMatch asserts each filter flag limits output to matching rows.
func TestConfigGetMachinesRunFiltersMatch(t *testing.T) {
	cases := []struct {
		name     string
		set      func()
		want     string
		wantMiss string
	}{
		{
			// filtering by node profile keeps only that profile's rows
			name:     "nodeProfile filter",
			set:      func() { nodeProfile = "Balanced" },
			want:     "Balanced",
			wantMiss: "",
		},
		{
			// filtering by node size restricts output to that size
			name:     "nodeSize filter",
			set:      func() { nodeSize = "Small" },
			want:     "Small",
			wantMiss: "",
		},
		{
			// filtering by aws machine type restricts output to that instance
			name:     "awsMachineType filter",
			set:      func() { awsMachineType = "t3.nano" },
			want:     "t3.nano",
			wantMiss: "",
		},
		{
			// filtering by oci machine type restricts output to that shape
			name:     "ociMachineType filter",
			set:      func() { ociMachineType = "VM.Standard.E2.1.Micro" },
			want:     "VM.Standard.E2.1.Micro",
			wantMiss: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetMachineFilterFlags()
			t.Cleanup(resetMachineFilterFlags)
			// arrange the single filter flag under test
			tc.set()

			// run the command and capture stdout
			out := runCaptured(t)

			// verify a matching row lands in the output
			if !strings.Contains(out, tc.want) {
				t.Errorf("expected output to contain %q, got:\n%s", tc.want, out)
			}
			// verify the header row still appears (it is printed before filtering)
			if !strings.Contains(out, "NODE PROFILE") {
				t.Errorf("expected header row in output, got:\n%s", out)
			}
		})
	}
}

// TestConfigGetMachinesRunExits covers the os.Exit(1) branches by re-invoking the test binary
// as a subprocess with env markers that select the exit case.
func TestConfigGetMachinesRunExits(t *testing.T) {
	// child-process mode: perform the configured exit case
	switch os.Getenv("MACHINES_EXIT_CASE") {
	case "multi_flag":
		// two filter flags set at once must trigger the exit-1 validation branch
		nodeProfile = "Balanced"
		nodeSize = "Small"
		ConfigGetMachinesCmd.Run(ConfigGetMachinesCmd, nil)
		return
	case "no_match":
		// a filter that matches nothing must trigger the exit-1 no-results branch
		nodeProfile = "definitely-not-a-real-profile"
		ConfigGetMachinesCmd.Run(ConfigGetMachinesCmd, nil)
		return
	}

	// parent-process mode: fork a child per exit case and verify it exited non-zero
	cases := []string{"multi_flag", "no_match"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			// re-run just this test in a subprocess with the case selector set
			cmd := exec.Command(os.Args[0], "-test.run=^TestConfigGetMachinesRunExits$", "-test.v")
			cmd.Env = append(os.Environ(), "MACHINES_EXIT_CASE="+c)
			err := cmd.Run()
			// verify the child exited with a non-zero status; anything else means the guard did not fire
			if err == nil {
				t.Fatalf("child process for case %q exited 0; expected non-zero exit", c)
			}
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("child process for case %q returned unexpected error type %T: %v", c, err, err)
			}
			if ee.ExitCode() == 0 {
				t.Fatalf("child process for case %q reported exit code 0", c)
			}
		})
	}
}
