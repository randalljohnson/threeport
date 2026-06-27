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
	"strings"
)

// linuxAssetSuffix is the goreleaser archive name suffix for the Linux amd64
// build that the download path extracts from.
const linuxAssetSuffix = "_Linux_x86_64.tar.gz"

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
	// resolve the Linux archive asset from the tagged release
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

	// select the Linux archive asset by name suffix
	var downloadURL string
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, linuxAssetSuffix) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("failed to find release asset ending in %s for %s %s", linuxAssetSuffix, repo, tag)
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
