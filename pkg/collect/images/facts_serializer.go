package images

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// FactsSerializer handles serialization of image facts to various formats
type FactsSerializer struct {
	prettyPrint bool
	includeEmpty bool
}

// NewFactsSerializer creates a new facts serializer
func NewFactsSerializer(prettyPrint bool) *FactsSerializer {
	return &FactsSerializer{
		prettyPrint:  prettyPrint,
		includeEmpty: false,
	}
}

// SetIncludeEmpty configures whether to include empty fields in serialization
func (fs *FactsSerializer) SetIncludeEmpty(include bool) {
	fs.includeEmpty = include
}

// SerializeToJSON serializes image facts to JSON
func (fs *FactsSerializer) SerializeToJSON(facts map[string]*ImageFacts) ([]byte, error) {
	// Create the facts JSON structure
	factsOutput := &ImageFactsOutput{
		Version:   "v1",
		Timestamp: time.Now(),
		Facts:     facts,
		Summary:   fs.generateSummary(facts),
	}

	if fs.prettyPrint {
		return json.MarshalIndent(factsOutput, "", "  ")
	}
	return json.Marshal(factsOutput)
}

// SerializeToFile serializes image facts to a JSON file
func (fs *FactsSerializer) SerializeToFile(facts map[string]*ImageFacts, filePath string) error {
	data, err := fs.SerializeToJSON(facts)
	if err != nil {
		return fmt.Errorf("failed to serialize facts: %w", err)
	}

	return os.WriteFile(filePath, data, 0644)
}

// SerializeToWriter serializes image facts to an io.Writer
func (fs *FactsSerializer) SerializeToWriter(facts map[string]*ImageFacts, writer io.Writer) error {
	data, err := fs.SerializeToJSON(facts)
	if err != nil {
		return fmt.Errorf("failed to serialize facts: %w", err)
	}

	_, err = writer.Write(data)
	return err
}

// DeserializeFromJSON deserializes image facts from JSON
func (fs *FactsSerializer) DeserializeFromJSON(data []byte) (map[string]*ImageFacts, error) {
	var factsOutput ImageFactsOutput
	if err := json.Unmarshal(data, &factsOutput); err != nil {
		return nil, fmt.Errorf("failed to parse facts JSON: %w", err)
	}

	if factsOutput.Facts == nil {
		return make(map[string]*ImageFacts), nil
	}

	return factsOutput.Facts, nil
}

// DeserializeFromFile deserializes image facts from a JSON file
func (fs *FactsSerializer) DeserializeFromFile(filePath string) (map[string]*ImageFacts, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read facts file: %w", err)
	}

	return fs.DeserializeFromJSON(data)
}

// DeserializeFromReader deserializes image facts from an io.Reader
func (fs *FactsSerializer) DeserializeFromReader(reader io.Reader) (map[string]*ImageFacts, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read facts data: %w", err)
	}

	return fs.DeserializeFromJSON(data)
}

// ValidateFactsJSON validates that JSON data contains valid image facts
func (fs *FactsSerializer) ValidateFactsJSON(data []byte) error {
	var factsOutput ImageFactsOutput
	if err := json.Unmarshal(data, &factsOutput); err != nil {
		return fmt.Errorf("invalid JSON structure: %w", err)
	}

	// Validate version
	if factsOutput.Version == "" {
		return fmt.Errorf("missing version field")
	}

	// Validate facts structure
	if factsOutput.Facts != nil {
		for imageRef, facts := range factsOutput.Facts {
			if err := fs.validateImageFacts(imageRef, facts); err != nil {
				return fmt.Errorf("invalid facts for image %s: %w", imageRef, err)
			}
		}
	}

	return nil
}

func (fs *FactsSerializer) validateImageFacts(imageRef string, facts *ImageFacts) error {
	if facts == nil {
		return fmt.Errorf("facts cannot be nil")
	}

	if facts.Repository == "" {
		return fmt.Errorf("repository cannot be empty")
	}

	if facts.Registry == "" {
		return fmt.Errorf("registry cannot be empty")
	}

	if facts.Tag == "" && facts.Digest == "" {
		return fmt.Errorf("must have either tag or digest")
	}

	// Validate platform
	if facts.Platform.Architecture == "" {
		return fmt.Errorf("platform architecture cannot be empty")
	}

	if facts.Platform.OS == "" {
		return fmt.Errorf("platform OS cannot be empty")
	}

	// Validate layers
	for i, layer := range facts.Layers {
		if layer.Digest == "" {
			return fmt.Errorf("layer %d digest cannot be empty", i)
		}
		if layer.Size <= 0 {
			return fmt.Errorf("layer %d size must be positive", i)
		}
	}

	return nil
}

func (fs *FactsSerializer) generateSummary(facts map[string]*ImageFacts) ImageFactsSummary {
	summary := ImageFactsSummary{
		TotalImages: len(facts),
		Registries:  make(map[string]int),
		Platforms:   make(map[string]int),
		TotalSize:   0,
	}

	for _, imageFacts := range facts {
		// Count registries
		summary.Registries[imageFacts.Registry]++

		// Count platforms
		platformKey := fmt.Sprintf("%s/%s", imageFacts.Platform.OS, imageFacts.Platform.Architecture)
		if imageFacts.Platform.Variant != "" {
			platformKey += "/" + imageFacts.Platform.Variant
		}
		summary.Platforms[platformKey]++

		// Sum total size
		summary.TotalSize += imageFacts.Size

		// Track largest image
		if imageFacts.Size > summary.LargestImageSize {
			summary.LargestImageSize = imageFacts.Size
			summary.LargestImageRef = imageFacts.Repository + ":" + imageFacts.Tag
		}
	}

	return summary
}

// ImageFactsOutput represents the complete facts output structure
type ImageFactsOutput struct {
	Version   string                  `json:"version"`
	Timestamp time.Time               `json:"timestamp"`
	Facts     map[string]*ImageFacts  `json:"facts"`
	Summary   ImageFactsSummary       `json:"summary"`
}

// ImageFactsSummary provides summary statistics about collected image facts
type ImageFactsSummary struct {
	TotalImages      int            `json:"totalImages"`
	Registries       map[string]int `json:"registries"`       // registry -> count
	Platforms        map[string]int `json:"platforms"`        // platform -> count
	TotalSize        int64          `json:"totalSize"`        // bytes
	LargestImageSize int64          `json:"largestImageSize"` // bytes
	LargestImageRef  string         `json:"largestImageRef"`
}

// CreateFactsJSONSpec creates the JSON schema specification for facts.json
func CreateFactsJSONSpec() map[string]interface{} {
	return map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"title":   "Image Facts Specification",
		"type":    "object",
		"required": []string{"version", "timestamp", "facts"},
		"properties": map[string]interface{}{
			"version": map[string]interface{}{
				"type":        "string",
				"description": "Schema version",
				"const":       "v1",
			},
			"timestamp": map[string]interface{}{
				"type":        "string",
				"format":      "date-time",
				"description": "When the facts were collected",
			},
			"facts": map[string]interface{}{
				"type":        "object",
				"description": "Map of image reference to image facts",
				"patternProperties": map[string]interface{}{
					".*": map[string]interface{}{
						"$ref": "#/definitions/ImageFacts",
					},
				},
			},
			"summary": map[string]interface{}{
				"$ref": "#/definitions/ImageFactsSummary",
			},
		},
		"definitions": map[string]interface{}{
			"ImageFacts": map[string]interface{}{
				"type":     "object",
				"required": []string{"repository", "registry", "platform"},
				"properties": map[string]interface{}{
					"repository": map[string]interface{}{
						"type":        "string",
						"description": "Image repository name",
					},
					"tag": map[string]interface{}{
						"type":        "string",
						"description": "Image tag",
					},
					"digest": map[string]interface{}{
						"type":        "string",
						"description": "Image digest (sha256:...)",
						"pattern":     "^sha256:[a-f0-9]{64}$",
					},
					"registry": map[string]interface{}{
						"type":        "string",
						"description": "Registry hostname",
					},
					"size": map[string]interface{}{
						"type":        "integer",
						"description": "Image size in bytes",
						"minimum":     0,
					},
					"created": map[string]interface{}{
						"type":        "string",
						"format":      "date-time",
						"description": "Image creation timestamp",
					},
					"labels": map[string]interface{}{
						"type":        "object",
						"description": "Image labels and metadata",
						"patternProperties": map[string]interface{}{
							".*": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"platform": map[string]interface{}{
						"$ref": "#/definitions/Platform",
					},
					"layers": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{
							"$ref": "#/definitions/LayerInfo",
						},
					},
					"config": map[string]interface{}{
						"$ref": "#/definitions/ImageConfig",
					},
				},
			},
			"Platform": map[string]interface{}{
				"type":     "object",
				"required": []string{"architecture", "os"},
				"properties": map[string]interface{}{
					"architecture": map[string]interface{}{
						"type":        "string",
						"description": "CPU architecture (amd64, arm64, etc.)",
					},
					"os": map[string]interface{}{
						"type":        "string",
						"description": "Operating system (linux, windows, etc.)",
					},
					"variant": map[string]interface{}{
						"type":        "string",
						"description": "Architecture variant",
					},
				},
			},
			"LayerInfo": map[string]interface{}{
				"type":     "object",
				"required": []string{"digest", "size", "mediaType"},
				"properties": map[string]interface{}{
					"digest": map[string]interface{}{
						"type":        "string",
						"description": "Layer digest",
					},
					"size": map[string]interface{}{
						"type":        "integer",
						"description": "Layer size in bytes",
						"minimum":     0,
					},
					"mediaType": map[string]interface{}{
						"type":        "string",
						"description": "Layer media type",
					},
				},
			},
			"ImageConfig": map[string]interface{}{
				"type":        "object",
				"description": "Image configuration",
				"properties": map[string]interface{}{
					"exposedPorts": map[string]interface{}{
						"type": "object",
					},
					"env": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
					"entrypoint": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
					"cmd": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
					"workingDir": map[string]interface{}{
						"type": "string",
					},
					"user": map[string]interface{}{
						"type": "string",
					},
				},
			},
			"ImageFactsSummary": map[string]interface{}{
				"type":     "object",
				"required": []string{"totalImages"},
				"properties": map[string]interface{}{
					"totalImages": map[string]interface{}{
						"type":        "integer",
						"description": "Total number of images",
						"minimum":     0,
					},
					"registries": map[string]interface{}{
						"type":        "object",
						"description": "Registry usage counts",
					},
					"platforms": map[string]interface{}{
						"type":        "object",
						"description": "Platform usage counts",
					},
					"totalSize": map[string]interface{}{
						"type":        "integer",
						"description": "Total size of all images in bytes",
						"minimum":     0,
					},
				},
			},
		},
	}
}
