package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
	"github.com/replicatedhq/troubleshoot/pkg/collect/images"
)

// DryRunExecutor handles dry-run mode execution
type DryRunExecutor struct {
	discoverer       *autodiscovery.Discoverer
	imageCollector   *images.AutoDiscoveryImageCollector
	rbacValidator    *RBACValidator
	outputFormat     string // "console", "json", "yaml"
	verboseMode      bool
}

// DryRunResult represents the result of a dry-run execution
type DryRunResult struct {
	Timestamp        time.Time                       `json:"timestamp"`
	Options          autodiscovery.DiscoveryOptions  `json:"options"`
	Summary          DryRunSummary                   `json:"summary"`
	Collectors       []autodiscovery.CollectorSpec   `json:"collectors"`
	RBACReport       *RBACValidationReport           `json:"rbacReport,omitempty"`
	ImageAnalysis    *DryRunImageAnalysis            `json:"imageAnalysis,omitempty"`
	Warnings         []string                        `json:"warnings,omitempty"`
	Recommendations  []string                        `json:"recommendations"`
	EstimatedSize    string                          `json:"estimatedSize"`
	EstimatedDuration time.Duration                  `json:"estimatedDuration"`
}

// DryRunSummary provides high-level summary of what would be collected
type DryRunSummary struct {
	TotalCollectors       int            `json:"totalCollectors"`
	CollectorsByType      map[string]int `json:"collectorsByType"`
	CollectorsByNamespace map[string]int `json:"collectorsByNamespace"`
	CollectorsByPriority  map[string]int `json:"collectorsByPriority"`
	NamespacesIncluded    []string       `json:"namespacesIncluded"`
	ResourceTypesIncluded []string       `json:"resourceTypesIncluded"`
}

// DryRunImageAnalysis provides analysis of image collection that would occur
type DryRunImageAnalysis struct {
	Enabled           bool     `json:"enabled"`
	ExpectedImages    int      `json:"expectedImages"`
	UniqueRegistries  []string `json:"uniqueRegistries"`
	EstimatedSize     string   `json:"estimatedSize"`
	AuthRequirements  []string `json:"authRequirements"`
}

// NewDryRunExecutor creates a new dry-run executor
func NewDryRunExecutor(discoverer *autodiscovery.Discoverer, imageCollector *images.AutoDiscoveryImageCollector) *DryRunExecutor {
	return &DryRunExecutor{
		discoverer:     discoverer,
		imageCollector: imageCollector,
		outputFormat:   "console",
		verboseMode:    false,
	}
}

// SetOutputFormat sets the output format for dry-run results
func (dre *DryRunExecutor) SetOutputFormat(format string) error {
	validFormats := []string{"console", "json", "yaml"}
	for _, valid := range validFormats {
		if format == valid {
			dre.outputFormat = format
			return nil
		}
	}
	return fmt.Errorf("invalid output format: %s (valid: %v)", format, validFormats)
}

// SetVerboseMode enables or disables verbose output
func (dre *DryRunExecutor) SetVerboseMode(verbose bool) {
	dre.verboseMode = verbose
}

// Execute performs the dry-run analysis
func (dre *DryRunExecutor) Execute(ctx context.Context, options autodiscovery.DiscoveryOptions, rbacMode RBACValidationMode) (*DryRunResult, error) {
	fmt.Printf("🔍 Executing auto-discovery dry run...\n")
	
	result := &DryRunResult{
		Timestamp:       time.Now(),
		Options:         options,
		Warnings:        make([]string, 0),
		Recommendations: make([]string, 0),
	}

	// Perform RBAC validation if requested
	if rbacMode != RBACValidationOff && dre.rbacValidator != nil {
		fmt.Printf("🔐 Validating RBAC permissions...\n")
		rbacReport, err := dre.rbacValidator.ValidateRBACAccess(ctx, options.Namespaces)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("RBAC validation failed: %v", err))
		} else {
			result.RBACReport = rbacReport
			
			// Add warnings for low access
			if rbacReport.Summary.AccessRate < 0.5 {
				result.Warnings = append(result.Warnings, "Limited RBAC access detected - some data may not be collectible")
			}
		}
	}

	// Perform discovery simulation
	fmt.Printf("🔍 Simulating auto-discovery...\n")
	collectors, err := dre.discoverer.Discover(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("discovery simulation failed: %w", err)
	}

	result.Collectors = collectors
	result.Summary = dre.generateSummary(collectors, options)

	// Analyze image collection if enabled
	if options.IncludeImages {
		imageAnalysis := dre.analyzeImageCollection(collectors)
		result.ImageAnalysis = imageAnalysis
	}

	// Generate recommendations
	result.Recommendations = dre.generateRecommendations(result)

	// Estimate collection size and duration
	result.EstimatedSize = dre.estimateCollectionSize(result)
	result.EstimatedDuration = dre.estimateCollectionDuration(result)

	return result, nil
}

// PrintResult outputs the dry-run result in the specified format
func (dre *DryRunExecutor) PrintResult(result *DryRunResult) error {
	switch dre.outputFormat {
	case "console":
		return dre.printConsoleResult(result)
	case "json":
		return dre.printJSONResult(result)
	case "yaml":
		return dre.printYAMLResult(result)
	default:
		return fmt.Errorf("unsupported output format: %s", dre.outputFormat)
	}
}

func (dre *DryRunExecutor) printConsoleResult(result *DryRunResult) error {
	fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("🔍 AUTO-DISCOVERY DRY RUN RESULTS\n")
	fmt.Printf(strings.Repeat("=", 60) + "\n\n")

	// Print configuration summary
	fmt.Printf("⚙️  Configuration:\n")
	fmt.Printf("  Namespaces: %v\n", result.Options.Namespaces)
	fmt.Printf("  Include Images: %v\n", result.Options.IncludeImages)
	fmt.Printf("  RBAC Check: %v\n", result.Options.RBACCheck)
	fmt.Printf("  Max Depth: %d\n", result.Options.MaxDepth)
	fmt.Printf("\n")

	// Print discovery summary
	fmt.Printf("📊 Discovery Summary:\n")
	fmt.Printf("  Total Collectors: %d\n", result.Summary.TotalCollectors)
	fmt.Printf("  Namespaces: %d (%v)\n", len(result.Summary.NamespacesIncluded), result.Summary.NamespacesIncluded)
	fmt.Printf("  Resource Types: %d\n", len(result.Summary.ResourceTypesIncluded))
	fmt.Printf("\n")

	// Print collectors by type
	fmt.Printf("📋 Collectors by Type:\n")
	for collectorType, count := range result.Summary.CollectorsByType {
		fmt.Printf("  %-20s: %d collectors\n", collectorType, count)
	}
	fmt.Printf("\n")

	// Print RBAC report if available
	if result.RBACReport != nil {
		fmt.Printf("🔐 RBAC Validation:\n")
		fmt.Printf("  Access Rate: %.1f%%\n", result.RBACReport.Summary.AccessRate*100)
		fmt.Printf("  Accessible Namespaces: %d\n", result.RBACReport.Summary.NamespaceAccess)
		fmt.Printf("  Accessible Resource Types: %d\n", result.RBACReport.Summary.ResourceTypeAccess)
		fmt.Printf("\n")
	}

	// Print image analysis if available
	if result.ImageAnalysis != nil {
		fmt.Printf("🖼️  Image Collection:\n")
		fmt.Printf("  Expected Images: %d\n", result.ImageAnalysis.ExpectedImages)
		fmt.Printf("  Unique Registries: %v\n", result.ImageAnalysis.UniqueRegistries)
		fmt.Printf("  Estimated Size: %s\n", result.ImageAnalysis.EstimatedSize)
		fmt.Printf("\n")
	}

	// Print estimates
	fmt.Printf("📏 Estimates:\n")
	fmt.Printf("  Collection Size: %s\n", result.EstimatedSize)
	fmt.Printf("  Collection Time: %v\n", result.EstimatedDuration.Round(time.Second))
	fmt.Printf("\n")

	// Print warnings
	if len(result.Warnings) > 0 {
		fmt.Printf("⚠️  Warnings:\n")
		for _, warning := range result.Warnings {
			fmt.Printf("  • %s\n", warning)
		}
		fmt.Printf("\n")
	}

	// Print recommendations
	if len(result.Recommendations) > 0 {
		fmt.Printf("💡 Recommendations:\n")
		for _, rec := range result.Recommendations {
			fmt.Printf("  • %s\n", rec)
		}
		fmt.Printf("\n")
	}

	// Print detailed collectors list if verbose
	if dre.verboseMode {
		fmt.Printf("📝 Detailed Collector List:\n")
		
		// Sort collectors by priority for display
		sortedCollectors := make([]autodiscovery.CollectorSpec, len(result.Collectors))
		copy(sortedCollectors, result.Collectors)
		sort.Slice(sortedCollectors, func(i, j int) bool {
			return sortedCollectors[i].Priority > sortedCollectors[j].Priority
		})

		for i, collector := range sortedCollectors {
			fmt.Printf("  [%3d] %-30s (type: %-15s, ns: %-15s, priority: %d)\n",
				i+1, collector.Name, collector.Type, collector.Namespace, collector.Priority)
		}
		fmt.Printf("\n")
	}

	fmt.Printf("✅ Dry run complete! Use the actual collection command to gather the data.\n\n")
	return nil
}

func (dre *DryRunExecutor) printJSONResult(result *DryRunResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal dry run result to JSON: %w", err)
	}
	
	fmt.Printf("%s\n", data)
	return nil
}

func (dre *DryRunExecutor) printYAMLResult(result *DryRunResult) error {
	// In a full implementation, this would use a YAML marshaler
	fmt.Printf("# Auto-Discovery Dry Run Result\n")
	fmt.Printf("timestamp: %s\n", result.Timestamp.Format(time.RFC3339))
	fmt.Printf("totalCollectors: %d\n", result.Summary.TotalCollectors)
	fmt.Printf("estimatedSize: %s\n", result.EstimatedSize)
	fmt.Printf("estimatedDuration: %s\n", result.EstimatedDuration.String())
	
	if len(result.Collectors) > 0 {
		fmt.Printf("\ncolletors:\n")
		for _, collector := range result.Collectors {
			fmt.Printf("  - name: %s\n", collector.Name)
			fmt.Printf("    type: %s\n", collector.Type)
			fmt.Printf("    namespace: %s\n", collector.Namespace)
			fmt.Printf("    priority: %d\n", collector.Priority)
		}
	}
	
	return nil
}

// Helper functions for dry-run analysis

func (dre *DryRunExecutor) generateSummary(collectors []autodiscovery.CollectorSpec, options autodiscovery.DiscoveryOptions) DryRunSummary {
	summary := DryRunSummary{
		TotalCollectors:       len(collectors),
		CollectorsByType:      make(map[string]int),
		CollectorsByNamespace: make(map[string]int),
		CollectorsByPriority:  make(map[string]int),
		NamespacesIncluded:    make([]string, 0),
		ResourceTypesIncluded: make([]string, 0),
	}

	namespaceSet := make(map[string]bool)
	resourceTypeSet := make(map[string]bool)

	for _, collector := range collectors {
		// Count by type
		summary.CollectorsByType[collector.Type]++

		// Count by namespace
		if collector.Namespace != "" {
			summary.CollectorsByNamespace[collector.Namespace]++
			namespaceSet[collector.Namespace] = true
		}

		// Count by priority
		priorityName := dre.getPriorityName(collector.Priority)
		summary.CollectorsByPriority[priorityName]++

		// Track resource types
		resourceTypeSet[collector.Type] = true
	}

	// Convert sets to slices
	for ns := range namespaceSet {
		summary.NamespacesIncluded = append(summary.NamespacesIncluded, ns)
	}
	for resourceType := range resourceTypeSet {
		summary.ResourceTypesIncluded = append(summary.ResourceTypesIncluded, resourceType)
	}

	// Sort for consistent output
	sort.Strings(summary.NamespacesIncluded)
	sort.Strings(summary.ResourceTypesIncluded)

	return summary
}

func (dre *DryRunExecutor) analyzeImageCollection(collectors []autodiscovery.CollectorSpec) *DryRunImageAnalysis {
	analysis := &DryRunImageAnalysis{
		Enabled:          true,
		UniqueRegistries: make([]string, 0),
		AuthRequirements: make([]string, 0),
	}

	// Count log collectors (which indicate pods with images)
	logCollectors := 0
	namespacesWithPods := make(map[string]bool)

	for _, collector := range collectors {
		if collector.Type == "logs" {
			logCollectors++
			if collector.Namespace != "" {
				namespacesWithPods[collector.Namespace] = true
			}
		}
	}

	// Estimate number of images based on log collectors
	// Rough estimate: 2-3 images per namespace with pods
	analysis.ExpectedImages = len(namespacesWithPods) * 3

	// Estimate common registries that would be encountered
	analysis.UniqueRegistries = []string{
		"index.docker.io",
		"gcr.io",
		"registry.k8s.io",
	}

	// Estimate size based on expected images
	if analysis.ExpectedImages == 0 {
		analysis.EstimatedSize = "None (no pods discovered)"
	} else if analysis.ExpectedImages < 10 {
		analysis.EstimatedSize = "Small (< 10MB)"
	} else if analysis.ExpectedImages < 50 {
		analysis.EstimatedSize = "Medium (10-50MB)"
	} else {
		analysis.EstimatedSize = "Large (> 50MB)"
	}

	// Determine auth requirements
	for _, registry := range analysis.UniqueRegistries {
		if registry != "index.docker.io" && registry != "registry.k8s.io" {
			analysis.AuthRequirements = append(analysis.AuthRequirements, 
				fmt.Sprintf("Credentials may be needed for %s", registry))
		}
	}

	return analysis
}

func (dre *DryRunExecutor) generateRecommendations(result *DryRunResult) []string {
	var recommendations []string

	// Check collector distribution
	if result.Summary.TotalCollectors == 0 {
		recommendations = append(recommendations, "No collectors would be generated - check namespace access and RBAC permissions")
		return recommendations
	}

	// Check for good practices
	if result.Summary.TotalCollectors > 100 {
		recommendations = append(recommendations, "Large number of collectors detected - consider using more specific namespace filtering")
	}

	// Check namespace distribution
	if len(result.Summary.NamespacesIncluded) > 20 {
		recommendations = append(recommendations, "Many namespaces included - consider excluding system namespaces for faster collection")
	}

	// Check RBAC
	if result.RBACReport != nil && result.RBACReport.Summary.AccessRate < 0.7 {
		recommendations = append(recommendations, "Limited RBAC access - consider using a ServiceAccount with broader permissions")
	}

	// Check image collection
	if result.ImageAnalysis != nil && len(result.ImageAnalysis.AuthRequirements) > 0 {
		recommendations = append(recommendations, "Configure registry credentials for private registries before collection")
	}

	// Performance recommendations
	logCollectors := result.Summary.CollectorsByType["logs"]
	if logCollectors > 20 {
		recommendations = append(recommendations, "Many log collectors - consider adjusting log retention settings to reduce size")
	}

	return recommendations
}

func (dre *DryRunExecutor) estimateCollectionSize(result *DryRunResult) string {
	size := 0

	// Base size from collectors
	size += result.Summary.TotalCollectors * 5 // 5MB average per collector

	// Add image metadata size if enabled
	if result.ImageAnalysis != nil && result.ImageAnalysis.Enabled {
		size += result.ImageAnalysis.ExpectedImages * 1 // 1MB average per image metadata
	}

	// Adjust for collector types
	logsCollectors := result.Summary.CollectorsByType["logs"]
	size += logsCollectors * 20 // Logs are larger

	clusterResourcesCollectors := result.Summary.CollectorsByType["cluster-resources"]
	size += clusterResourcesCollectors * 2 // YAML/JSON resources are smaller

	switch {
	case size < 50:
		return "Small (< 50MB)"
	case size < 200:
		return "Medium (50-200MB)"
	case size < 1000:
		return "Large (200MB-1GB)"
	default:
		return "Very Large (> 1GB)"
	}
}

func (dre *DryRunExecutor) estimateCollectionDuration(result *DryRunResult) time.Duration {
	// Base time: 10 seconds setup
	duration := 10 * time.Second

	// Add time per collector
	duration += time.Duration(result.Summary.TotalCollectors) * 2 * time.Second

	// Add extra time for image collection
	if result.ImageAnalysis != nil && result.ImageAnalysis.Enabled {
		duration += time.Duration(result.ImageAnalysis.ExpectedImages) * 5 * time.Second
	}

	// Add extra time for logs (slower to collect)
	logsCollectors := result.Summary.CollectorsByType["logs"]
	duration += time.Duration(logsCollectors) * 10 * time.Second

	return duration
}

func (dre *DryRunExecutor) getPriorityName(priority int) string {
	switch {
	case priority >= int(autodiscovery.PriorityCritical):
		return "Critical"
	case priority >= int(autodiscovery.PriorityHigh):
		return "High"
	case priority >= int(autodiscovery.PriorityNormal):
		return "Normal"
	default:
		return "Low"
	}
}

// SetRBACValidator sets the RBAC validator for dry-run RBAC checks
func (dre *DryRunExecutor) SetRBACValidator(validator *RBACValidator) {
	dre.rbacValidator = validator
}

// ValidateDryRunOptions validates options for dry-run mode
func ValidateDryRunOptions(options autodiscovery.DiscoveryOptions) error {
	// Validate namespaces
	for _, ns := range options.Namespaces {
		if ns == "" {
			return fmt.Errorf("namespace cannot be empty")
		}
		if strings.Contains(ns, " ") {
			return fmt.Errorf("namespace cannot contain spaces: %s", ns)
		}
	}

	// Validate max depth
	if options.MaxDepth < 0 || options.MaxDepth > 20 {
		return fmt.Errorf("maxDepth must be between 0 and 20")
	}

	return nil
}

// GenerateDryRunExample creates an example dry-run command
func GenerateDryRunExample() string {
	return `
Example dry-run commands:

# Basic dry run
support-bundle collect --auto --dry-run

# Dry run with specific namespaces
support-bundle collect --auto --namespace "default,app-*" --dry-run

# Comprehensive dry run with images and RBAC check
support-bundle collect --auto --include-images --rbac-check=report --dry-run

# Dry run with custom profile and JSON output
support-bundle collect --auto --profile comprehensive --dry-run --output json

# Dry run with exclusion patterns
support-bundle collect --auto --exclude "ns:kube-*,secrets" --dry-run --verbose
`
}
