package v0

import (
	"fmt"
	"io"
	"os"
)

// ReadConfigContent reads configuration content from either stdin or a file
// path. When useStdin is true, it reads from os.Stdin. When false, it reads
// from the file at configPath.
func ReadConfigContent(configPath string, useStdin bool) ([]byte, error) {
	if configPath != "" && useStdin {
		return nil, fmt.Errorf("cannot use both --config and --stdin flags")
	}

	if useStdin {
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read from stdin: %w", err)
		}
		if len(content) == 0 {
			return nil, fmt.Errorf("no input received from stdin")
		}
		return content, nil
	}

	if configPath == "" {
		return nil, fmt.Errorf("config path is required when not using stdin")
	}

	return os.ReadFile(configPath)
}