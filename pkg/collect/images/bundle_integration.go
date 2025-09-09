package images

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BundleImageCollector integrates image collection into support bundle generation
type BundleImageCollector struct {
	imageCollector   *AutoDiscoveryImageCollector
	factsSerializer  *FactsSerializer
	progressReporter ProgressReporter
	outputPath       string
}

// NewBundleImageCollector creates a new bundle image collector
func NewBundleImageCollector(outputPath string, imageCollector *AutoDiscoveryImageCollector) *BundleImageCollector {
	return &BundleImageCollector{
		imageCollector:  imageCollector,
		factsSerializer: NewFactsSerializer(true),
		outputPath:      outputPath,
	}
}

// SetProgressReporter sets the progress reporter for bundle operations
func (bic *BundleImageCollector) SetProgressReporter(reporter ProgressReporter) {
	bic.progressReporter = reporter
	bic.imageCollector.SetProgressReporter(reporter)
}

// CollectAndSerialize collects image facts and adds them to the support bundle
func (bic *BundleImageCollector) CollectAndSerialize(ctx context.Context, resources []AutoDiscoveryResource, options ImageCollectionOptions) (*BundleImageResult, error) {
	// Collect image facts from resources
	result, err := bic.imageCollector.CollectImageFactsFromResources(ctx, resources, options)
	if err != nil {
		return nil, fmt.Errorf("failed to collect image facts: %w", err)
	}

	// Generate facts.json
	factsPath := filepath.Join(bic.outputPath, "facts.json")
	if err := bic.factsSerializer.SerializeToFile(result.Facts, factsPath); err != nil {
		return nil, fmt.Errorf("failed to write facts.json: %w", err)
	}

	// Generate image-collection-stats.json with detailed statistics
	statsPath := filepath.Join(bic.outputPath, "image-collection-stats.json")
	if err := bic.writeCollectionStats(result, statsPath); err != nil {
		return nil, fmt.Errorf("failed to write collection stats: %w", err)
	}

	// Generate image-errors.json if there were errors
	if len(result.Errors) > 0 {
		errorsPath := filepath.Join(bic.outputPath, "image-errors.json")
		if err := bic.writeImageErrors(result.Errors, errorsPath); err != nil {
			return nil, fmt.Errorf("failed to write image errors: %w", err)
		}
	}

	bundleResult := &BundleImageResult{
		FactsPath:       factsPath,
		StatsPath:       statsPath,
		FactsCount:      len(result.Facts),
		ErrorsCount:     len(result.Errors),
		CollectionTime:  result.Duration,
		TotalSize:       bic.calculateTotalImageSize(result.Facts),
	}

	if len(result.Errors) > 0 {
		bundleResult.ErrorsPath = filepath.Join(bic.outputPath, "image-errors.json")
	}

	return bundleResult, nil
}

// WriteFactsToBundle writes image facts directly to a bundle writer
func (bic *BundleImageCollector) WriteFactsToBundle(facts map[string]*ImageFacts, bundleWriter BundleWriter) error {
	// Write facts.json
	factsData, err := bic.factsSerializer.SerializeToJSON(facts)
	if err != nil {
		return fmt.Errorf("failed to serialize facts: %w", err)
	}

	if err := bundleWriter.WriteFile("facts.json", factsData); err != nil {
		return fmt.Errorf("failed to write facts.json to bundle: %w", err)
	}

	// Write summary metadata
	summary := bic.generateImageSummary(facts)
	summaryData, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize image summary: %w", err)
	}

	if err := bundleWriter.WriteFile("image-summary.json", summaryData); err != nil {
		return fmt.Errorf("failed to write image summary to bundle: %w", err)
	}

	return nil
}

func (bic *BundleImageCollector) writeCollectionStats(result *ImageCollectionResult, filePath string) error {
	stats := map[string]interface{}{
		"collectionTime": result.Duration.String(),
		"timestamp":      result.Timestamp,
		"statistics":     result.Statistics,
		"summary": map[string]interface{}{
			"totalImages":       result.Statistics.TotalImages,
			"successfulImages":  result.Statistics.SuccessfulImages,
			"failedImages":      result.Statistics.FailedImages,
			"successRate":       float64(result.Statistics.SuccessfulImages) / float64(result.Statistics.TotalImages),
			"cacheHitRate":      float64(result.Statistics.CacheHits) / float64(result.Statistics.TotalImages),
			"registriesAccessed": result.Statistics.RegistriesAccessed,
		},
	}

	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal collection stats: %w", err)
	}

	return bic.writeFile(filePath, data)
}

func (bic *BundleImageCollector) writeImageErrors(errors map[string]error, filePath string) error {
	errorData := map[string]interface{}{
		"timestamp": time.Now(),
		"errors":    make([]map[string]interface{}, 0),
	}

	for imageRef, err := range errors {
		errorEntry := map[string]interface{}{
			"imageRef": imageRef,
			"error":    err.Error(),
			"registry": GetRegistryFromImageRef(imageRef),
		}
		
		errorData["errors"] = append(errorData["errors"].([]map[string]interface{}), errorEntry)
	}

	data, err := json.MarshalIndent(errorData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal image errors: %w", err)
	}

	return bic.writeFile(filePath, data)
}

func (bic *BundleImageCollector) calculateTotalImageSize(facts map[string]*ImageFacts) int64 {
	var totalSize int64
	for _, imageFacts := range facts {
		totalSize += imageFacts.Size
	}
	return totalSize
}

func (bic *BundleImageCollector) generateImageSummary(facts map[string]*ImageFacts) map[string]interface{} {
	summary := map[string]interface{}{
		"totalImages": len(facts),
		"registries":  make(map[string]int),
		"platforms":   make(map[string]int),
		"totalSize":   int64(0),
		"sizeByRegistry": make(map[string]int64),
	}

	for imageRef, imageFacts := range facts {
		// Count registries
		registries := summary["registries"].(map[string]int)
		registries[imageFacts.Registry]++

		// Count platforms
		platforms := summary["platforms"].(map[string]int)
		platformKey := fmt.Sprintf("%s/%s", imageFacts.Platform.OS, imageFacts.Platform.Architecture)
		platforms[platformKey]++

		// Track size
		totalSize := summary["totalSize"].(int64)
		totalSize += imageFacts.Size
		summary["totalSize"] = totalSize

		// Size by registry
		sizeByRegistry := summary["sizeByRegistry"].(map[string]int64)
		sizeByRegistry[imageFacts.Registry] += imageFacts.Size

		// Add image reference to summary for reference
		if _, exists := summary["imageRefs"]; !exists {
			summary["imageRefs"] = make([]string, 0)
		}
		summary["imageRefs"] = append(summary["imageRefs"].([]string), imageRef)
	}

	return summary
}

func (bic *BundleImageCollector) writeFile(filePath string, data []byte) error {
	// In a real implementation, this would use the support bundle writer
	// For testing, create the directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	
	// Write the file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	
	fmt.Printf("Writing %d bytes to %s in support bundle\n", len(data), filePath)
	return nil
}

// BundleWriter represents an interface for writing files to a support bundle
type BundleWriter interface {
	WriteFile(filename string, data []byte) error
	WriteFileWithPath(path string, data []byte) error
	Close() error
}

// BundleImageResult represents the result of adding image facts to a bundle
type BundleImageResult struct {
	FactsPath      string        `json:"factsPath"`
	StatsPath      string        `json:"statsPath"`
	ErrorsPath     string        `json:"errorsPath,omitempty"`
	FactsCount     int           `json:"factsCount"`
	ErrorsCount    int           `json:"errorsCount"`
	CollectionTime time.Duration `json:"collectionTime"`
	TotalSize      int64         `json:"totalSize"`
}

// ImageCollectionManifest represents metadata about image collection in the bundle
type ImageCollectionManifest struct {
	Version    string    `json:"version"`
	Timestamp  time.Time `json:"timestamp"`
	Options    ImageCollectionOptions `json:"options"`
	Results    BundleImageResult      `json:"results"`
	Artifacts  []string  `json:"artifacts"` // List of files created
}
