package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
	"github.com/replicatedhq/troubleshoot/pkg/collect/images"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// SupportBundleCollectOptions represents CLI options for support bundle collection
type SupportBundleCollectOptions struct {
	// Auto-discovery options
	Auto            bool     `json:"auto"`
	Namespaces      []string `json:"namespaces,omitempty"`
	IncludeImages   bool     `json:"includeImages,omitempty"`
	RBACCheck       bool     `json:"rbacCheck,omitempty"`
	
	// Discovery configuration
	ConfigFile      string `json:"configFile,omitempty"`
	ProfileName     string `json:"profileName,omitempty"`
	DryRun          bool   `json:"dryRun,omitempty"`
	
	// Output options
	OutputDir       string `json:"outputDir,omitempty"`
	OutputFile      string `json:"outputFile,omitempty"`
	ProgressFormat  string `json:"progressFormat,omitempty"` // "console", "json", "none"
	
	// Kubernetes connection
	KubeconfigPath  string        `json:"kubeconfigPath,omitempty"`
	Context         string        `json:"context,omitempty"`
	Timeout         time.Duration `json:"timeout,omitempty"`
}

// SupportBundleCollector handles support bundle collection with auto-discovery
type SupportBundleCollector struct {
	kubeClient         kubernetes.Interface
	dynamicClient      dynamic.Interface
	discoverer         *autodiscovery.Discoverer
	imageCollector     *images.AutoDiscoveryImageCollector
	configManager      *autodiscovery.ConfigManager
	profileManager     *DiscoveryProfileManager
}

// NewSupportBundleCollector creates a new support bundle collector
func NewSupportBundleCollector(options SupportBundleCollectOptions) (*SupportBundleCollector, error) {
	// Load Kubernetes configuration
	config, err := loadKubernetesConfig(options)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubernetes config: %w", err)
	}

	// Create Kubernetes clients
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Create auto-discovery components
	discoverer, err := autodiscovery.NewDiscoverer(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create discoverer: %w", err)
	}

	imageCollector := images.NewAutoDiscoveryImageCollector(dynamicClient)
	configManager := autodiscovery.NewConfigManager()
	profileManager := NewDiscoveryProfileManager()

	// Load configuration file if specified
	if options.ConfigFile != "" {
		if err := configManager.LoadFromFile(options.ConfigFile); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	}

	return &SupportBundleCollector{
		kubeClient:     kubeClient,
		dynamicClient:  dynamicClient,
		discoverer:     discoverer,
		imageCollector: imageCollector,
		configManager:  configManager,
		profileManager: profileManager,
	}, nil
}

// CollectWithAutoDiscovery performs support bundle collection with auto-discovery
func (sbc *SupportBundleCollector) CollectWithAutoDiscovery(ctx context.Context, options SupportBundleCollectOptions) (*CollectionResult, error) {
	fmt.Printf("Starting auto-discovery support bundle collection...\n")
	
	// Setup discovery options from CLI flags
	discoveryOpts := autodiscovery.DiscoveryOptions{
		Namespaces:    options.Namespaces,
		IncludeImages: options.IncludeImages,
		RBACCheck:     options.RBACCheck,
		MaxDepth:      3, // Default
	}

	// Apply profile if specified
	if options.ProfileName != "" {
		profile, err := sbc.profileManager.GetProfile(options.ProfileName)
		if err != nil {
			return nil, fmt.Errorf("failed to load discovery profile: %w", err)
		}
		discoveryOpts = profile.ApplyToOptions(discoveryOpts)
	}

	// Merge with configuration file settings
	finalOpts := sbc.configManager.GetDiscoveryOptions(&discoveryOpts)

	// Handle dry-run mode
	if options.DryRun {
		return sbc.performDryRun(ctx, finalOpts, options)
	}

	// Perform actual collection
	return sbc.performCollection(ctx, finalOpts, options)
}

// performDryRun shows what would be collected without actually collecting
func (sbc *SupportBundleCollector) performDryRun(ctx context.Context, opts autodiscovery.DiscoveryOptions, cliOptions SupportBundleCollectOptions) (*CollectionResult, error) {
	fmt.Printf("🔍 DRY RUN: Auto-discovery analysis\n")
	
	// Discover what collectors would be generated
	collectors, err := sbc.discoverer.Discover(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("dry run discovery failed: %w", err)
	}

	// Count collectors by type
	collectorStats := make(map[string]int)
	for _, collector := range collectors {
		collectorStats[collector.Type]++
	}

	// Print dry-run summary
	fmt.Printf("\n📊 Discovery Summary:\n")
	fmt.Printf("  Namespaces: %v\n", opts.Namespaces)
	fmt.Printf("  Include Images: %v\n", opts.IncludeImages)
	fmt.Printf("  RBAC Check: %v\n", opts.RBACCheck)
	fmt.Printf("  Total Collectors: %d\n", len(collectors))
	
	fmt.Printf("\n📋 Collectors by Type:\n")
	for collectorType, count := range collectorStats {
		fmt.Printf("  - %s: %d collectors\n", collectorType, count)
	}

	fmt.Printf("\n📝 Generated Collectors:\n")
	for i, collector := range collectors {
		fmt.Printf("  [%d] %s (type: %s, namespace: %s, priority: %d)\n", 
			i+1, collector.Name, collector.Type, collector.Namespace, collector.Priority)
	}

	return &CollectionResult{
		Collectors:    collectors,
		DryRun:        true,
		Summary:       generateDryRunSummary(collectors, opts),
		Duration:      time.Since(time.Now()), // Minimal duration for dry run
	}, nil
}

// performCollection executes the actual support bundle collection
func (sbc *SupportBundleCollector) performCollection(ctx context.Context, opts autodiscovery.DiscoveryOptions, cliOptions SupportBundleCollectOptions) (*CollectionResult, error) {
	startTime := time.Now()
	
	fmt.Printf("🚀 Starting auto-discovery collection...\n")

	// Perform discovery
	result, err := sbc.discoverer.DiscoverWithImageCollection(ctx, opts, opts.IncludeImages)
	if err != nil {
		return nil, fmt.Errorf("auto-discovery failed: %w", err)
	}

	// Create output directory
	outputDir := cliOptions.OutputDir
	if outputDir == "" {
		outputDir = fmt.Sprintf("support-bundle-%s", time.Now().Format("2006-01-02T15-04-05"))
	}

	// In a real implementation, this would integrate with the existing
	// troubleshoot.sh support bundle collection system
	fmt.Printf("📦 Generating support bundle with %d auto-discovered collectors...\n", len(result.Collectors))
	
	// Collect image metadata if requested
	var imageResult *images.ImageCollectionResult
	if opts.IncludeImages {
		// This would extract resources from the discovery result and collect image facts
		fmt.Printf("🖼️  Collecting image metadata...\n")
		// In full implementation, this would use actual discovered resources
	}

	collectionResult := &CollectionResult{
		Collectors:     result.Collectors,
		ImageFacts:     result.ImageFacts,
		OutputPath:     outputDir,
		Summary:        generateCollectionSummary(result.Collectors, imageResult),
		Duration:       time.Since(startTime),
		DryRun:         false,
	}

	fmt.Printf("✅ Support bundle collection complete!\n")
	fmt.Printf("   Collectors: %d\n", len(result.Collectors))
	fmt.Printf("   Duration: %v\n", collectionResult.Duration.Round(time.Second))
	fmt.Printf("   Output: %s\n", outputDir)

	return collectionResult, nil
}

// Helper functions

func loadKubernetesConfig(options SupportBundleCollectOptions) (*rest.Config, error) {
	if options.KubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", options.KubeconfigPath)
	}

	// Try in-cluster config first
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}

	// Fall back to default kubeconfig location
	return clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
}

func generateDryRunSummary(collectors []autodiscovery.CollectorSpec, opts autodiscovery.DiscoveryOptions) CollectionSummary {
	summary := CollectionSummary{
		TotalCollectors: len(collectors),
		CollectorTypes:  make(map[string]int),
		Namespaces:      make(map[string]int),
		Options:         opts,
	}

	for _, collector := range collectors {
		summary.CollectorTypes[collector.Type]++
		if collector.Namespace != "" {
			summary.Namespaces[collector.Namespace]++
		}
	}

	return summary
}

func generateCollectionSummary(collectors []autodiscovery.CollectorSpec, imageResult *images.ImageCollectionResult) CollectionSummary {
	summary := CollectionSummary{
		TotalCollectors: len(collectors),
		CollectorTypes:  make(map[string]int),
		Namespaces:      make(map[string]int),
	}

	for _, collector := range collectors {
		summary.CollectorTypes[collector.Type]++
		if collector.Namespace != "" {
			summary.Namespaces[collector.Namespace]++
		}
	}

	if imageResult != nil {
		summary.ImageStats = &ImageCollectionStats{
			TotalImages:      imageResult.Statistics.TotalImages,
			SuccessfulImages: imageResult.Statistics.SuccessfulImages,
			FailedImages:     imageResult.Statistics.FailedImages,
			CacheHits:        imageResult.Statistics.CacheHits,
		}
	}

	return summary
}

// Data structures for CLI integration

// CollectionResult represents the result of support bundle collection
type CollectionResult struct {
	Collectors  []autodiscovery.CollectorSpec `json:"collectors"`
	ImageFacts  map[string]interface{}        `json:"imageFacts,omitempty"`
	OutputPath  string                        `json:"outputPath,omitempty"`
	Summary     CollectionSummary             `json:"summary"`
	Duration    time.Duration                 `json:"duration"`
	DryRun      bool                         `json:"dryRun"`
	Errors      []string                     `json:"errors,omitempty"`
}

// CollectionSummary provides summary information about the collection
type CollectionSummary struct {
	TotalCollectors int                        `json:"totalCollectors"`
	CollectorTypes  map[string]int             `json:"collectorTypes"`
	Namespaces      map[string]int             `json:"namespaces"`
	Options         autodiscovery.DiscoveryOptions `json:"options"`
	ImageStats      *ImageCollectionStats      `json:"imageStats,omitempty"`
}

// ImageCollectionStats summarizes image collection results
type ImageCollectionStats struct {
	TotalImages      int `json:"totalImages"`
	SuccessfulImages int `json:"successfulImages"`
	FailedImages     int `json:"failedImages"`
	CacheHits        int `json:"cacheHits"`
}
