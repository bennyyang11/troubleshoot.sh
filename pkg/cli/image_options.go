package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/replicatedhq/troubleshoot/pkg/collect/images"
)

// ImageCollectionHandler manages image collection CLI options and execution
type ImageCollectionHandler struct {
	enabled             bool
	registryCredentials map[string]*images.RegistryCredentials
	options             images.ImageCollectionOptions
	progressReporter    images.ProgressReporter
}

// NewImageCollectionHandler creates a new image collection handler
func NewImageCollectionHandler() *ImageCollectionHandler {
	return &ImageCollectionHandler{
		enabled:             false,
		registryCredentials: make(map[string]*images.RegistryCredentials),
		options: images.ImageCollectionOptions{
			IncludeManifests: true,
			IncludeLayers:    true,
			IncludeConfig:    true,
			CacheEnabled:     true,
			Timeout:          60 * time.Second,
			MaxConcurrency:   5,
			RetryCount:       3,
		},
	}
}

// SetEnabled enables or disables image collection
func (ich *ImageCollectionHandler) SetEnabled(enabled bool) {
	ich.enabled = enabled
}

// IsEnabled returns whether image collection is enabled
func (ich *ImageCollectionHandler) IsEnabled() bool {
	return ich.enabled
}

// ParseImageOptions parses image-related CLI options
func (ich *ImageCollectionHandler) ParseImageOptions(includeImages bool, imageOpts string) error {
	ich.enabled = includeImages

	if !includeImages {
		return nil
	}

	// Parse additional image options if provided
	// Format: "manifests=true,layers=false,cache=true,timeout=60s"
	if imageOpts != "" {
		return ich.parseImageOptionsString(imageOpts)
	}

	return nil
}

func (ich *ImageCollectionHandler) parseImageOptionsString(imageOpts string) error {
	opts := strings.Split(imageOpts, ",")
	for _, opt := range opts {
		opt = strings.TrimSpace(opt)
		if opt == "" {
			continue
		}

		if !strings.Contains(opt, "=") {
			return fmt.Errorf("image option must be in format key=value: %s", opt)
		}

		parts := strings.SplitN(opt, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "manifests":
			ich.options.IncludeManifests = parseBool(value, true)
		case "layers":
			ich.options.IncludeLayers = parseBool(value, true)
		case "config":
			ich.options.IncludeConfig = parseBool(value, true)
		case "cache":
			ich.options.CacheEnabled = parseBool(value, true)
		case "timeout":
			timeout, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("invalid timeout duration: %w", err)
			}
			ich.options.Timeout = timeout
		case "concurrency":
			var concurrency int
			if _, err := fmt.Sscanf(value, "%d", &concurrency); err != nil {
				return fmt.Errorf("invalid concurrency value: %w", err)
			}
			if concurrency < 1 || concurrency > 20 {
				return fmt.Errorf("concurrency must be between 1 and 20")
			}
			ich.options.MaxConcurrency = concurrency
		case "retries":
			var retries int
			if _, err := fmt.Sscanf(value, "%d", &retries); err != nil {
				return fmt.Errorf("invalid retry count: %w", err)
			}
			if retries < 0 || retries > 10 {
				return fmt.Errorf("retry count must be between 0 and 10")
			}
			ich.options.RetryCount = retries
		default:
			return fmt.Errorf("unknown image option: %s", key)
		}
	}

	return nil
}

// SetRegistryCredentials configures registry authentication
func (ich *ImageCollectionHandler) SetRegistryCredentials(registry string, creds *images.RegistryCredentials) {
	ich.registryCredentials[registry] = creds
}

// LoadRegistryCredentialsFromConfig loads registry credentials from configuration
func (ich *ImageCollectionHandler) LoadRegistryCredentialsFromConfig(configFile string) error {
	// In a full implementation, this would load registry credentials from:
	// 1. Docker config (~/.docker/config.json)
	// 2. Kubernetes image pull secrets
	// 3. Environment variables
	// 4. CLI configuration file

	fmt.Printf("Info: registry credentials would be loaded from %s\n", configFile)
	return nil
}

// GetImageCollectionOptions returns the current image collection options
func (ich *ImageCollectionHandler) GetImageCollectionOptions() images.ImageCollectionOptions {
	// Merge registry credentials into options
	ich.options.Credentials = ich.registryCredentials
	return ich.options
}

// SetProgressReporter sets the progress reporter for image operations
func (ich *ImageCollectionHandler) SetProgressReporter(reporter images.ProgressReporter) {
	ich.progressReporter = reporter
}

// GetProgressReporter returns the current progress reporter
func (ich *ImageCollectionHandler) GetProgressReporter() images.ProgressReporter {
	return ich.progressReporter
}

// ValidateImageOptions validates image collection options
func (ich *ImageCollectionHandler) ValidateImageOptions() error {
	if !ich.enabled {
		return nil
	}

	// Validate timeout
	if ich.options.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if ich.options.Timeout > 10*time.Minute {
		return fmt.Errorf("timeout cannot exceed 10 minutes")
	}

	// Validate concurrency
	if ich.options.MaxConcurrency <= 0 {
		return fmt.Errorf("max concurrency must be positive")
	}

	// Validate retry count
	if ich.options.RetryCount < 0 {
		return fmt.Errorf("retry count cannot be negative")
	}

	return nil
}

// GenerateImageCollectionSummary creates a summary for CLI output
func (ich *ImageCollectionHandler) GenerateImageCollectionSummary() string {
	if !ich.enabled {
		return "Image collection: disabled"
	}

	summary := []string{
		fmt.Sprintf("Image collection: enabled"),
		fmt.Sprintf("  Include manifests: %v", ich.options.IncludeManifests),
		fmt.Sprintf("  Include layers: %v", ich.options.IncludeLayers),
		fmt.Sprintf("  Include config: %v", ich.options.IncludeConfig),
		fmt.Sprintf("  Cache enabled: %v", ich.options.CacheEnabled),
		fmt.Sprintf("  Timeout: %v", ich.options.Timeout),
		fmt.Sprintf("  Max concurrency: %d", ich.options.MaxConcurrency),
		fmt.Sprintf("  Retry count: %d", ich.options.RetryCount),
	}

	if len(ich.registryCredentials) > 0 {
		registries := make([]string, 0, len(ich.registryCredentials))
		for registry := range ich.registryCredentials {
			registries = append(registries, registry)
		}
		summary = append(summary, fmt.Sprintf("  Authenticated registries: %s", strings.Join(registries, ", ")))
	}

	return strings.Join(summary, "\n")
}

// Helper functions

func parseBool(value string, defaultValue bool) bool {
	value = strings.ToLower(value)
	switch value {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return defaultValue
	}
}
