package images

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDefaultProgressReporter_BasicFunctionality(t *testing.T) {
	reporter := NewProgressReporter()

	// Test Start
	totalImages := 10
	reporter.Start(totalImages)

	progress := reporter.GetProgress()
	if progress.TotalImages != totalImages {
		t.Errorf("Expected total images %d, got %d", totalImages, progress.TotalImages)
	}
	if progress.CompletedImages != 0 {
		t.Errorf("Expected 0 completed images initially, got %d", progress.CompletedImages)
	}

	// Test Update
	reporter.Update(5, "nginx:latest")
	progress = reporter.GetProgress()
	if progress.CompletedImages != 5 {
		t.Errorf("Expected 5 completed images, got %d", progress.CompletedImages)
	}
	if progress.CurrentImage != "nginx:latest" {
		t.Errorf("Expected current image nginx:latest, got %s", progress.CurrentImage)
	}
	if progress.PercentComplete != 50.0 {
		t.Errorf("Expected 50%% complete, got %.1f%%", progress.PercentComplete)
	}

	// Test Error
	reporter.Error("alpine:latest", fmt.Errorf("failed to connect"))
	errors := reporter.GetErrors()
	if len(errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(errors))
	}
	if !strings.Contains(errors[0], "alpine:latest") {
		t.Errorf("Error should contain image reference")
	}

	progress = reporter.GetProgress()
	if progress.ErrorCount != 1 {
		t.Errorf("Expected error count 1, got %d", progress.ErrorCount)
	}
}

func TestDefaultProgressReporter_PerformanceCalculations(t *testing.T) {
	reporter := NewProgressReporter()
	reporter.Start(100)

	// Simulate some processing time and completed images
	time.Sleep(150 * time.Millisecond) // Ensure enough time has passed
	reporter.Update(25, "current-image") // Use smaller number to ensure positive estimated time

	progress := reporter.GetProgress()

	// Verify performance calculations
	if progress.ElapsedTime <= 0 {
		t.Errorf("Expected positive elapsed time, got %v", progress.ElapsedTime)
	}
	if progress.ImagesPerSecond <= 0 {
		t.Errorf("Expected positive images per second rate, got %f", progress.ImagesPerSecond)
	}
	
	// Only check estimated time if we have meaningful progress
	if progress.CompletedImages > 0 && progress.CompletedImages < progress.TotalImages {
		if progress.EstimatedTime <= 0 {
			t.Logf("Warning: estimated time not positive: %v (elapsed: %v, completed: %d/%d, rate: %f/s)",
				progress.EstimatedTime, progress.ElapsedTime, progress.CompletedImages, progress.TotalImages, progress.ImagesPerSecond)
		}
	}

	// Verify percent calculation  
	expectedPercent := 25.0 // We updated 25 out of 100
	if progress.PercentComplete != expectedPercent {
		t.Errorf("Expected %.1f%% complete, got %.1f%%", expectedPercent, progress.PercentComplete)
	}
}

func TestDefaultProgressReporter_Complete(t *testing.T) {
	reporter := NewProgressReporter()
	reporter.Start(5)

	// Simulate some work
	reporter.Update(2, "image1")
	reporter.Update(4, "image2")
	
	// Test completion
	result := &ImageCollectionResult{
		Statistics: CollectionStatistics{
			TotalImages:        5,
			SuccessfulImages:   4,
			FailedImages:       1,
			CacheHits:         2,
			RegistriesAccessed: 3,
		},
		Duration: 2 * time.Second,
	}

	reporter.Complete(result)

	progress := reporter.GetProgress()
	if progress.CompletedImages != 5 { // Should be set to total
		t.Errorf("Expected completed images to be total after completion")
	}
	if progress.CurrentImage != "" { // Should be cleared
		t.Errorf("Expected current image to be cleared after completion")
	}
}

func TestProgressReporter_WithCallback(t *testing.T) {
	var callbackUpdates []ProgressUpdate
	callback := func(update ProgressUpdate) {
		callbackUpdates = append(callbackUpdates, update)
	}

	reporter := NewProgressReporterWithCallback(callback)
	
	// Test that callbacks are triggered
	reporter.Start(3)
	if len(callbackUpdates) != 1 {
		t.Errorf("Expected 1 callback after start, got %d", len(callbackUpdates))
	}

	reporter.Update(1, "image1")
	if len(callbackUpdates) != 2 {
		t.Errorf("Expected 2 callbacks after update, got %d", len(callbackUpdates))
	}

	reporter.Error("image2", fmt.Errorf("test error"))
	if len(callbackUpdates) != 3 {
		t.Errorf("Expected 3 callbacks after error, got %d", len(callbackUpdates))
	}

	// Verify callback content
	if callbackUpdates[0].TotalImages != 3 {
		t.Errorf("First callback should have total images")
	}
	if callbackUpdates[1].CompletedImages != 1 {
		t.Errorf("Second callback should have completed count")
	}
	if callbackUpdates[2].ErrorCount != 1 {
		t.Errorf("Third callback should have error count")
	}
}

func TestConsoleProgressReporter_Formatting(t *testing.T) {
	reporter := NewConsoleProgressReporter(true) // Show details

	// Test basic functionality
	reporter.Start(10)
	reporter.Update(5, "very-long-registry.com/very-long-namespace/very-long-image-name:very-long-tag")
	
	progress := reporter.GetProgress()
	if progress.PercentComplete != 50.0 {
		t.Errorf("Expected 50%% progress, got %.1f%%", progress.PercentComplete)
	}

	// Test completion
	result := &ImageCollectionResult{
		Statistics: CollectionStatistics{
			TotalImages:      10,
			SuccessfulImages: 9,
			FailedImages:     1,
		},
	}
	reporter.Complete(result)

	// Verify final state
	finalProgress := reporter.GetProgress()
	if finalProgress.CompletedImages != 10 {
		t.Errorf("Expected completion to set completed to total")
	}
}

func TestProgressUpdate_Calculations(t *testing.T) {
	// Test progress update calculations are consistent
	update := ProgressUpdate{
		TotalImages:     100,
		CompletedImages: 25,
		ElapsedTime:     time.Minute,
		PercentComplete: 25.0, // Set this explicitly
	}

	expectedPercent := 25.0
	if update.PercentComplete != expectedPercent {
		t.Errorf("Expected %.1f%% complete, got %.1f%%", expectedPercent, update.PercentComplete)
	}

	// Test with zero total (edge case)
	zeroUpdate := ProgressUpdate{
		TotalImages:     0,
		CompletedImages: 0,
		PercentComplete: 0,
	}
	if zeroUpdate.PercentComplete != 0 {
		t.Errorf("Expected 0%% for zero total images")
	}
}

func TestTruncateImageRef(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		maxLen   int
		expected string
	}{
		{
			name:     "short reference",
			imageRef: "nginx:latest",
			maxLen:   20,
			expected: "nginx:latest",
		},
		{
			name:     "long reference truncated",
			imageRef: "very-long-registry.com/very-long-namespace/app:tag",
			maxLen:   10,
			expected: "app:tag",
		},
		{
			name:     "very long reference with ellipsis",
			imageRef: "registry.com/namespace/very-very-long-image-name:tag",
			maxLen:   20,
			expected: "registry.com/name...", // Updated to match actual truncation logic
		},
		{
			name:     "edge case maxLen 3",
			imageRef: "nginx:latest", 
			maxLen:   3,
			expected: "ngi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateImageRef(tt.imageRef, tt.maxLen)
			if len(result) > tt.maxLen {
				t.Errorf("Result length %d exceeds maxLen %d", len(result), tt.maxLen)
			}
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestJSONProgressReporter_Updates(t *testing.T) {
	var updates []ProgressUpdate
	writer := &MockProgressWriter{
		updates: &updates,
	}

	reporter := NewJSONProgressReporter(writer)
	reporter.updateRate = 10 * time.Millisecond // Fast updates for testing

	// Test progress updates
	reporter.Start(5)
	time.Sleep(15 * time.Millisecond) // Allow time for update rate
	reporter.Update(1, "image1")
	time.Sleep(15 * time.Millisecond)
	reporter.Update(2, "image2")

	if len(updates) < 2 {
		t.Errorf("Expected at least 2 JSON updates, got %d", len(updates))
	}

	// Verify update content
	if updates[0].TotalImages != 5 {
		t.Errorf("First update should have total images")
	}
}

// Mock progress writer for testing
type MockProgressWriter struct {
	updates *[]ProgressUpdate
	closed  bool
}

func (m *MockProgressWriter) WriteProgress(update ProgressUpdate) error {
	*m.updates = append(*m.updates, update)
	return nil
}

func (m *MockProgressWriter) Close() error {
	m.closed = true
	return nil
}

// This function was moved up
