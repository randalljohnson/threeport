/*
Copyright © 2023 Threeport admin@threeport.io
*/
package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// validateSingleFilterFlag returns an error naming every flag the caller set
// when more than one is set. Commands whose local flags are all filters use it
// to enforce that only one filter applies at a time.
//
// The flag names come from cobra rather than from a list written out beside
// the check, so they cannot disagree with the declarations. A hand-written
// list can and did: get-machines named the get-locations flags.
//
// LocalFlags excludes flags inherited from a parent command, so a global like
// --threeport-config does not count as a filter. Reading cmd.Flags() instead
// would count it, because mergePersistentFlags folds inherited flags in.
//
// A command that gains a local flag which is not a filter needs this check
// changed with it; today every local flag on the commands that call it is a
// filter.
func validateSingleFilterFlag(cmd *cobra.Command) error {
	var provided []string
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			provided = append(provided, "--"+f.Name)
		}
	})

	if len(provided) > 1 {
		return fmt.Errorf(
			"only one filter flag can be provided at a time. Provided flags: %s",
			strings.Join(provided, ", "),
		)
	}
	return nil
}
