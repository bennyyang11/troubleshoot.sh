package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/replicatedhq/troubleshoot/pkg/collect/images"
)

func TestImageCollectionHandler_ParseImageOptions(t *testing.T) {
	tests := []struct {
		name          string
		includeImages bool
		imageOpts     string
		expectError   bool
		validate      func(*ImageCollectionHandler) error
	}{
		{
			name:          "images disabled",
			includeImages: false,
			imageOpts:     "",
			expectError:   false,
			validate: func(handler *ImageCollectionHandler) error {
				if handler.IsEnabled() {
					return fmt.Errorf("handler should be disabled")
				}
				return nil
			},
		},
		{
			name:          "images enabled with defaults",
			includeImages: true,
			imageOpts:     "",
			expectError:   false,
			validate: func(handler *ImageCollectionHandler) error {
				if !handler.IsEnabled() {
					return fmt.Errorf("handler should be enabled")
				}
				return nil
			},
		},
		{
			name:          "custom options",
			includeImages: true,
			imageOpts:     "manifests=false,cache=true,timeout=90s",
			expectError:   false,
			validate: func(handler *ImageCollectionHandler) error {
				opts := handler.GetImageCollectionOptions()
				if opts.IncludeManifests {
					return fmt.Errorf("manifests should be disabled")
				}
				if !opts.CacheEnabled {
					return fmt.Errorf("cache should be enabled")
				}
				if opts.Timeout != 90*time.Second {
					return fmt.Errorf("timeout should be 90s, got %v", opts.Timeout)
				}
				return nil
			},
		},
		{
			name:          "invalid timeout",
			includeImages: true,
			imageOpts:     "timeout=invalid",
			expectError:   true,
		},
		{
			name:          "invalid concurrency",
			includeImages: true,
			imageOpts:     "concurrency=100", // Too high
			expectError:   true,
		},
		{
			name:          "invalid retry count",
			includeImages: true,
			imageOpts:     "retries=20", // Too high
			expectError:   true,
		},
		{
			name:          "unknown option",
			includeImages: true,
			imageOpts:     "unknown=value",
			expectError:   true,
		},
		{
			name:          "malformed option",
			includeImages: true,
			imageOpts:     "invalid-format",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewImageCollectionHandler()
			err := handler.ParseImageOptions(tt.includeImages, tt.imageOpts)

			if tt.expectError && err == nil {
				t.Errorf("Expected error parsing image options but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error parsing image options: %v", err)
			}

			if !tt.expectError && tt.validate != nil {
				if validateErr := tt.validate(handler); validateErr != nil {
					t.Errorf("Validation failed: %v", validateErr)
				}
			}
		})
	}
}

func TestImageCollectionHandler_ValidateImageOptions(t *testing.T) {
	tests := []struct {
		name        string
		setupHandler func(*ImageCollectionHandler)
		expectError bool
	}{
		{
			name: "valid options",
			setupHandler: func(handler *ImageCollectionHandler) {
				handler.SetEnabled(true)
				// Use default options which should be valid
			},
			expectError: false,
		},
		{
			name: "disabled handler",
			setupHandler: func(handler *ImageCollectionHandler) {
				handler.SetEnabled(false)
			},
			expectError: false, // Should not validate when disabled
		},
		{
			name: "invalid timeout",
			setupHandler: func(handler *ImageCollectionHandler) {
				handler.SetEnabled(true)
				handler.options.Timeout = -1 * time.Second
			},
			expectError: true,
		},
		{
			name: "excessive timeout",
			setupHandler: func(handler *ImageCollectionHandler) {
				handler.SetEnabled(true)
				handler.options.Timeout = 15 * time.Minute // > 10 minutes
			},
			expectError: true,
		},
		{
			name: "invalid concurrency",
			setupHandler: func(handler *ImageCollectionHandler) {
				handler.SetEnabled(true)
				handler.options.MaxConcurrency = 0
			},
			expectError: true,
		},
		{
			name: "invalid retry count",
			setupHandler: func(handler *ImageCollectionHandler) {
				handler.SetEnabled(true)
				handler.options.RetryCount = -1
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewImageCollectionHandler()
			tt.setupHandler(handler)

			err := handler.ValidateImageOptions()

			if tt.expectError && err == nil {
				t.Errorf("Expected validation error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected validation error: %v", err)
			}
		})
	}
}

func TestImageCollectionHandler_SetRegistryCredentials(t *testing.T) {
	handler := NewImageCollectionHandler()

	// Test setting credentials
	creds1 := &images.RegistryCredentials{
		Username: "testuser",
		Password: "testpass",
	}
	handler.SetRegistryCredentials("docker.io", creds1)

	creds2 := &images.RegistryCredentials{
		Token: "gcr-token",
	}
	handler.SetRegistryCredentials("gcr.io", creds2)

	// Verify credentials are stored
	options := handler.GetImageCollectionOptions()
	if len(options.Credentials) != 2 {
		t.Errorf("Expected 2 registry credentials, got %d", len(options.Credentials))
	}

	dockerCreds, exists := options.Credentials["docker.io"]
	if !exists {
		t.Errorf("Docker.io credentials not found")
	} else {
		if dockerCreds.Username != "testuser" {
			t.Errorf("Docker.io username not set correctly")
		}
	}

	gcrCreds, exists := options.Credentials["gcr.io"]
	if !exists {
		t.Errorf("GCR credentials not found")
	} else {
		if gcrCreds.Token != "gcr-token" {
			t.Errorf("GCR token not set correctly")
		}
	}
}

func TestImageCollectionHandler_GenerateImageCollectionSummary(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*ImageCollectionHandler)
		expected []string // Strings that should be in the summary
	}{
		{
			name: "disabled handler",
			setup: func(handler *ImageCollectionHandler) {
				handler.SetEnabled(false)
			},
			expected: []string{"disabled"},
		},
		{
			name: "enabled with default options",
			setup: func(handler *ImageCollectionHandler) {
				handler.SetEnabled(true)
			},
			expected: []string{"enabled", "Include manifests: true", "Cache enabled: true"},
		},
		{
			name: "enabled with custom options",
			setup: func(handler *ImageCollectionHandler) {
				handler.SetEnabled(true)
				handler.ParseImageOptions(true, "manifests=false,timeout=120s,concurrency=10")
			},
			expected: []string{"enabled", "Include manifests: false", "Timeout: 2m0s", "Max concurrency: 10"},
		},
		{
			name: "enabled with registry credentials",
			setup: func(handler *ImageCollectionHandler) {
				handler.SetEnabled(true)
				handler.SetRegistryCredentials("docker.io", &images.RegistryCredentials{Username: "user"})
				handler.SetRegistryCredentials("gcr.io", &images.RegistryCredentials{Token: "token"})
			},
			expected: []string{"enabled", "Authenticated registries"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewImageCollectionHandler()
			tt.setup(handler)

			summary := handler.GenerateImageCollectionSummary()

			for _, expected := range tt.expected {
				if !strings.Contains(summary, expected) {
					t.Errorf("Summary should contain '%s': %s", expected, summary)
				}
			}
		})
	}
}

func TestParseBool(t *testing.T) {
	tests := []struct {
		value        string
		defaultValue bool
		expected     bool
	}{
		{"true", false, true},
		{"1", false, true},
		{"yes", false, true},
		{"on", false, true},
		{"false", true, false},
		{"0", true, false},
		{"no", true, false},
		{"off", true, false},
		{"invalid", true, true},   // Should use default
		{"invalid", false, false}, // Should use default
		{"", true, true},          // Should use default
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			result := parseBool(tt.value, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("parseBool('%s', %v) = %v, expected %v", tt.value, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

// Integration test for image collection handler
func TestImageCollectionHandler_Integration(t *testing.T) {
	handler := NewImageCollectionHandler()

	// Test complete workflow
	err := handler.ParseImageOptions(true, "cache=true,timeout=60s,concurrency=5")
	if err != nil {
		t.Fatalf("Failed to parse image options: %v", err)
	}

	// Set some credentials
	handler.SetRegistryCredentials("private-registry.com", &images.RegistryCredentials{
		Username: "testuser",
		Password: "testpass",
	})

	// Validate options
	err = handler.ValidateImageOptions()
	if err != nil {
		t.Errorf("Options should be valid: %v", err)
	}

	// Get final options
	options := handler.GetImageCollectionOptions()
	if !options.CacheEnabled {
		t.Errorf("Cache should be enabled")
	}
	if options.Timeout != 60*time.Second {
		t.Errorf("Timeout should be 60s")
	}
	if options.MaxConcurrency != 5 {
		t.Errorf("Concurrency should be 5")
	}

	// Check credentials
	if len(options.Credentials) != 1 {
		t.Errorf("Should have 1 credential")
	}

	// Generate summary
	summary := handler.GenerateImageCollectionSummary()
	if !strings.Contains(summary, "enabled") {
		t.Errorf("Summary should show enabled state")
	}
}
