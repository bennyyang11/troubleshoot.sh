package images

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// DefaultFactsBuilder implements the FactsBuilder interface
type DefaultFactsBuilder struct {
	registryClient   RegistryClient
	digestResolver   DigestResolver
	progressReporter ProgressReporter
}

// NewFactsBuilder creates a new facts builder
func NewFactsBuilder(registryClient RegistryClient, digestResolver DigestResolver) *DefaultFactsBuilder {
	return &DefaultFactsBuilder{
		registryClient: registryClient,
		digestResolver: digestResolver,
	}
}

// SetProgressReporter sets the progress reporter for the facts builder
func (fb *DefaultFactsBuilder) SetProgressReporter(reporter ProgressReporter) {
	fb.progressReporter = reporter
}

// BuildFacts creates comprehensive ImageFacts from registry data
func (fb *DefaultFactsBuilder) BuildFacts(ctx context.Context, imageRef string, manifest *ManifestInfo, config *ImageConfig) (*ImageFacts, error) {
	registry, repository, tag, err := fb.ExtractImageReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to extract image reference: %w", err)
	}

	facts := &ImageFacts{
		Repository: repository,
		Tag:        tag,
		Registry:   registry,
		Created:    time.Now(),
		Labels:     make(map[string]string),
		Platform:   Platform{Architecture: "amd64", OS: "linux"}, // Default
		Layers:     make([]LayerInfo, 0),
	}

	// Set digest from manifest or resolve it
	if manifest != nil && manifest.Config.Digest != "" {
		facts.Digest = manifest.Config.Digest
		facts.Size = manifest.Config.Size
	} else {
		digest, err := fb.digestResolver.ResolveTagToDigest(ctx, imageRef)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve digest: %w", err)
		}
		facts.Digest = digest
	}

	// Extract platform information
	if manifest != nil && manifest.Platform != nil {
		facts.Platform = *manifest.Platform
	}

	// Add layer information from manifest
	if manifest != nil {
		for _, layer := range manifest.Layers {
			layerInfo := LayerInfo{
				Digest:      layer.Digest,
				Size:        layer.Size,
				MediaType:   layer.MediaType,
				URLs:        layer.URLs,
				Annotations: layer.Annotations,
			}
			facts.Layers = append(facts.Layers, layerInfo)
			facts.Size += layer.Size // Add layer sizes to total
		}
	}

	// Extract configuration information
	if config != nil {
		facts.Config = *config
		
		// Extract creation time from environment or labels
		facts.Created = fb.extractCreationTime(config)
		
		// Extract labels from environment variables
		for _, env := range config.Env {
			if label, value, found := fb.parseEnvLabel(env); found {
				facts.Labels[label] = value
			}
		}
	}

	// Add derived metadata
	facts.Labels["image.digest"] = facts.Digest
	facts.Labels["image.registry"] = facts.Registry
	facts.Labels["image.size"] = fmt.Sprintf("%d", facts.Size)
	facts.Labels["image.layers"] = fmt.Sprintf("%d", len(facts.Layers))

	return facts, nil
}

// ExtractImageReference extracts components from an image reference string
func (fb *DefaultFactsBuilder) ExtractImageReference(imageRef string) (registry, repository, tag string, err error) {
	// Since parseImageReference is not exported, let's implement the parsing here
	if imageRef == "" {
		return "", "", "", fmt.Errorf("empty image reference")
	}

	// Handle digest format
	var digest string
	if strings.Contains(imageRef, "@sha256:") {
		parts := strings.Split(imageRef, "@")
		imageRef = parts[0]
		digest = parts[1]
	}

	// Split by colon for tag
	var repo string
	tag = "latest" // default
	if strings.Contains(imageRef, ":") && !strings.Contains(imageRef, "://") {
		lastColon := strings.LastIndex(imageRef, ":")
		repo = imageRef[:lastColon]
		tag = imageRef[lastColon+1:]
		
		// Check if what we think is a tag is actually part of a registry port
		if strings.Contains(tag, "/") {
			// This is a registry with port, no tag specified
			repo = imageRef
			tag = "latest"
		}
	} else {
		repo = imageRef
	}

	// Split registry and repository
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) == 1 {
		// No registry specified, assume Docker Hub
		registry = "index.docker.io"
		repository = "library/" + parts[0] // Docker Hub library namespace
	} else if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
		// First part looks like a registry (has . or :)
		registry = parts[0]
		repository = parts[1]
	} else {
		// Assume Docker Hub with user namespace
		registry = "index.docker.io"
		repository = repo
	}

	// If we had a digest, append it to the tag
	if digest != "" {
		tag = tag + "@" + digest
	}

	return registry, repository, tag, nil
}

// NormalizeImageReference normalizes an image reference to canonical form
func (fb *DefaultFactsBuilder) NormalizeImageReference(imageRef string) (string, error) {
	return NormalizeImageReference(imageRef)
}

// Helper methods for metadata extraction

func (fb *DefaultFactsBuilder) extractCreationTime(config *ImageConfig) time.Time {
	// Try to extract creation time from common environment variables
	for _, env := range config.Env {
		if strings.HasPrefix(env, "BUILD_DATE=") || strings.HasPrefix(env, "IMAGE_CREATED=") {
			dateStr := strings.SplitN(env, "=", 2)[1]
			if created, err := time.Parse(time.RFC3339, dateStr); err == nil {
				return created
			}
		}
	}
	
	// If no creation time found, use current time as fallback
	return time.Now()
}

func (fb *DefaultFactsBuilder) parseEnvLabel(env string) (string, string, bool) {
	// Parse environment variables that represent labels
	// Format: LABEL_key=value or key=value (for common label patterns)
	
	if !strings.Contains(env, "=") {
		return "", "", false
	}
	
	parts := strings.SplitN(env, "=", 2)
	key := parts[0]
	value := parts[1]
	
	// Handle LABEL_ prefix
	if strings.HasPrefix(key, "LABEL_") {
		labelKey := strings.TrimPrefix(key, "LABEL_")
		return fb.normalizeLabel(labelKey), value, true
	}
	
	// Handle common label-like environment variables
	commonLabels := map[string]string{
		"VERSION":     "version",
		"BUILD":       "build",
		"COMMIT":      "commit",
		"BRANCH":      "branch",
		"MAINTAINER":  "maintainer",
		"DESCRIPTION": "description",
		"VENDOR":      "vendor",
		"LICENSE":     "license",
	}
	
	if labelKey, exists := commonLabels[key]; exists {
		return labelKey, value, true
	}
	
	return "", "", false
}

func (fb *DefaultFactsBuilder) normalizeLabel(label string) string {
	// Convert label to lowercase and replace underscores with dots
	label = strings.ToLower(label)
	label = strings.ReplaceAll(label, "_", ".")
	return label
}

// BuildFactsFromPodImages extracts image references from pod specifications and builds facts
func (fb *DefaultFactsBuilder) BuildFactsFromPodImages(ctx context.Context, podSpec map[string]interface{}) (map[string]*ImageFacts, error) {
	imageRefs := fb.extractImageRefsFromPodSpec(podSpec)
	facts := make(map[string]*ImageFacts)
	
	for _, imageRef := range imageRefs {
		if fb.progressReporter != nil {
			fb.progressReporter.Update(len(facts), imageRef)
		}
		
		imageFacts, err := fb.registryClient.GetImageFacts(ctx, imageRef)
		if err != nil {
			if fb.progressReporter != nil {
				fb.progressReporter.Error(imageRef, err)
			}
			// Continue with other images even if one fails
			continue
		}
		
		facts[imageRef] = imageFacts
	}
	
	return facts, nil
}

func (fb *DefaultFactsBuilder) extractImageRefsFromPodSpec(podSpec map[string]interface{}) []string {
	var imageRefs []string
	
	// Extract from containers
	if containers, exists := podSpec["containers"]; exists {
		if containerList, ok := containers.([]interface{}); ok {
			for _, container := range containerList {
				if containerMap, ok := container.(map[string]interface{}); ok {
					if image, exists := containerMap["image"]; exists {
						if imageStr, ok := image.(string); ok && imageStr != "" {
							imageRefs = append(imageRefs, imageStr)
						}
					}
				}
			}
		}
	}
	
	// Extract from init containers
	if initContainers, exists := podSpec["initContainers"]; exists {
		if containerList, ok := initContainers.([]interface{}); ok {
			for _, container := range containerList {
				if containerMap, ok := container.(map[string]interface{}); ok {
					if image, exists := containerMap["image"]; exists {
						if imageStr, ok := image.(string); ok && imageStr != "" {
							imageRefs = append(imageRefs, imageStr)
						}
					}
				}
			}
		}
	}
	
	// Extract from ephemeral containers (if present)
	if ephemeralContainers, exists := podSpec["ephemeralContainers"]; exists {
		if containerList, ok := ephemeralContainers.([]interface{}); ok {
			for _, container := range containerList {
				if containerMap, ok := container.(map[string]interface{}); ok {
					if image, exists := containerMap["image"]; exists {
						if imageStr, ok := image.(string); ok && imageStr != "" {
							imageRefs = append(imageRefs, imageStr)
						}
					}
				}
			}
		}
	}
	
	return fb.deduplicateImageRefs(imageRefs)
}

func (fb *DefaultFactsBuilder) deduplicateImageRefs(imageRefs []string) []string {
	seen := make(map[string]bool)
	var unique []string
	
	for _, imageRef := range imageRefs {
		// Normalize the reference for deduplication
		normalized, err := fb.NormalizeImageReference(imageRef)
		if err != nil {
			// Use original reference if normalization fails
			normalized = imageRef
		}
		
		if !seen[normalized] {
			seen[normalized] = true
			unique = append(unique, imageRef) // Use original reference
		}
	}
	
	return unique
}

// ExtractVulnerabilityInfo extracts vulnerability information from image labels (if available)
func (fb *DefaultFactsBuilder) ExtractVulnerabilityInfo(facts *ImageFacts) {
	// Look for common vulnerability scanning labels
	vulnPatterns := map[string]string{
		"security.scan.date":      "last-scan-date",
		"security.scan.result":    "scan-result",
		"security.vulnerabilities": "vulnerability-count",
		"security.severity":       "max-severity",
	}
	
	for labelKey, vulnKey := range vulnPatterns {
		if value, exists := facts.Labels[labelKey]; exists {
			facts.Labels["vulnerability."+vulnKey] = value
		}
	}
}

// ExtractBuildInfo extracts build information from image metadata
func (fb *DefaultFactsBuilder) ExtractBuildInfo(facts *ImageFacts) {
	// Extract build information from various sources
	buildInfo := make(map[string]string)
	
	// From labels
	for key, value := range facts.Labels {
		if strings.Contains(key, "build") || strings.Contains(key, "commit") || 
		   strings.Contains(key, "version") || strings.Contains(key, "source") {
			buildInfo[key] = value
		}
	}
	
	// From environment variables
	for _, env := range facts.Config.Env {
		if strings.Contains(env, "BUILD") || strings.Contains(env, "COMMIT") || 
		   strings.Contains(env, "VERSION") || strings.Contains(env, "GIT") {
			buildInfo["env."+env] = env
		}
	}
	
	// Add build info to facts labels
	for key, value := range buildInfo {
		facts.Labels["build."+key] = value
	}
}

// ValidateImageReference validates that an image reference is well-formed
func (fb *DefaultFactsBuilder) ValidateImageReference(imageRef string) error {
	if imageRef == "" {
		return fmt.Errorf("image reference cannot be empty")
	}
	
	// Basic validation patterns
	// Registry pattern: domain.com[:port]
	// Repository pattern: namespace/repo or repo  
	// Tag pattern: alphanumeric with dots, dashes, underscores
	// Digest pattern: sha256:hexstring
	
	_, _, _, err := fb.ExtractImageReference(imageRef)
	if err != nil {
		return fmt.Errorf("invalid image reference format: %w", err)
	}
	
	// Additional validation for common issues
	if strings.Contains(imageRef, " ") {
		return fmt.Errorf("image reference cannot contain spaces")
	}
	
	// Check for dashes in repository name (which is valid)
	// Only reject if the entire reference starts/ends with dash
	if strings.HasPrefix(imageRef, "-") || strings.HasSuffix(imageRef, "-") {
		// But allow dashes in the middle of names (like "my-app:latest")
		if strings.Count(imageRef, "-") == 1 && (strings.HasPrefix(imageRef, "-") || strings.HasSuffix(imageRef, "-")) {
			return fmt.Errorf("image reference cannot start or end with dashes")
		}
	}
	
	// Validate tag format if present
	if strings.Contains(imageRef, ":") && !strings.Contains(imageRef, "@") {
		parts := strings.Split(imageRef, ":")
		tag := parts[len(parts)-1]
		if !fb.isValidTag(tag) {
			return fmt.Errorf("invalid tag format: %s", tag)
		}
	}
	
	return nil
}

func (fb *DefaultFactsBuilder) isValidTag(tag string) bool {
	// Tag validation based on Docker specs
	// Must be valid ASCII, max 128 chars, specific character set
	if len(tag) == 0 || len(tag) > 128 {
		return false
	}
	
	// Valid tag pattern: [a-zA-Z0-9._-]+
	tagPattern := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	return tagPattern.MatchString(tag)
}
