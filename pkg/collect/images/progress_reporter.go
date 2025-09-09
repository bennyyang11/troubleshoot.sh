package images

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// DefaultProgressReporter implements the ProgressReporter interface
type DefaultProgressReporter struct {
	totalImages     int
	completedImages int
	currentImage    string
	startTime       time.Time
	errors          []string
	mu              sync.RWMutex
	callback        ProgressCallback
}

// ProgressCallback defines a function called on progress updates
type ProgressCallback func(update ProgressUpdate)

// ProgressUpdate contains progress information
type ProgressUpdate struct {
	TotalImages     int           `json:"totalImages"`
	CompletedImages int           `json:"completedImages"`
	CurrentImage    string        `json:"currentImage"`
	PercentComplete float64       `json:"percentComplete"`
	ElapsedTime     time.Duration `json:"elapsedTime"`
	EstimatedTime   time.Duration `json:"estimatedTime"`
	ErrorCount      int           `json:"errorCount"`
	ImagesPerSecond float64       `json:"imagesPerSecond"`
}

// NewProgressReporter creates a new progress reporter
func NewProgressReporter() *DefaultProgressReporter {
	return &DefaultProgressReporter{
		errors: make([]string, 0),
	}
}

// NewProgressReporterWithCallback creates a progress reporter with callback
func NewProgressReporterWithCallback(callback ProgressCallback) *DefaultProgressReporter {
	return &DefaultProgressReporter{
		errors:   make([]string, 0),
		callback: callback,
	}
}

// Start initializes progress tracking
func (pr *DefaultProgressReporter) Start(totalImages int) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	
	pr.totalImages = totalImages
	pr.completedImages = 0
	pr.currentImage = ""
	pr.startTime = time.Now()
	pr.errors = make([]string, 0)
	
	pr.notifyProgress()
	fmt.Printf("Starting image collection for %d images...\n", totalImages)
}

// Update updates the progress with current image being processed
func (pr *DefaultProgressReporter) Update(completedImages int, currentImage string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	
	pr.completedImages = completedImages
	pr.currentImage = currentImage
	
	pr.notifyProgress()
	
	if completedImages%10 == 0 || completedImages == pr.totalImages {
		elapsed := time.Since(pr.startTime)
		percent := float64(completedImages) / float64(pr.totalImages) * 100
		fmt.Printf("Progress: %d/%d (%.1f%%) - Current: %s - Elapsed: %v\n", 
			completedImages, pr.totalImages, percent, currentImage, elapsed.Round(time.Second))
	}
}

// Error reports an error for a specific image
func (pr *DefaultProgressReporter) Error(imageRef string, err error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	
	errorMsg := fmt.Sprintf("%s: %v", imageRef, err)
	pr.errors = append(pr.errors, errorMsg)
	
	pr.notifyProgress()
	fmt.Printf("Error collecting %s: %v\n", imageRef, err)
}

// Complete finalizes progress reporting
func (pr *DefaultProgressReporter) Complete(result *ImageCollectionResult) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	
	pr.completedImages = pr.totalImages
	pr.currentImage = ""
	
	elapsed := time.Since(pr.startTime)
	
	fmt.Printf("\nImage collection complete!\n")
	fmt.Printf("  Total: %d images\n", result.Statistics.TotalImages)
	fmt.Printf("  Successful: %d images\n", result.Statistics.SuccessfulImages)
	fmt.Printf("  Failed: %d images\n", result.Statistics.FailedImages)
	fmt.Printf("  Duration: %v\n", elapsed.Round(time.Second))
	fmt.Printf("  Registries: %d\n", result.Statistics.RegistriesAccessed)
	
	if len(pr.errors) > 0 {
		fmt.Printf("  Errors: %d\n", len(pr.errors))
	}
	
	if result.Statistics.CacheHits > 0 {
		fmt.Printf("  Cache hits: %d\n", result.Statistics.CacheHits)
	}
	
	pr.notifyProgress()
}

// GetProgress returns current progress information
func (pr *DefaultProgressReporter) GetProgress() ProgressUpdate {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	
	elapsed := time.Since(pr.startTime)
	var percentComplete float64
	var estimatedTime time.Duration
	var imagesPerSecond float64
	
	if pr.totalImages > 0 {
		percentComplete = float64(pr.completedImages) / float64(pr.totalImages) * 100
		
		if pr.completedImages > 0 && elapsed > 0 {
			imagesPerSecond = float64(pr.completedImages) / elapsed.Seconds()
			
			if imagesPerSecond > 0 {
				remaining := pr.totalImages - pr.completedImages
				estimatedTime = time.Duration(float64(remaining)/imagesPerSecond) * time.Second
			}
		}
	}
	
	return ProgressUpdate{
		TotalImages:     pr.totalImages,
		CompletedImages: pr.completedImages,
		CurrentImage:    pr.currentImage,
		PercentComplete: percentComplete,
		ElapsedTime:     elapsed,
		EstimatedTime:   estimatedTime,
		ErrorCount:      len(pr.errors),
		ImagesPerSecond: imagesPerSecond,
	}
}

// GetErrors returns all collected errors
func (pr *DefaultProgressReporter) GetErrors() []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	
	// Return a copy to avoid race conditions
	errorsCopy := make([]string, len(pr.errors))
	copy(errorsCopy, pr.errors)
	return errorsCopy
}

// SetCallback sets the progress callback function
func (pr *DefaultProgressReporter) SetCallback(callback ProgressCallback) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.callback = callback
}

func (pr *DefaultProgressReporter) notifyProgress() {
	if pr.callback != nil {
		update := pr.getProgressUnsafe()
		pr.callback(update)
	}
}

func (pr *DefaultProgressReporter) getProgressUnsafe() ProgressUpdate {
	elapsed := time.Since(pr.startTime)
	var percentComplete float64
	var estimatedTime time.Duration
	var imagesPerSecond float64
	
	if pr.totalImages > 0 {
		percentComplete = float64(pr.completedImages) / float64(pr.totalImages) * 100
		
		if pr.completedImages > 0 && elapsed > 0 {
			imagesPerSecond = float64(pr.completedImages) / elapsed.Seconds()
			
			if imagesPerSecond > 0 {
				remaining := pr.totalImages - pr.completedImages
				estimatedTime = time.Duration(float64(remaining)/imagesPerSecond) * time.Second
			}
		}
	}
	
	return ProgressUpdate{
		TotalImages:     pr.totalImages,
		CompletedImages: pr.completedImages,
		CurrentImage:    pr.currentImage,
		PercentComplete: percentComplete,
		ElapsedTime:     elapsed,
		EstimatedTime:   estimatedTime,
		ErrorCount:      len(pr.errors),
		ImagesPerSecond: imagesPerSecond,
	}
}

// ConsoleProgressReporter provides console-based progress reporting
type ConsoleProgressReporter struct {
	*DefaultProgressReporter
	showDetails bool
	lastUpdate  time.Time
	updateRate  time.Duration
}

// NewConsoleProgressReporter creates a console progress reporter
func NewConsoleProgressReporter(showDetails bool) *ConsoleProgressReporter {
	return &ConsoleProgressReporter{
		DefaultProgressReporter: NewProgressReporter(),
		showDetails:             showDetails,
		updateRate:              time.Second, // Update every second
	}
}

// Update overrides the default update to provide console-specific formatting
func (cpr *ConsoleProgressReporter) Update(completedImages int, currentImage string) {
	cpr.DefaultProgressReporter.Update(completedImages, currentImage)
	
	// Throttle console updates
	if time.Since(cpr.lastUpdate) < cpr.updateRate && completedImages < cpr.totalImages {
		return
	}
	cpr.lastUpdate = time.Now()
	
	progress := cpr.GetProgress()
	
	if cpr.showDetails {
		fmt.Printf("\r[%3.0f%%] %d/%d images | %s | %.1f img/s | ETA: %v",
			progress.PercentComplete,
			progress.CompletedImages,
			progress.TotalImages,
			truncateImageRef(currentImage, 40),
			progress.ImagesPerSecond,
			progress.EstimatedTime.Round(time.Second))
	} else {
		fmt.Printf("\r[%3.0f%%] %d/%d images",
			progress.PercentComplete,
			progress.CompletedImages,
			progress.TotalImages)
	}
}

// Complete provides final console output
func (cpr *ConsoleProgressReporter) Complete(result *ImageCollectionResult) {
	// Clear the progress line
	fmt.Printf("\r%80s\r", "")
	
	// Call parent complete method
	cpr.DefaultProgressReporter.Complete(result)
}

func truncateImageRef(imageRef string, maxLen int) string {
	if len(imageRef) <= maxLen {
		return imageRef
	}
	
	// Try to keep the most relevant part (repo:tag)
	if parts := strings.Split(imageRef, "/"); len(parts) > 1 {
		relevant := parts[len(parts)-1] // Get the last part
		if len(relevant) <= maxLen {
			return relevant
		}
	}
	
	// If still too long, truncate with ellipsis
	if maxLen > 3 {
		return imageRef[:maxLen-3] + "..."
	}
	return imageRef[:maxLen]
}

// JSONProgressReporter writes progress updates to JSON format
type JSONProgressReporter struct {
	*DefaultProgressReporter
	writer     ProgressWriter
	updateRate time.Duration
	lastUpdate time.Time
}

// ProgressWriter defines interface for writing progress updates
type ProgressWriter interface {
	WriteProgress(update ProgressUpdate) error
	Close() error
}

// NewJSONProgressReporter creates a JSON progress reporter
func NewJSONProgressReporter(writer ProgressWriter) *JSONProgressReporter {
	return &JSONProgressReporter{
		DefaultProgressReporter: NewProgressReporter(),
		writer:                  writer,
		updateRate:              5 * time.Second, // Less frequent updates for JSON
	}
}

// Update writes progress updates in JSON format
func (jpr *JSONProgressReporter) Update(completedImages int, currentImage string) {
	jpr.DefaultProgressReporter.Update(completedImages, currentImage)
	
	// Throttle JSON updates
	if time.Since(jpr.lastUpdate) < jpr.updateRate && completedImages < jpr.totalImages {
		return
	}
	jpr.lastUpdate = time.Now()
	
	progress := jpr.GetProgress()
	if err := jpr.writer.WriteProgress(progress); err != nil {
		fmt.Printf("Warning: failed to write progress update: %v\n", err)
	}
}

// Complete writes final progress state
func (jpr *JSONProgressReporter) Complete(result *ImageCollectionResult) {
	jpr.DefaultProgressReporter.Complete(result)
	
	finalProgress := jpr.GetProgress()
	if err := jpr.writer.WriteProgress(finalProgress); err != nil {
		fmt.Printf("Warning: failed to write final progress: %v\n", err)
	}
}
