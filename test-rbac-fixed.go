package main

import (
	"context"
	"fmt"
	"os"

	"github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type testCase struct {
	gvr       schema.GroupVersionResource
	namespace string
	name      string
}

func main() {
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		panic(err)
	}
	
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	
	rbacChecker := autodiscovery.NewRBACChecker(client)
	ctx := context.Background()

	// Test specific resource access
	testCases := []testCase{
		{
			gvr:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			namespace: "kube-system",
			name:      "pods in kube-system",
		},
		{
			gvr:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
			namespace: "kube-system", 
			name:      "secrets in kube-system",
		},
		{
			gvr:       schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			namespace: "kube-system",
			name:      "deployments in kube-system",
		},
		{
			gvr:       schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"},
			namespace: "default",
			name:      "services in default",
		},
	}

	fmt.Printf("🔐 RBAC Test Results Against Real K3s Cluster:\n")
	fmt.Printf("===============================================\n")
	
	for _, test := range testCases {
		allowed, err := rbacChecker.CheckResourceTypeAccess(ctx, test.gvr, test.namespace)
		
		status := "✅ ALLOWED"
		if err != nil {
			status = fmt.Sprintf("❌ ERROR: %v", err)
		} else if !allowed {
			status = "🚫 DENIED"
		}
		
		fmt.Printf("  %-30s: %s\n", test.name, status)
	}

	// Test namespace access
	fmt.Printf("\n🗂️ Namespace Access Test:\n")
	testNamespaces := []string{"default", "kube-system", "kube-public", "kube-node-lease"}
	accessible, err := rbacChecker.GetAccessibleNamespaces(ctx, testNamespaces)
	if err != nil {
		fmt.Printf("❌ Namespace access check failed: %v\n", err)
	} else {
		fmt.Printf("✅ Accessible namespaces: %v\n", accessible)
		fmt.Printf("📊 Access rate: %d/%d (%.1f%%)\n", 
			len(accessible), len(testNamespaces), 
			float64(len(accessible))/float64(len(testNamespaces))*100)
	}
}
