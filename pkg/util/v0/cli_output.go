package v0

import (
	"fmt"
	"os"

	aurora "github.com/logrusorgru/aurora"
	term "golang.org/x/term"
)

// CliOutputError prints a formatted error message in red.
func CliOutputError(message string, err error) {
	if err != nil {
		fmt.Println(aurora.Red(fmt.Sprintf("Error: %s\n%s", message, err)))
	} else {
		fmt.Println(aurora.Red(fmt.Sprintf("Error: %s", message)))
	}
}

// CliOutputInfo prints a formatted info message.
func CliOutputInfo(message string) {
	fmt.Printf("Info: %s\n", message)
}

// CliOutputNotice prints a formatted notice message in blue.
func CliOutputNotice(message string) {
	fmt.Println(aurora.Blue(fmt.Sprintf("Notice: %s", message)))
}

// CliOutputWarning prints a formatted warning message in yellow.
func CliOutputWarning(message string) {
	fmt.Println(aurora.Yellow(fmt.Sprintf("Warning: %s", message)))
}

// CliOutputComplete prints a formatted message in green. Used when operations are finished.
func CliOutputComplete(message string) {
	fmt.Println(aurora.Green(fmt.Sprintf("Complete: %s", message)))
}

// CliColorizeWarningInline wraps s in an inline yellow ANSI sequence when
// stdout is a TTY; otherwise it returns s unchanged so redirected output
// stays free of escape bytes. The escape sequences are bracketed by the
// text/tabwriter escape byte 0xff so tabwriter's column-width count treats
// the ANSI bytes as zero-width and columns still align.
func CliColorizeWarningInline(s string) string {
	// leave redirected output uncolored so pipes and files stay clean
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return s
	}

	// bracket the ANSI escape halves with tabwriter's 0xff escape byte so
	// its cell-width count skips the invisible bytes and only sees s
	const (
		tw     = "\xff"
		prefix = "\x1b[33m"
		suffix = "\x1b[0m"
	)
	return tw + prefix + tw + s + tw + suffix + tw
}
