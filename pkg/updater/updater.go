package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	githubOwner = "Constellation-Overwatch"
	githubRepo  = "constellation-overwatch"
	apiURL      = "https://api.github.com/repos/" + githubOwner + "/" + githubRepo + "/releases/latest"
)

// Release represents a GitHub release
type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a release asset
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Update checks for and applies updates to the overwatch binary
func Update(currentVersion string, force bool) error {
	fmt.Println("Checking for updates...")

	release, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion = strings.TrimPrefix(currentVersion, "v")

	if !force && latestVersion == currentVersion {
		fmt.Printf("Already running the latest version (%s)\n", currentVersion)
		return nil
	}

	if !force && currentVersion != "dev" {
		fmt.Printf("New version available: %s (current: %s)\n", latestVersion, currentVersion)
	} else if currentVersion == "dev" {
		fmt.Printf("Development build detected. Updating to latest release: %s\n", latestVersion)
	}

	// Find the right asset for this platform
	assetName := getAssetName(release.TagName)
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	fmt.Printf("Downloading %s...\n", assetName)

	// Download to temp file
	tmpDir, err := os.MkdirTemp("", "overwatch-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, assetName)
	if err := downloadFile(archivePath, downloadURL); err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}

	fmt.Println("Extracting...")

	// Extract the binary
	binaryPath, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		return fmt.Errorf("failed to extract update: %w", err)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	fmt.Printf("Updating %s...\n", execPath)

	// Replace the binary
	if err := replaceBinary(execPath, binaryPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	fmt.Printf("Successfully updated to version %s\n", latestVersion)
	return nil
}

// CheckUpdate checks if an update is available without applying it
func CheckUpdate(currentVersion string) (*Release, bool, error) {
	release, err := getLatestRelease()
	if err != nil {
		return nil, false, err
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion = strings.TrimPrefix(currentVersion, "v")

	if currentVersion == "dev" {
		return release, true, nil
	}

	return release, latestVersion != currentVersion, nil
}

func getLatestRelease() (*Release, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "overwatch-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func getAssetName(tag string) string {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("overwatch_%s_%s_%s.%s", tag, runtime.GOOS, runtime.GOARCH, ext)
}

func downloadFile(filepath string, url string) error {
	client := &http.Client{Timeout: 5 * time.Minute}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func extractBinary(archivePath, destDir string) (string, error) {
	binaryName := "overwatch"
	if runtime.GOOS == "windows" {
		binaryName = "overwatch.exe"
	}

	if strings.HasSuffix(archivePath, ".zip") {
		return extractFromZip(archivePath, destDir, binaryName)
	}
	return extractFromTarGz(archivePath, destDir, binaryName)
}

func extractFromTarGz(archivePath, destDir, binaryName string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		// Look for the binary (might be in a subdirectory)
		if filepath.Base(header.Name) == binaryName && header.Typeflag == tar.TypeReg {
			outPath := filepath.Join(destDir, binaryName)
			outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY, 0755)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", err
			}
			outFile.Close()
			return outPath, nil
		}
	}

	return "", fmt.Errorf("binary %s not found in archive", binaryName)
}

func extractFromZip(archivePath, destDir, binaryName string) (string, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer r.Close()

	for _, f := range r.File {
		if filepath.Base(f.Name) == binaryName && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()

			outPath := filepath.Join(destDir, binaryName)
			outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY, 0755)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(outFile, rc); err != nil {
				outFile.Close()
				return "", err
			}
			outFile.Close()
			return outPath, nil
		}
	}

	return "", fmt.Errorf("binary %s not found in archive", binaryName)
}

func replaceBinary(oldPath, newPath string) error {
	// On Windows, we can't replace a running binary directly
	// We need to rename the old one first
	if runtime.GOOS == "windows" {
		backupPath := oldPath + ".old"
		os.Remove(backupPath) // Remove any existing backup
		if err := os.Rename(oldPath, backupPath); err != nil {
			return fmt.Errorf("failed to backup old binary: %w", err)
		}
	}

	// Read new binary
	newBinary, err := os.ReadFile(newPath)
	if err != nil {
		return err
	}

	// Get original file permissions
	info, err := os.Stat(oldPath)
	mode := os.FileMode(0755)
	if err == nil {
		mode = info.Mode()
	}

	// Write new binary
	if err := os.WriteFile(oldPath, newBinary, mode); err != nil {
		// On Unix, try atomic rename
		if runtime.GOOS != "windows" {
			return os.Rename(newPath, oldPath)
		}
		return err
	}

	return nil
}
