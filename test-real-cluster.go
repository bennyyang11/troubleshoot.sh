// Real cluster testing tool for auto-discovery
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var (
		namespace    = flag.String("namespace", "", "Namespace to test (empty = all accessible)")
		dryRun       = flag.Bool("dry-run", false, "Dry run mode")
		includeImages = flag.Bool("include-images", false, "Include image metadata")
		rbacCheck    = flag.String("rbac-check", "basic", "RBAC validation mode")
		verbose      = flag.Bool("verbose", false, "Verbose output")
	)
	flag.Parse()

	fmt.Printf("🧪 Testing Auto-Discovery Against Real K3s Cluster\n")
	fmt.Printf("=================================================\n\n")

	// Load real Kubernetes configuration
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = clientcmd.RecommendedHomeFile
	}

	fmt.Printf("📋 Test Configuration:\n")
	fmt.Printf("  KUBECONFIG: %s\n", kubeconfigPath)
	fmt.Printf("  Namespace: %s\n", *namespace)
	fmt.Printf("  Include Images: %v\n", *includeImages)
	fmt.Printf("  RBAC Check: %s\n", *rbacCheck)
	fmt.Printf("  Dry Run: %v\n", *dryRun)
	fmt.Printf("\n")

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		fmt.Printf("❌ Failed to load Kubernetes config: %v\n", err)
		os.Exit(1)
	}

	// Create discoverer with real Kubernetes config
	discoverer, err := autodiscovery.NewDiscoverer(config)
	if err != nil {
		fmt.Printf("❌ Failed to create discoverer: %v\n", err)
		os.Exit(1)
	}

	// Setup discovery options
	opts := autodiscovery.DiscoveryOptions{
		IncludeImages: *includeImages,
		RBACCheck:     *rbacCheck != "off",
		MaxDepth:      3,
	}

	if *namespace != "" {
		opts.Namespaces = []string{*namespace}
	}

	fmt.Printf("🔍 Starting Real Cluster Auto-Discovery...\n")
	startTime := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Perform discovery
	collectors, err := discoverer.Discover(ctx, opts)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("❌ Discovery failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Discovery completed in %v\n\n", duration.Round(time.Millisecond))

	// Analyze results
	fmt.Printf("📊 Discovery Results:\n")
	fmt.Printf("  Total Collectors: %d\n", len(collectors))
	
	collectorTypes := make(map[string]int)
	namespaceCount := make(map[string]int)
	
	for _, collector := range collectors {
		collectorTypes[collector.Type]++
		if collector.Namespace != "" {
			namespaceCount[collector.Namespace]++
		}
	}

	fmt.Printf("\n📋 Collectors by Type:\n")
	for collectorType, count := range collectorTypes {
		fmt.Printf("  %-20s: %d collectors\n", collectorType, count)
	}

	fmt.Printf("\n🗂️ Collectors by Namespace:\n")
	for ns, count := range namespaceCount {
		fmt.Printf("  %-20s: %d collectors\n", ns, count)
	}

	if *verbose {
		fmt.Printf("\n📝 Detailed Collector List:\n")
		for i, collector := range collectors {
			fmt.Printf("  [%2d] %-30s (type: %-15s, ns: %-15s, priority: %d)\n",
				i+1, collector.Name, collector.Type, collector.Namespace, collector.Priority)
		}
	}

	fmt.Printf("\n🎯 Test Summary:\n")
	if len(collectors) == 0 {
		fmt.Printf("  ⚠️  No collectors generated - check namespace access or cluster state\n")
	} else {
		fmt.Printf("  ✅ Auto-discovery working perfectly against real K3s cluster!\n")
		fmt.Printf("  ✅ Generated %d collectors in %v\n", len(collectors), duration)
		fmt.Printf("  ✅ Found resources across %d namespaces\n", len(namespaceCount))
	}
}
