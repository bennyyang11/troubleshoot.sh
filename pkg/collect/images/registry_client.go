package images

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultRegistryClient implements the RegistryClient interface
type DefaultRegistryClient struct {
	httpClient  *http.Client
	credentials map[string]*RegistryCredentials
	authTokens  map[string]string // registry -> auth token
	userAgent   string
}

// NewRegistryClient creates a new registry client
func NewRegistryClient(timeout time.Duration) *DefaultRegistryClient {
	return &DefaultRegistryClient{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		credentials: make(map[string]*RegistryCredentials),
		authTokens:  make(map[string]string),
		userAgent:   "troubleshoot.sh/image-collector/1.0",
	}
}

// SetCredentials sets authentication credentials for a registry
func (rc *DefaultRegistryClient) SetCredentials(registry string, creds *RegistryCredentials) {
	rc.credentials[registry] = creds
}

// GetImageFacts retrieves comprehensive metadata for an image
func (rc *DefaultRegistryClient) GetImageFacts(ctx context.Context, imageRef string) (*ImageFacts, error) {
	// Parse image reference
	imgRef, err := rc.parseImageReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference: %w", err)
	}

	// Authenticate with registry
	if err := rc.ensureAuthenticated(ctx, imgRef.Registry); err != nil {
		return nil, fmt.Errorf("failed to authenticate with registry %s: %w", imgRef.Registry, err)
	}

	// Get manifest
	manifest, err := rc.ParseManifest(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}

	// Get image configuration if available
	var imageConfig *ImageConfig
	if manifest.Config.Digest != "" {
		imageConfig, err = rc.getImageConfig(ctx, imgRef, manifest.Config.Digest)
		if err != nil {
			// Log warning but continue - config is optional
			fmt.Printf("Warning: failed to get image config: %v\n", err)
		}
	}

	// Build facts
	facts := &ImageFacts{
		Repository: imgRef.Repository,
		Tag:        imgRef.Tag,
		Digest:     manifest.Config.Digest,
		Registry:   imgRef.Registry,
		Size:       manifest.Config.Size,
		Created:    time.Now(), // Will be updated from config if available
		Labels:     make(map[string]string),
		Platform:   Platform{Architecture: "amd64", OS: "linux"}, // Default, updated from manifest
		Layers:     make([]LayerInfo, 0),
	}

	// Add platform info if available
	if manifest.Platform != nil {
		facts.Platform = *manifest.Platform
	}

	// Add layer information
	for _, layer := range manifest.Layers {
		layerInfo := LayerInfo{
			Digest:      layer.Digest,
			Size:        layer.Size,
			MediaType:   layer.MediaType,
			URLs:        layer.URLs,
			Annotations: layer.Annotations,
		}
		facts.Layers = append(facts.Layers, layerInfo)
	}

	// Add config information if available
	if imageConfig != nil {
		facts.Config = *imageConfig
		if len(imageConfig.Env) > 0 {
			// Extract labels from environment variables if present
			for _, env := range imageConfig.Env {
				if strings.HasPrefix(env, "LABEL_") {
					parts := strings.SplitN(env, "=", 2)
					if len(parts) == 2 {
						key := strings.TrimPrefix(parts[0], "LABEL_")
						facts.Labels[key] = parts[1]
					}
				}
			}
		}
	}

	return facts, nil
}

// ResolveDigest resolves an image reference to its digest
func (rc *DefaultRegistryClient) ResolveDigest(ctx context.Context, imageRef string) (string, error) {
	imgRef, err := rc.parseImageReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}

	if err := rc.ensureAuthenticated(ctx, imgRef.Registry); err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}

	// Build manifest URL
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", imgRef.Registry, imgRef.Repository, imgRef.Tag)

	req, err := http.NewRequestWithContext(ctx, "HEAD", manifestURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication
	rc.addAuthHeader(req, imgRef.Registry)
	
	// Set accept headers for both Docker v2 and OCI formats
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json,application/vnd.oci.image.manifest.v1+json")

	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("manifest request failed with status %d", resp.StatusCode)
	}

	// Get digest from Docker-Content-Digest header
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		digest = resp.Header.Get("Content-Digest")
	}
	
	if digest == "" {
		return "", fmt.Errorf("no digest found in response headers")
	}

	return digest, nil
}

// ParseManifest retrieves and parses an image manifest
func (rc *DefaultRegistryClient) ParseManifest(ctx context.Context, imageRef string) (*ManifestInfo, error) {
	imgRef, err := rc.parseImageReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference: %w", err)
	}

	if err := rc.ensureAuthenticated(ctx, imgRef.Registry); err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}

	// Build manifest URL
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", imgRef.Registry, imgRef.Repository, imgRef.Tag)

	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication
	rc.addAuthHeader(req, imgRef.Registry)
	
	// Set accept headers for both Docker v2 and OCI formats
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json,application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.list.v2+json")

	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Read and parse manifest
	manifestData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	manifest := &ManifestInfo{}
	if err := json.Unmarshal(manifestData, manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	return manifest, nil
}

// Authenticate authenticates with a registry using provided credentials
func (rc *DefaultRegistryClient) Authenticate(ctx context.Context, registry string, credentials *RegistryCredentials) error {
	if credentials == nil {
		return fmt.Errorf("no credentials provided")
	}

	rc.credentials[registry] = credentials

	// For Docker Hub and other registries, we'll do token-based auth
	if credentials.Token != "" {
		rc.authTokens[registry] = credentials.Token
		return nil
	}

	// For username/password auth, we'll do basic auth or token exchange
	if credentials.Username != "" && credentials.Password != "" {
		// Try to get a token from the registry auth service
		token, err := rc.getAuthToken(ctx, registry, credentials)
		if err != nil {
			// Fall back to basic auth
			fmt.Printf("Warning: failed to get auth token, using basic auth: %v\n", err)
			basicAuth := base64.StdEncoding.EncodeToString([]byte(credentials.Username + ":" + credentials.Password))
			rc.authTokens[registry] = "Basic " + basicAuth
		} else {
			rc.authTokens[registry] = "Bearer " + token
		}
		return nil
	}

	return fmt.Errorf("invalid credentials: must provide either token or username/password")
}

// SupportsRegistry checks if the registry is supported
func (rc *DefaultRegistryClient) SupportsRegistry(registry string) bool {
	supportedRegistries := map[string]bool{
		"docker.io":                     true,
		"index.docker.io":               true,
		"registry-1.docker.io":          true,
		"gcr.io":                        true,
		"us.gcr.io":                     true,
		"eu.gcr.io":                     true,
		"asia.gcr.io":                   true,
		"quay.io":                       true,
		"ghcr.io":                       true,
		"registry.k8s.io":               true,
		"k8s.gcr.io":                    true,
	}

	// Check exact match first
	if supportedRegistries[registry] {
		return true
	}

	// Check for AWS ECR pattern
	if strings.Contains(registry, ".amazonaws.com") && strings.Contains(registry, ".dkr.ecr.") {
		return true
	}

	// Check for Azure Container Registry pattern
	if strings.HasSuffix(registry, ".azurecr.io") {
		return true
	}

	// Check for Harbor registry pattern (common self-hosted pattern)
	if strings.Contains(registry, "harbor") {
		return true
	}

	// Default: assume registry is supported (be permissive)
	return true
}

// Helper methods

func (rc *DefaultRegistryClient) parseImageReference(imageRef string) (*ImageReference, error) {
	// Handle references like:
	// nginx:latest
	// docker.io/library/nginx:latest  
	// gcr.io/my-project/my-app:v1.0.0
	// my-registry.com:5000/my-app@sha256:abc123

	ref := &ImageReference{
		Original: imageRef,
	}

	// Check for digest
	if strings.Contains(imageRef, "@sha256:") {
		parts := strings.Split(imageRef, "@")
		imageRef = parts[0]
		ref.Digest = parts[1]
	}

	// Split by colon for tag
	var repo string
	if strings.Contains(imageRef, ":") && !strings.Contains(imageRef, "://") {
		lastColon := strings.LastIndex(imageRef, ":")
		repo = imageRef[:lastColon]
		ref.Tag = imageRef[lastColon+1:]
		
		// Check if what we think is a tag is actually part of a registry port
		if strings.Contains(ref.Tag, "/") {
			// This is a registry with port, no tag specified
			repo = imageRef
			ref.Tag = "latest"
		}
	} else {
		repo = imageRef
		ref.Tag = "latest"
	}

	// Split registry and repository
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) == 1 {
		// No registry specified, assume Docker Hub
		ref.Registry = "index.docker.io"
		ref.Repository = "library/" + parts[0] // Docker Hub library namespace
	} else if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
		// First part looks like a registry (has . or :)
		ref.Registry = parts[0]
		ref.Repository = parts[1]
	} else {
		// Assume Docker Hub with user namespace
		ref.Registry = "index.docker.io"
		ref.Repository = repo
	}

	return ref, nil
}

func (rc *DefaultRegistryClient) ensureAuthenticated(ctx context.Context, registry string) error {
	// Check if we already have a token
	if _, exists := rc.authTokens[registry]; exists {
		return nil
	}

	// Check if we have credentials for this registry
	creds, exists := rc.credentials[registry]
	if !exists {
		// Try to authenticate with default/anonymous access
		return nil
	}

	return rc.Authenticate(ctx, registry, creds)
}

func (rc *DefaultRegistryClient) addAuthHeader(req *http.Request, registry string) {
	if token, exists := rc.authTokens[registry]; exists {
		if strings.HasPrefix(token, "Basic ") || strings.HasPrefix(token, "Bearer ") {
			req.Header.Set("Authorization", token)
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
}

func (rc *DefaultRegistryClient) getAuthToken(ctx context.Context, registry string, creds *RegistryCredentials) (string, error) {
	// This is a simplified token exchange - in a full implementation,
	// this would handle the Docker Registry HTTP API V2 authentication flow
	
	// For Docker Hub
	if registry == "index.docker.io" || registry == "docker.io" {
		return rc.getDockerHubToken(ctx, creds)
	}
	
	// For other registries, try generic OAuth2-style token endpoint
	authURL := fmt.Sprintf("https://%s/v2/token", registry)
	
	req, err := http.NewRequestWithContext(ctx, "GET", authURL, nil)
	if err != nil {
		return "", err
	}
	
	// Add basic auth
	req.SetBasicAuth(creds.Username, creds.Password)
	req.Header.Set("User-Agent", rc.userAgent)
	
	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed with status %d", resp.StatusCode)
	}
	
	var tokenResp struct {
		Token string `json:"token"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}
	
	return tokenResp.Token, nil
}

func (rc *DefaultRegistryClient) getDockerHubToken(ctx context.Context, creds *RegistryCredentials) (string, error) {
	authURL := "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/alpine:pull"
	
	req, err := http.NewRequestWithContext(ctx, "GET", authURL, nil)
	if err != nil {
		return "", err
	}
	
	if creds.Username != "" && creds.Password != "" {
		req.SetBasicAuth(creds.Username, creds.Password)
	}
	
	req.Header.Set("User-Agent", rc.userAgent)
	
	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Docker Hub auth failed with status %d", resp.StatusCode)
	}
	
	var tokenResp struct {
		Token string `json:"token"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse Docker Hub token response: %w", err)
	}
	
	return tokenResp.Token, nil
}

func (rc *DefaultRegistryClient) getImageConfig(ctx context.Context, imgRef *ImageReference, configDigest string) (*ImageConfig, error) {
	configURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", imgRef.Registry, imgRef.Repository, configDigest)
	
	req, err := http.NewRequestWithContext(ctx, "GET", configURL, nil)
	if err != nil {
		return nil, err
	}
	
	rc.addAuthHeader(req, imgRef.Registry)
	req.Header.Set("Accept", "application/vnd.docker.container.image.v1+json")
	
	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("config request failed with status %d", resp.StatusCode)
	}
	
	configData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	// Parse Docker image config format
	var dockerConfig struct {
		Config ImageConfig `json:"config"`
	}
	
	if err := json.Unmarshal(configData, &dockerConfig); err != nil {
		return nil, fmt.Errorf("failed to parse image config: %w", err)
	}
	
	return &dockerConfig.Config, nil
}

// GetRegistryFromImageRef extracts registry from image reference
func GetRegistryFromImageRef(imageRef string) string {
	ref, err := (&DefaultRegistryClient{}).parseImageReference(imageRef)
	if err != nil {
		return "unknown"
	}
	return ref.Registry
}

// NormalizeImageReference normalizes an image reference to a canonical form
func NormalizeImageReference(imageRef string) (string, error) {
	ref, err := (&DefaultRegistryClient{}).parseImageReference(imageRef)
	if err != nil {
		return "", err
	}
	
	if ref.Digest != "" {
		return fmt.Sprintf("%s/%s@%s", ref.Registry, ref.Repository, ref.Digest), nil
	}
	return fmt.Sprintf("%s/%s:%s", ref.Registry, ref.Repository, ref.Tag), nil
}
