package v0

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// GCP OAuth2 configuration for Application Default Credentials.
// These are the public client credentials used by gcloud CLI for user authentication.
// See: https://cloud.google.com/sdk/docs/authorizing
const (
	gcpOAuthClientID     = "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com"
	gcpOAuthClientSecret = "d-FL95Q19q7MQmFpd7hHD0Ty"
)

// GcpOAuthScopes defines the scopes needed for GCP operations.
var GcpOAuthScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
}

// adcCredentials represents the structure of the Application Default Credentials file.
type adcCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	Type         string `json:"type"`
}

// EnsureGCPAuth ensures GCP credentials for a controller-side caller and never
// opens a browser. When serviceAccountCredentials is non-empty, it validates
// that JSON in memory only; the caller passes the same JSON into each GCP
// client per call so concurrent operations for different accounts stay
// independent. Otherwise it requires application default credentials already
// on the machine, such as Workload Identity or a gcloud user login. Without
// either, it returns an error immediately: a controller pod has no browser and
// the OAuth flow would wait five minutes before timing out.
func EnsureGCPAuth(serviceAccountCredentials string) error {
	return ensureGCPAuth(serviceAccountCredentials, false)
}

// EnsureGCPAuthWithBrowser ensures GCP credentials for an interactive CLI
// caller. It follows the same service-account and ambient paths as the
// non-browser entry point, then opens a browser OAuth flow when neither is
// available. Controller paths must not call this: a pod has no browser and the
// fallback waits five minutes before timing out.
func EnsureGCPAuthWithBrowser(serviceAccountCredentials string) error {
	return ensureGCPAuth(serviceAccountCredentials, true)
}

// ensureGCPAuth ensures GCP credentials, opening a browser only when
// interactive is true and no usable credentials exist.
func ensureGCPAuth(serviceAccountCredentials string, interactive bool) error {
	ctx := context.Background()

	// validate service account JSON in memory and return; caller threads it per call
	if serviceAccountCredentials != "" {
		return validateServiceAccountCredentials(ctx, serviceAccountCredentials)
	}

	// accept ambient credentials from workload identity or gcloud user login
	if hasValidGCPCredentials(ctx) {
		return nil
	}

	// refuse the browser flow for non-interactive callers; a pod has no browser
	if !interactive {
		return errors.New("gcp authentication unavailable: no ambient application default credentials and no service account credentials configured")
	}

	util.CliOutputInfo("GCP credentials not found or expired. Initiating authentication...")

	// run browser OAuth and write application default credentials
	if err := performGCPOAuthFlow(ctx); err != nil {
		return fmt.Errorf("failed to authenticate with GCP: %w", err)
	}

	util.CliOutputInfo("GCP authentication successful!")
	return nil
}

// validateServiceAccountCredentials parses service-account JSON for
// the configured OAuth scopes. A well-formed document with a bad
// private key fails later, when a token is first requested.
func validateServiceAccountCredentials(ctx context.Context, credentialsJSON string) error {
	if _, err := google.CredentialsFromJSON(ctx, []byte(credentialsJSON), GcpOAuthScopes...); err != nil {
		return fmt.Errorf("failed to parse service account credentials: %w", err)
	}
	return nil
}

// hasValidGCPCredentials reports whether application default credentials on the
// machine can mint a live token that includes cloud-platform.
func hasValidGCPCredentials(ctx context.Context) bool {
	tokenSource, err := google.DefaultTokenSource(ctx, GcpOAuthScopes...)
	if err != nil {
		return false
	}

	// mint an access token from the ambient credentials
	token, err := tokenSource.Token()
	if err != nil {
		return false
	}

	// reject an expired or empty access token
	if !token.Valid() {
		return false
	}

	return gcpTokenHasCloudPlatformScope(token)
}

// tokeninfoClient is a dedicated HTTP client for scope checks with a short
// timeout so a stalled tokeninfo response cannot block EnsureGCPAuth for long.
var tokeninfoClient = &http.Client{Timeout: 5 * time.Second}

// gcpTokenHasCloudPlatformScope verifies the access token includes the
// cloud-platform scope by querying the Google tokeninfo endpoint.
func gcpTokenHasCloudPlatformScope(token *oauth2.Token) bool {
	// query Google tokeninfo for the token's granted scopes
	resp, err := tokeninfoClient.Get("https://oauth2.googleapis.com/tokeninfo?access_token=" + token.AccessToken)
	if err != nil {
		// treat an unreachable tokeninfo endpoint as in-scope
		return true
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var info struct {
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		// treat an unreadable tokeninfo body as in-scope
		return true
	}

	// accept only when cloud-platform is among the granted scopes
	for _, s := range strings.Fields(info.Scope) {
		if s == "https://www.googleapis.com/auth/cloud-platform" {
			return true
		}
	}
	return false
}

// performGCPOAuthFlow performs the browser-based OAuth flow for GCP authentication.
func performGCPOAuthFlow(ctx context.Context) error {
	state, err := generateRandomState()
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://localhost:%d/callback", port)

	oauth2Config := &oauth2.Config{
		ClientID:     gcpOAuthClientID,
		ClientSecret: gcpOAuthClientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       GcpOAuthScopes,
	}

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errChan <- fmt.Errorf("invalid state parameter")
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			return
		}

		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errChan <- fmt.Errorf("OAuth error: %s - %s", errMsg, r.URL.Query().Get("error_description"))
			http.Error(w, "Authentication failed", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errChan <- fmt.Errorf("no authorization code received")
			http.Error(w, "No authorization code received", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><h1>Authentication Successful!</h1><p>You can close this window and return to the terminal.</p></body></html>`)
		codeChan <- code
	})

	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			errChan <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	authURL := oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	util.CliOutputNotice("Opening browser for GCP authentication...")
	util.CliOutputInfo("If the browser doesn't open automatically, please visit:")
	util.CliOutputInfo(authURL)
	fmt.Println()

	if err := openBrowser(authURL); err != nil {
		util.CliOutputWarning("Failed to open browser automatically. Please open the URL above manually.")
	}

	var code string
	select {
	case code = <-codeChan:
	case err := <-errChan:
		server.Shutdown(ctx)
		return err
	case <-time.After(5 * time.Minute):
		server.Shutdown(ctx)
		return fmt.Errorf("authentication timed out after 5 minutes")
	}

	server.Shutdown(ctx)

	token, err := oauth2Config.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	if err := saveADCCredentials(token); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	return nil
}

// saveADCCredentials saves the OAuth2 token as Application Default Credentials.
func saveADCCredentials(token *oauth2.Token) error {
	adcPath, err := getADCPath()
	if err != nil {
		return err
	}

	adcDir := filepath.Dir(adcPath)
	if err := os.MkdirAll(adcDir, 0700); err != nil {
		return fmt.Errorf("failed to create ADC directory: %w", err)
	}

	creds := adcCredentials{
		ClientID:     gcpOAuthClientID,
		ClientSecret: gcpOAuthClientSecret,
		RefreshToken: token.RefreshToken,
		Type:         "authorized_user",
	}

	credsJSON, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	if err := os.WriteFile(adcPath, credsJSON, 0600); err != nil {
		return fmt.Errorf("failed to write ADC file: %w", err)
	}

	return nil
}

// getADCPath returns the well-known gcloud Application Default
// Credentials file path. It ignores GOOGLE_APPLICATION_CREDENTIALS
// so an OAuth save cannot overwrite the file that variable names.
func getADCPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	if runtime.GOOS == "windows" {
		return filepath.Join(homeDir, "AppData", "Roaming", "gcloud", "application_default_credentials.json"), nil
	}

	return filepath.Join(homeDir, ".config", "gcloud", "application_default_credentials.json"), nil
}

// generateRandomState generates a random state string for CSRF protection.
func generateRandomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// openBrowser opens the specified URL in the default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		if _, err := exec.LookPath("xdg-open"); err == nil {
			cmd = exec.Command("xdg-open", url)
		} else if _, err := exec.LookPath("gnome-open"); err == nil {
			cmd = exec.Command("gnome-open", url)
		} else if _, err := exec.LookPath("kde-open"); err == nil {
			cmd = exec.Command("kde-open", url)
		} else {
			return fmt.Errorf("no browser opener found")
		}
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", strings.ReplaceAll(url, "&", "^&"))
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
