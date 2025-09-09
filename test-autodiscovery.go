package main

import (
	"context"
	"fmt"
	"os"

	"github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	// Load cluster config
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		panic(err)
	}

	// Create discoverer
	discoverer, err := autodiscovery.NewDiscoverer(config)
	if err != nil {
		panic(err)
	}

	// Test discovery
	opts := autodiscovery.DiscoveryOptions{
		Namespaces: []string{"kube-system"}, // Test against kube-system
		RBACCheck:  true,
		MaxDepth:   2,
	}

	ctx := context.Background()
	collectors, err := discoverer.Discover(ctx, opts)
	if err != nil {
		panic(err)
	}

	fmt.Printf("🎯 Real Cluster Test Results:\n")
	fmt.Printf("Found %d collectors in kube-system namespace:\n", len(collectors))
	
	for i, collector := range collectors {
		fmt.Printf("  [%d] %s (type: %s, priority: %d)\n", 
			i+1, collector.Name, collector.Type, collector.Priority)
	}
}
