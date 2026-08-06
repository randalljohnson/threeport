package root

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	cli "github.com/threeport/threeport/pkg/cli/v0"
	sdk "github.com/threeport/threeport/pkg/sdk/v0"
	"github.com/threeport/threeport/pkg/sdk/v0/gen"
)

//go:embed style-guide.md
var styleGuideContent string

// GenStyleGuide writes the module developer style guide if not already
// present. It lands at the same path the core project keeps its own style
// guide, so a contributor moving between the two looks in one place. The
// embedded source carries the rules that hold for every module and marks the
// sections a module fills in with its own domain terms. The write is skipped
// when the file already exists so threeport's own repo and any module that has
// written its guide are left untouched.
func GenStyleGuide(generator *gen.Generator, sdkConfig *sdk.SdkConfig) error {
	target := filepath.Join("docs", "dev", "style-guide.md")

	if slices.Contains(sdkConfig.ExcludeFiles, target) {
		cli.Info(fmt.Sprintf("source code generation skipped for %s", target))
		return nil
	}

	if _, err := os.Stat(target); err == nil {
		cli.Info(fmt.Sprintf("style guide already exists at %s - not overwritten", target))
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat %s: %w", target, err)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", target, err)
	}

	if err := os.WriteFile(target, []byte(styleGuideContent), 0644); err != nil {
		return fmt.Errorf("failed to write style guide to %s: %w", target, err)
	}
	cli.Info(fmt.Sprintf("style guide written to %s", target))

	return nil
}
