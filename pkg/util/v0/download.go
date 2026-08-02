package v0

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// releaseArchSuffix maps a GOARCH value to the architecture token goreleaser
// embeds in archive names: amd64 to x86_64, 386 to i386, and every other
// architecture to the GOARCH string verbatim.
func releaseArchSuffix(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "386":
		return "i386"
	default:
		return goarch
	}
}

// releaseAssetInfix returns the OS and architecture token goreleaser embeds in
// archive names for goos and goarch, for example "Linux_x86_64" or
// "Darwin_arm64". The OS is title-cased and the architecture follows the
// goreleaser mapping.
func releaseAssetInfix(goos, goarch string) string {
	return titleCaseOS(goos) + "_" + releaseArchSuffix(goarch)
}

// titleCaseOS upper-cases the first letter of a GOOS value to match the
// title-cased OS token goreleaser embeds in archive names, for example linux to
// Linux.
func titleCaseOS(goos string) string {
	if goos == "" {
		return goos
	}
	return strings.ToUpper(goos[:1]) + goos[1:]
}

// githubRelease is the subset of the GitHub release API response the download
// path reads.
type githubRelease struct {
	Assets []githubReleaseAsset `json:"assets"`
}

// githubReleaseAsset is the subset of a GitHub release asset the download path
// reads.
type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// DownloadReleaseBinary downloads binaryName from the tag release of repo (an
// "owner/name" path on github.com) and installs it executable into destDir.
// token may be empty for public repos.
func DownloadReleaseBinary(repo, tag, binaryName, destDir, token string) error {
	// resolve the archive asset matching the running OS and architecture
	assetSuffix := "_" + releaseAssetInfix(runtime.GOOS, runtime.GOARCH) + ".tar.gz"
	releaseURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, tag)
	req, err := http.NewRequest(http.MethodGet, releaseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch release metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch release metadata: unexpected status %s", resp.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("failed to decode release metadata: %w", err)
	}

	// select the archive asset by name suffix
	var downloadURL string
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, assetSuffix) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("failed to find release asset ending in %s for %s %s", assetSuffix, repo, tag)
	}

	// stream the archive and extract the requested binary
	assetReq, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build asset request: %w", err)
	}
	if token != "" {
		assetReq.Header.Set("Authorization", "Bearer "+token)
	}

	assetResp, err := http.DefaultClient.Do(assetReq)
	if err != nil {
		return fmt.Errorf("failed to download release asset: %w", err)
	}
	defer assetResp.Body.Close()

	if assetResp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download release asset: unexpected status %s", assetResp.Status)
	}

	if err := extractBinary(assetResp.Body, binaryName, destDir); err != nil {
		return err
	}

	return nil
}

// extractBinary reads a gzipped tar from r and writes binaryName (matched by
// base name) into destDir, executable.
func extractBinary(r io.Reader, binaryName, destDir string) error {
	gzReader, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to open gzip reader: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		// match the binary by base name to strip the versioned top-level directory
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != binaryName {
			continue
		}

		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}

		destPath := filepath.Join(destDir, binaryName)
		out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return fmt.Errorf("failed to create destination file: %w", err)
		}

		if _, err := io.Copy(out, tarReader); err != nil {
			out.Close()
			return fmt.Errorf("failed to write binary: %w", err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("failed to close binary: %w", err)
		}

		return nil
	}

	return fmt.Errorf("failed to find %s in release archive", binaryName)
}
