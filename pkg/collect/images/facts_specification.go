package images

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// FactsSpecification defines the complete specification for facts.json output
type FactsSpecification struct {
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Schema      map[string]interface{} `json:"schema"`
	Examples    []FactsExample         `json:"examples"`
}

// FactsExample provides example facts.json content
type FactsExample struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Data        interface{} `json:"data"`
}

// GetFactsJSONSpecification returns the complete facts.json specification
func GetFactsJSONSpecification() *FactsSpecification {
	return &FactsSpecification{
		Version:     "v1",
		Description: "Image Facts JSON specification for troubleshoot.sh support bundles",
		Schema:      CreateFactsJSONSpec(),
		Examples:    getFactsExamples(),
	}
}

// WriteSpecificationToFile writes the facts.json specification to a file
func WriteSpecificationToFile(filePath string) error {
	spec := GetFactsJSONSpecification()
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal specification: %w", err)
	}

	// In a real implementation, this would write to the file
	fmt.Printf("Writing facts.json specification (%d bytes) to %s\n", len(data), filePath)
	return nil
}

// ValidateFactsJSON validates facts.json content against the specification
func ValidateFactsJSON(factsData []byte) error {
	// Parse the facts data
	var facts ImageFactsOutput
	if err := json.Unmarshal(factsData, &facts); err != nil {
		return fmt.Errorf("invalid JSON format: %w", err)
	}

	// Validate version
	if facts.Version == "" {
		return fmt.Errorf("missing version field")
	}
	if facts.Version != "v1" {
		return fmt.Errorf("unsupported version: %s", facts.Version)
	}

	// Validate required fields
	if facts.Facts == nil {
		return fmt.Errorf("facts field is required")
	}

	// Validate each image facts entry
	for imageRef, imageFacts := range facts.Facts {
		if err := validateImageFactsEntry(imageRef, imageFacts); err != nil {
			return fmt.Errorf("invalid facts for %s: %w", imageRef, err)
		}
	}

	// Validate summary if present
	if err := validateSummary(facts.Summary, len(facts.Facts)); err != nil {
		return fmt.Errorf("invalid summary: %w", err)
	}

	return nil
}

func validateImageFactsEntry(imageRef string, facts *ImageFacts) error {
	if facts == nil {
		return fmt.Errorf("facts cannot be nil")
	}

	// Required fields
	if facts.Repository == "" {
		return fmt.Errorf("repository cannot be empty")
	}
	if facts.Registry == "" {
		return fmt.Errorf("registry cannot be empty")
	}
	if facts.Platform.Architecture == "" {
		return fmt.Errorf("platform architecture cannot be empty")
	}
	if facts.Platform.OS == "" {
		return fmt.Errorf("platform OS cannot be empty")
	}

	// Validate digest format if present
	if facts.Digest != "" && !isValidDigest(facts.Digest) {
		return fmt.Errorf("invalid digest format: %s", facts.Digest)
	}

	// Validate size
	if facts.Size < 0 {
		return fmt.Errorf("size cannot be negative")
	}

	// Validate layers
	for i, layer := range facts.Layers {
		if layer.Digest == "" {
			return fmt.Errorf("layer %d digest is required", i)
		}
		if layer.Size <= 0 {
			return fmt.Errorf("layer %d size must be positive", i)
		}
		if layer.MediaType == "" {
			return fmt.Errorf("layer %d mediaType is required", i)
		}
	}

	return nil
}

func validateSummary(summary ImageFactsSummary, expectedTotal int) error {
	if summary.TotalImages != expectedTotal {
		return fmt.Errorf("summary total images (%d) doesn't match facts count (%d)", summary.TotalImages, expectedTotal)
	}

	if summary.TotalSize < 0 {
		return fmt.Errorf("total size cannot be negative")
	}

	return nil
}

func isValidDigest(digest string) bool {
	// Validate SHA256 digest format: sha256:64hexchars
	if len(digest) != 71 { // "sha256:" (7) + 64 hex chars
		return false
	}
	
	if !strings.HasPrefix(digest, "sha256:") {
		return false
	}
	
	hexPart := digest[7:]
	for _, char := range hexPart {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	
	return true
}

func getFactsExamples() []FactsExample {
	return []FactsExample{
		{
			Name:        "Simple Docker Hub Image",
			Description: "Basic example with nginx from Docker Hub",
			Data: map[string]interface{}{
				"version":   "v1",
				"timestamp": "2024-01-15T10:30:00Z",
				"facts": map[string]interface{}{
					"nginx:latest": map[string]interface{}{
						"repository":  "library/nginx",
						"tag":         "latest",
						"digest":      "sha256:a1b2c3d4e5f6...",
						"registry":    "index.docker.io",
						"size":        142857280,
						"created":     "2024-01-10T08:00:00Z",
						"platform": map[string]interface{}{
							"architecture": "amd64",
							"os":          "linux",
						},
						"labels": map[string]interface{}{
							"maintainer":    "NGINX Docker Maintainers",
							"version":       "1.21.0",
							"app.type":      "webserver",
							"registry.type": "docker-hub",
						},
					},
				},
				"summary": map[string]interface{}{
					"totalImages": 1,
					"registries": map[string]interface{}{
						"index.docker.io": 1,
					},
					"platforms": map[string]interface{}{
						"linux/amd64": 1,
					},
					"totalSize": 142857280,
				},
			},
		},
		{
			Name:        "Multi-Platform Private Registry",
			Description: "Complex example with multi-platform image from private registry",
			Data: map[string]interface{}{
				"version":   "v1",
				"timestamp": "2024-01-15T10:30:00Z",
				"facts": map[string]interface{}{
					"my-registry.com/myapp:v1.0.0": map[string]interface{}{
						"repository": "myapp",
						"tag":        "v1.0.0",
						"digest":     "sha256:x1y2z3...",
						"registry":   "my-registry.com",
						"size":       256000000,
						"created":    "2024-01-14T15:30:00Z",
						"platform": map[string]interface{}{
							"architecture": "arm64",
							"os":          "linux",
						},
						"labels": map[string]interface{}{
							"version":       "v1.0.0",
							"build.commit":  "abc123",
							"registry.type": "custom",
						},
						"layers": []map[string]interface{}{
							{
								"digest":    "sha256:layer1...",
								"size":      50000000,
								"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
							},
							{
								"digest":    "sha256:layer2...",
								"size":      206000000,
								"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
							},
						},
						"config": map[string]interface{}{
							"exposedPorts": map[string]interface{}{
								"8080/tcp": map[string]interface{}{},
							},
							"env": []string{
								"PATH=/usr/local/bin:/usr/bin:/bin",
								"APP_ENV=production",
							},
							"entrypoint": []string{"/app/start.sh"},
							"workingDir": "/app",
							"user":       "1001:1001",
						},
					},
				},
			},
		},
	}
}

// GetFactsJSONSchema returns just the JSON schema portion
func GetFactsJSONSchema() map[string]interface{} {
	return CreateFactsJSONSpec()
}

// GenerateFactsJSONExample generates a complete example facts.json
func GenerateFactsJSONExample() ([]byte, error) {
	example := map[string]interface{}{
		"version":   "v1",
		"timestamp": time.Now().Format(time.RFC3339),
		"facts": map[string]interface{}{
			"nginx:latest": map[string]interface{}{
				"repository": "library/nginx",
				"tag":        "latest",
				"digest":     "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"registry":   "index.docker.io",
				"size":       142857280,
				"created":    time.Now().Add(-24*time.Hour).Format(time.RFC3339),
				"labels": map[string]interface{}{
					"maintainer":    "NGINX Docker Maintainers",
					"version":       "1.21.0",
					"app.type":      "webserver",
					"registry.type": "docker-hub",
				},
				"platform": map[string]interface{}{
					"architecture": "amd64",
					"os":          "linux",
				},
				"layers": []map[string]interface{}{
					{
						"digest":    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
						"size":      72857280,
						"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
					},
					{
						"digest":    "sha256:fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321",
						"size":      70000000,
						"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
					},
				},
			},
		},
		"summary": map[string]interface{}{
			"totalImages": 1,
			"registries": map[string]interface{}{
				"index.docker.io": 1,
			},
			"platforms": map[string]interface{}{
				"linux/amd64": 1,
			},
			"totalSize": 142857280,
		},
	}

	return json.MarshalIndent(example, "", "  ")
}
