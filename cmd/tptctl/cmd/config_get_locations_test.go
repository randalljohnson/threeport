/*
Copyright © 2023 Threeport admin@threeport.io
*/
package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureLocationsStdout redirects os.Stdout while fn runs, returning what fn
// wrote. The Run closure writes directly to os.Stdout via tabwriter, so tests
// swap the descriptor to observe its output.
func captureLocationsStdout(t *testing.T, fn func()) string {
	t.Helper()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	// drain the pipe on a background goroutine so writes never block
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	// restore stdout and close the writer so the drain goroutine returns
	_ = w.Close()
	os.Stdout = origStdout
	return <-done
}

// TestConfigGetLocationsCmdMetadata asserts the command carries the expected
// use, short summary, silence-usage flag, and Run hook.
func TestConfigGetLocationsCmdMetadata(t *testing.T) {
	// verify cobra fields wired at package init
	if got, want := ConfigGetLocationsCmd.Use, "get-locations"; got != want {
		t.Errorf("Use = %q, want %q", got, want)
	}
	if ConfigGetLocationsCmd.Short == "" {
		t.Error("Short is empty; expected a short description")
	}
	if ConfigGetLocationsCmd.Long == "" {
		t.Error("Long is empty; expected a long description")
	}
	if ConfigGetLocationsCmd.Example == "" {
		t.Error("Example is empty; expected an example invocation")
	}

	// verify silence-usage stays on so cobra doesn't print usage on error
	if !ConfigGetLocationsCmd.SilenceUsage {
		t.Error("SilenceUsage = false, want true")
	}

	// verify Run is registered so cobra dispatches into it
	if ConfigGetLocationsCmd.Run == nil {
		t.Error("Run is nil; expected a Run function")
	}
}

// TestConfigGetLocationsCmdFlagsRegistered asserts each filter flag is
// registered with the expected long/short forms and default empty value.
func TestConfigGetLocationsCmdFlagsRegistered(t *testing.T) {
	cases := []struct {
		long, short string
	}{
		{"location", "l"},
		{"continent", "c"},
		{"aws-region", "a"},
		{"oci-region", "o"},
	}
	// walk each expected flag and confirm registration
	for _, c := range cases {
		f := ConfigGetLocationsCmd.Flags().Lookup(c.long)
		if f == nil {
			t.Errorf("flag --%s not registered", c.long)
			continue
		}
		if f.Shorthand != c.short {
			t.Errorf("flag --%s shorthand = %q, want %q", c.long, f.Shorthand, c.short)
		}
		if f.DefValue != "" {
			t.Errorf("flag --%s default = %q, want empty", c.long, f.DefValue)
		}
	}
}

// TestConfigGetLocationsCmdRegisteredOnParent asserts the command is wired
// as a subcommand of ConfigCmd via init().
func TestConfigGetLocationsCmdRegisteredOnParent(t *testing.T) {
	// walk the parent command's children looking for get-locations
	var found bool
	for _, sub := range ConfigCmd.Commands() {
		if sub == ConfigGetLocationsCmd {
			found = true
			break
		}
	}
	if !found {
		t.Error("ConfigGetLocationsCmd not registered under ConfigCmd")
	}
}

// resetLocationFlags clears the package-scope filter variables so a prior
// test case's assignment does not leak into the next.
func resetLocationFlags() {
	locationName = ""
	locationContinent = ""
	locationAwsRegion = ""
	locationOciRegion = ""
}

// TestConfigGetLocationsCmdRunDefault asserts running with no filter flags
// prints the header and every entry from the region map.
func TestConfigGetLocationsCmdRunDefault(t *testing.T) {
	// clear any leftover filter state before invoking Run
	resetLocationFlags()
	defer resetLocationFlags()

	// capture stdout while Run executes with no filters set
	out := captureLocationsStdout(t, func() {
		ConfigGetLocationsCmd.Run(ConfigGetLocationsCmd, nil)
	})

	// verify the header line is present
	if !strings.Contains(out, "LOCATION") || !strings.Contains(out, "AWS REGION") || !strings.Contains(out, "OCI REGION") {
		t.Errorf("output missing header columns: %q", out)
	}
	// verify at least one well-known entry from the region map is present
	if !strings.Contains(out, "Local") {
		t.Errorf("output missing 'Local' row: %q", out)
	}
	if !strings.Contains(out, "NorthAmerica:NewYork") {
		t.Errorf("output missing 'NorthAmerica:NewYork' row: %q", out)
	}
}

// TestConfigGetLocationsCmdRunFilters exercises each single-flag filter path
// and confirms only matching rows land in the output.
func TestConfigGetLocationsCmdRunFilters(t *testing.T) {
	cases := []struct {
		name           string
		setFlag        func()
		mustContain    []string
		mustNotContain []string
	}{
		{
			name:           "filter by exact location name",
			setFlag:        func() { locationName = "NorthAmerica:NewYork" },
			mustContain:    []string{"NorthAmerica:NewYork"},
			mustNotContain: []string{"NorthAmerica:Chicago"},
		},
		{
			name:           "filter by continent prefix",
			setFlag:        func() { locationContinent = "NorthAmerica" },
			mustContain:    []string{"NorthAmerica:NewYork", "NorthAmerica:Chicago"},
			mustNotContain: []string{"Local"},
		},
		{
			name:        "filter by aws region",
			setFlag:     func() { locationAwsRegion = "us-east-1" },
			mustContain: []string{"us-east-1"},
		},
		{
			name:        "filter by oci region",
			setFlag:     func() { locationOciRegion = "us-ashburn-1" },
			mustContain: []string{"us-ashburn-1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// clear filters and apply just the one under test
			resetLocationFlags()
			defer resetLocationFlags()
			tc.setFlag()

			// capture stdout while Run executes with the single filter set
			out := captureLocationsStdout(t, func() {
				ConfigGetLocationsCmd.Run(ConfigGetLocationsCmd, nil)
			})

			// verify every required substring appears
			for _, want := range tc.mustContain {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q: %q", want, out)
				}
			}
			// verify excluded substrings do not appear
			for _, avoid := range tc.mustNotContain {
				if strings.Contains(out, avoid) {
					t.Errorf("output unexpectedly contains %q: %q", avoid, out)
				}
			}
			// verify the header still lands regardless of filter
			if !strings.Contains(out, "LOCATION") {
				t.Errorf("output missing header: %q", out)
			}
		})
	}
}

// TestConfigGetLocationsCmdContinentSplitsOnColon verifies the continent
// filter matches only the token before the ':' in the location name, so an
// entry with no ':' (like "Local") never satisfies a continent filter.
func TestConfigGetLocationsCmdContinentSplitsOnColon(t *testing.T) {
	// filter by "Local" as if it were a continent
	resetLocationFlags()
	defer resetLocationFlags()
	locationContinent = "NorthAmerica"

	// capture stdout while Run executes with the continent filter
	out := captureLocationsStdout(t, func() {
		ConfigGetLocationsCmd.Run(ConfigGetLocationsCmd, nil)
	})

	// verify the header still lands
	if !strings.Contains(out, "LOCATION") {
		t.Errorf("output missing header: %q", out)
	}
	// verify a same-continent entry is present
	if !strings.Contains(out, "NorthAmerica:NewYork") {
		t.Errorf("continent match missing: %q", out)
	}
	// verify the 'Local' row (no continent prefix) is excluded
	// scan line-by-line so we don't false-match a substring inside another row
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Local ") {
			t.Errorf("Local row leaked into continent-filtered output: %q", line)
		}
	}
}
