package images

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFactsSerializer_SerializeToJSON(t *testing.T) {
	serializer := NewFactsSerializer(true) // Pretty print enabled

	facts := map[string]*ImageFacts{
		"nginx:latest": {
			Repository: "library/nginx",
			Tag:        "latest",
			Digest:     "sha256:abc123",
			Registry:   "index.docker.io",
			Size:       142857280,
			Created:    time.Date(2023, 1, 15, 10, 30, 0, 0, time.UTC),
			Labels:     map[string]string{"version": "1.21.0"},
			Platform:   Platform{Architecture: "amd64", OS: "linux"},
			Layers: []LayerInfo{
				{
					Digest:    "sha256:layer1",
					Size:      71428640,
					MediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
				},
			},
		},
	}

	data, err := serializer.SerializeToJSON(facts)
	if err != nil {
		t.Fatalf("Serialization failed: %v", err)
	}

	// Verify it's valid JSON
	var parsed ImageFactsOutput
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("Generated JSON is invalid: %v", err)
	}

	// Verify structure
	if parsed.Version != "v1" {
		t.Errorf("Expected version v1, got %s", parsed.Version)
	}

	if parsed.Facts == nil {
		t.Errorf("Facts should not be nil")
	}

	// Verify nginx facts
	nginxFacts, exists := parsed.Facts["nginx:latest"]
	if !exists {
		t.Errorf("nginx:latest facts not found")
	} else {
		if nginxFacts.Repository != "library/nginx" {
			t.Errorf("Wrong repository in serialized facts")
		}
		if nginxFacts.Digest != "sha256:abc123" {
			t.Errorf("Wrong digest in serialized facts")
		}
		if len(nginxFacts.Layers) != 1 {
			t.Errorf("Wrong number of layers in serialized facts")
		}
	}

	// Verify summary
	if parsed.Summary.TotalImages != 1 {
		t.Errorf("Expected 1 image in summary, got %d", parsed.Summary.TotalImages)
	}
}

func TestFactsSerializer_SerializeToFile(t *testing.T) {
	serializer := NewFactsSerializer(false) // No pretty print

	facts := map[string]*ImageFacts{
		"alpine:latest": {
			Repository: "library/alpine",
			Tag:        "latest",
			Registry:   "index.docker.io",
			Size:       5000000,
			Platform:   Platform{Architecture: "amd64", OS: "linux"},
		},
	}

	// Create temporary file
	tmpDir, err := os.MkdirTemp("", "facts-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test-facts.json")

	// Serialize to file
	err = serializer.SerializeToFile(facts, filePath)
	if err != nil {
		t.Fatalf("SerializeToFile failed: %v", err)
	}

	// Verify file exists and contains valid JSON
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read serialized file: %v", err)
	}

	var parsed ImageFactsOutput
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("File contains invalid JSON: %v", err)
	}

	// Verify content
	if len(parsed.Facts) != 1 {
		t.Errorf("Expected 1 fact, got %d", len(parsed.Facts))
	}
}

func TestFactsSerializer_SerializeToWriter(t *testing.T) {
	serializer := NewFactsSerializer(true)

	facts := map[string]*ImageFacts{
		"busybox:latest": {
			Repository: "library/busybox",
			Tag:        "latest",
			Registry:   "index.docker.io",
			Platform:   Platform{Architecture: "amd64", OS: "linux"},
		},
	}

	var buffer bytes.Buffer
	err := serializer.SerializeToWriter(facts, &buffer)
	if err != nil {
		t.Fatalf("SerializeToWriter failed: %v", err)
	}

	// Verify written data
	if buffer.Len() == 0 {
		t.Errorf("No data written to buffer")
	}

	// Verify it's valid JSON
	var parsed ImageFactsOutput
	if err := json.Unmarshal(buffer.Bytes(), &parsed); err != nil {
		t.Errorf("Buffer contains invalid JSON: %v", err)
	}
}

func TestFactsSerializer_DeserializeFromJSON(t *testing.T) {
	serializer := NewFactsSerializer(false)

	// Create test JSON data
	testData := `{
		"version": "v1",
		"timestamp": "2023-01-15T10:30:00Z",
		"facts": {
			"nginx:latest": {
				"repository": "library/nginx",
				"tag": "latest",
				"digest": "sha256:abc123",
				"registry": "index.docker.io",
				"size": 142857280,
				"created": "2023-01-15T10:30:00Z",
				"labels": {
					"version": "1.21.0"
				},
				"platform": {
					"architecture": "amd64",
					"os": "linux"
				}
			}
		},
		"summary": {
			"totalImages": 1,
			"registries": {"index.docker.io": 1},
			"platforms": {"linux/amd64": 1},
			"totalSize": 142857280
		}
	}`

	facts, err := serializer.DeserializeFromJSON([]byte(testData))
	if err != nil {
		t.Fatalf("Deserialization failed: %v", err)
	}

	// Verify deserialized content
	if len(facts) != 1 {
		t.Errorf("Expected 1 fact, got %d", len(facts))
	}

	nginxFacts, exists := facts["nginx:latest"]
	if !exists {
		t.Errorf("nginx:latest facts not found")
	} else {
		if nginxFacts.Repository != "library/nginx" {
			t.Errorf("Wrong repository in deserialized facts")
		}
		if nginxFacts.Digest != "sha256:abc123" {
			t.Errorf("Wrong digest in deserialized facts")
		}
		if nginxFacts.Size != 142857280 {
			t.Errorf("Wrong size in deserialized facts")
		}
	}
}

func TestFactsSerializer_DeserializeFromFile(t *testing.T) {
	serializer := NewFactsSerializer(false)

	// Create temporary file with test data
	tmpDir, err := os.MkdirTemp("", "facts-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testData := `{
		"version": "v1",
		"timestamp": "2023-01-15T10:30:00Z",
		"facts": {
			"alpine:latest": {
				"repository": "library/alpine",
				"tag": "latest",
				"registry": "index.docker.io",
				"platform": {
					"architecture": "amd64",
					"os": "linux"
				}
			}
		}
	}`

	filePath := filepath.Join(tmpDir, "test-facts.json")
	err = os.WriteFile(filePath, []byte(testData), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Deserialize from file
	facts, err := serializer.DeserializeFromFile(filePath)
	if err != nil {
		t.Fatalf("DeserializeFromFile failed: %v", err)
	}

	// Verify content
	if len(facts) != 1 {
		t.Errorf("Expected 1 fact, got %d", len(facts))
	}

	if alpineFacts, exists := facts["alpine:latest"]; !exists {
		t.Errorf("alpine:latest facts not found")
	} else {
		if alpineFacts.Repository != "library/alpine" {
			t.Errorf("Wrong repository in deserialized facts")
		}
	}
}

func TestFactsSerializer_ValidateFactsJSON(t *testing.T) {

	tests := []struct {
		name        string
		data        string
		expectError bool
		errorContains string
	}{
		{
			name: "valid facts JSON",
			data: `{
				"version": "v1",
				"timestamp": "2023-01-15T10:30:00Z",
				"facts": {
					"nginx:latest": {
						"repository": "library/nginx",
						"tag": "latest",
						"registry": "index.docker.io",
						"platform": {
							"architecture": "amd64",
							"os": "linux"
						}
					}
				},
				"summary": {
					"totalImages": 1,
					"registries": {"index.docker.io": 1},
					"platforms": {"linux/amd64": 1},
					"totalSize": 0
				}
			}`,
			expectError: false,
		},
		{
			name:          "invalid JSON",
			data:          `{"invalid": json}`,
			expectError:   true,
			errorContains: "invalid JSON",
		},
		{
			name: "missing version",
			data: `{
				"timestamp": "2023-01-15T10:30:00Z",
				"facts": {}
			}`,
			expectError:   true,
			errorContains: "missing version",
		},
		{
			name: "invalid version",
			data: `{
				"version": "v2",
				"timestamp": "2023-01-15T10:30:00Z",
				"facts": {}
			}`,
			expectError:   true,
			errorContains: "unsupported version",
		},
		{
			name: "missing required fields in facts",
			data: `{
				"version": "v1",
				"facts": {
					"nginx:latest": {
						"tag": "latest"
					}
				}
			}`,
			expectError:   true,
			errorContains: "repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFactsJSON([]byte(tt.data)) // Use standalone validation function

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
				// Debug: let's see what was actually parsed
				var parsed ImageFactsOutput
				parseErr := json.Unmarshal([]byte(tt.data), &parsed)
				t.Errorf("Debug: parseErr=%v, parsed.Version='%s'", parseErr, parsed.Version)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if tt.expectError && tt.errorContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing %s, got: %v", tt.errorContains, err)
				}
			}
		})
	}
}

func TestFactsSerializer_GenerateSummary(t *testing.T) {
	serializer := NewFactsSerializer(false)

	facts := map[string]*ImageFacts{
		"nginx:latest": {
			Registry: "index.docker.io",
			Size:     100000000,
			Platform: Platform{Architecture: "amd64", OS: "linux"},
		},
		"alpine:latest": {
			Registry: "index.docker.io",
			Size:     5000000,
			Platform: Platform{Architecture: "amd64", OS: "linux"},
		},
		"arm-app:v1": {
			Registry: "gcr.io",
			Size:     50000000,
			Platform: Platform{Architecture: "arm64", OS: "linux"},
		},
	}

	summary := serializer.generateSummary(facts)

	// Verify summary statistics
	if summary.TotalImages != 3 {
		t.Errorf("Expected 3 total images, got %d", summary.TotalImages)
	}

	if summary.TotalSize != 155000000 {
		t.Errorf("Expected total size 155000000, got %d", summary.TotalSize)
	}

	// Check registries count
	if summary.Registries["index.docker.io"] != 2 {
		t.Errorf("Expected 2 Docker Hub images, got %d", summary.Registries["index.docker.io"])
	}
	if summary.Registries["gcr.io"] != 1 {
		t.Errorf("Expected 1 GCR image, got %d", summary.Registries["gcr.io"])
	}

	// Check platforms count
	if summary.Platforms["linux/amd64"] != 2 {
		t.Errorf("Expected 2 linux/amd64 images, got %d", summary.Platforms["linux/amd64"])
	}
	if summary.Platforms["linux/arm64"] != 1 {
		t.Errorf("Expected 1 linux/arm64 image, got %d", summary.Platforms["linux/arm64"])
	}

	// Check largest image
	if summary.LargestImageSize != 100000000 {
		t.Errorf("Expected largest image size 100000000, got %d", summary.LargestImageSize)
	}
}
