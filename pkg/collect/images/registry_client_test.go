package images

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDefaultRegistryClient_ParseImageReference(t *testing.T) {
	tests := []struct {
		name           string
		imageRef       string
		expectedReg    string
		expectedRepo   string
		expectedTag    string
		expectError    bool
	}{
		{
			name:         "Docker Hub library image",
			imageRef:     "nginx:latest",
			expectedReg:  "index.docker.io",
			expectedRepo: "library/nginx",
			expectedTag:  "latest",
			expectError:  false,
		},
		{
			name:         "Docker Hub user image",
			imageRef:     "myuser/myapp:v1.0.0",
			expectedReg:  "index.docker.io",
			expectedRepo: "myuser/myapp",
			expectedTag:  "v1.0.0",
			expectError:  false,
		},
		{
			name:         "GCR image",
			imageRef:     "gcr.io/my-project/my-app:latest",
			expectedReg:  "gcr.io",
			expectedRepo: "my-project/my-app",
			expectedTag:  "latest",
			expectError:  false,
		},
		{
			name:         "Private registry with port",
			imageRef:     "my-registry.com:5000/myapp:v2.1",
			expectedReg:  "my-registry.com:5000",
			expectedRepo: "myapp",
			expectedTag:  "v2.1",
			expectError:  false,
		},
		{
			name:         "Image with digest",
			imageRef:     "nginx@sha256:1234567890abcdef",
			expectedReg:  "index.docker.io",
			expectedRepo: "library/nginx",
			expectedTag:  "latest@sha256:1234567890abcdef",
			expectError:  false,
		},
		{
			name:         "No tag specified",
			imageRef:     "alpine",
			expectedReg:  "index.docker.io",
			expectedRepo: "library/alpine",
			expectedTag:  "latest",
			expectError:  false,
		},
		{
			name:        "Empty reference",
			imageRef:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, repository, tag, err := (&DefaultFactsBuilder{}).ExtractImageReference(tt.imageRef)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.expectError {
				if registry != tt.expectedReg {
					t.Errorf("Expected registry %s, got %s", tt.expectedReg, registry)
				}
				if repository != tt.expectedRepo {
					t.Errorf("Expected repository %s, got %s", tt.expectedRepo, repository)
				}
				if tag != tt.expectedTag {
					t.Errorf("Expected tag %s, got %s", tt.expectedTag, tag)
				}
			}
		})
	}
}

func TestDefaultRegistryClient_SupportsRegistry(t *testing.T) {
	client := NewRegistryClient(30 * time.Second)

	tests := []struct {
		name     string
		registry string
		expected bool
	}{
		{"Docker Hub", "docker.io", true},
		{"Docker Hub index", "index.docker.io", true},
		{"GCR", "gcr.io", true},
		{"Quay", "quay.io", true},
		{"GitHub Container Registry", "ghcr.io", true},
		{"AWS ECR", "123456789012.dkr.ecr.us-west-2.amazonaws.com", true},
		{"Azure ACR", "myregistry.azurecr.io", true},
		{"Harbor", "harbor.company.com", true},
		{"Custom registry", "my-registry.com", true}, // Should be permissive
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.SupportsRegistry(tt.registry)
			if result != tt.expected {
				t.Errorf("Expected %v for registry %s, got %v", tt.expected, tt.registry, result)
			}
		})
	}
}

func TestDefaultRegistryClient_Authentication(t *testing.T) {
	tests := []struct {
		name        string
		registry    string
		credentials *RegistryCredentials
		expectError bool
	}{
		{
			name:     "valid username/password",
			registry: "my-registry.com",
			credentials: &RegistryCredentials{
				Username: "testuser",
				Password: "testpass",
			},
			expectError: false,
		},
		{
			name:     "valid token",
			registry: "my-registry.com",
			credentials: &RegistryCredentials{
				Token: "abc123token",
			},
			expectError: false,
		},
		{
			name:        "no credentials",
			registry:    "my-registry.com",
			credentials: nil,
			expectError: true,
		},
		{
			name:     "empty credentials",
			registry: "my-registry.com",
			credentials: &RegistryCredentials{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewRegistryClient(30 * time.Second)
			ctx := context.Background()

			err := client.Authenticate(ctx, tt.registry, tt.credentials)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// If successful, verify credentials are stored
			if !tt.expectError && err == nil {
				if creds, exists := client.credentials[tt.registry]; !exists {
					t.Errorf("Credentials not stored for registry")
				} else if creds != tt.credentials {
					t.Errorf("Stored credentials don't match input")
				}
			}
		})
	}
}

func TestDefaultRegistryClient_ManifestParsing(t *testing.T) {
	tests := []struct {
		name         string
		manifestJSON string
		expectError  bool
		expectedType string
	}{
		{
			name: "valid Docker v2 manifest",
			manifestJSON: `{
				"schemaVersion": 2,
				"mediaType": "application/vnd.docker.distribution.manifest.v2+json",
				"config": {
					"mediaType": "application/vnd.docker.container.image.v1+json",
					"size": 1234,
					"digest": "sha256:abc123"
				},
				"layers": [
					{
						"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
						"size": 5678,
						"digest": "sha256:def456"
					}
				]
			}`,
			expectError:  false,
			expectedType: "application/vnd.docker.distribution.manifest.v2+json",
		},
		{
			name: "valid OCI manifest",
			manifestJSON: `{
				"schemaVersion": 2,
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"config": {
					"mediaType": "application/vnd.oci.image.config.v1+json",
					"size": 1234,
					"digest": "sha256:abc123"
				},
				"layers": [
					{
						"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
						"size": 5678,
						"digest": "sha256:def456"
					}
				]
			}`,
			expectError:  false,
			expectedType: "application/vnd.oci.image.manifest.v1+json",
		},
		{
			name:         "invalid JSON",
			manifestJSON: `{"invalid": json}`,
			expectError:  true,
		},
		{
			name: "missing required fields",
			manifestJSON: `{
				"schemaVersion": 2
			}`,
			expectError: false, // JSON parsing succeeds, but manifest might be incomplete
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && strings.Contains(r.URL.Path, "/manifests/") {
					w.Header().Set("Content-Type", tt.expectedType)
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(tt.manifestJSON))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()
			
			// This test would require more complex mocking to fully test ParseManifest
			// For now, we test the JSON parsing logic directly
			var manifest ManifestInfo
			err := json.Unmarshal([]byte(tt.manifestJSON), &manifest)

			if tt.expectError && err == nil {
				t.Errorf("Expected JSON parsing error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected JSON parsing error: %v", err)
			}

			if !tt.expectError && err == nil {
				if manifest.MediaType != tt.expectedType {
					t.Errorf("Expected media type %s, got %s", tt.expectedType, manifest.MediaType)
				}
			}
		})
	}
}

func TestDefaultRegistryClient_SetCredentials(t *testing.T) {
	client := NewRegistryClient(30 * time.Second)

	registry := "test-registry.com"
	credentials := &RegistryCredentials{
		Username: "testuser",
		Password: "testpass",
	}

	// Set credentials
	client.SetCredentials(registry, credentials)

	// Verify credentials are stored
	storedCreds, exists := client.credentials[registry]
	if !exists {
		t.Errorf("Credentials not stored")
	}
	if storedCreds.Username != credentials.Username {
		t.Errorf("Username not stored correctly")
	}
	if storedCreds.Password != credentials.Password {
		t.Errorf("Password not stored correctly")
	}
}

func TestDefaultRegistryClient_UserAgent(t *testing.T) {
	client := NewRegistryClient(30 * time.Second)
	
	expectedUA := "troubleshoot.sh/image-collector/1.0"
	if client.userAgent != expectedUA {
		t.Errorf("Expected user agent %s, got %s", expectedUA, client.userAgent)
	}
}

func TestDefaultRegistryClient_HTTPClientConfiguration(t *testing.T) {
	timeout := 45 * time.Second
	client := NewRegistryClient(timeout)
	
	if client.httpClient.Timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, client.httpClient.Timeout)
	}
}

// Benchmark tests for performance validation
func BenchmarkRegistryClient_ParseImageReference(b *testing.B) {
	imageRef := "gcr.io/my-project/my-app:v1.0.0"
	factsBuilder := &DefaultFactsBuilder{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, err := factsBuilder.ExtractImageReference(imageRef)
		if err != nil {
			b.Fatalf("Parse failed: %v", err)
		}
	}
}

func TestNormalizeImageReference(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "simple image",
			input:    "nginx",
			expected: "index.docker.io/library/nginx:latest",
			wantErr:  false,
		},
		{
			name:     "image with tag",
			input:    "nginx:1.21",
			expected: "index.docker.io/library/nginx:1.21",
			wantErr:  false,
		},
		{
			name:     "full reference",
			input:    "gcr.io/my-project/my-app:v1.0.0",
			expected: "gcr.io/my-project/my-app:v1.0.0",
			wantErr:  false,
		},
		{
			name:     "image with digest",
			input:    "nginx@sha256:abc123def456",
			expected: "index.docker.io/library/nginx@sha256:abc123def456",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NormalizeImageReference(tt.input)

			if tt.wantErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tt.wantErr && result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
