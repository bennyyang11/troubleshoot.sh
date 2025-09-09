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

func main() {
	config, _ := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	client, _ := kubernetes.NewForConfig(config)
	
	rbacChecker := autodiscovery.NewRBACChecker(client)
	ctx := context.Background()

	// Test specific resource access
	testCases := []struct{
		gvr schema.GroupVersionResource
		namespace string
	}{
		{{Group: "", Version: "v1", Resource: "pods"}, "kube-system"},
		{{Group: "", Version: "v1", Resource: "secrets"}, "kube-system"},
		{{Group: "apps", Version: "v1", Resource: "deployments"}, "kube-system"},
	}

	fmt.Printf("🔐 RBAC Test Results:\n")
	for _, test := range testCases {
		allowed, err := rbacChecker.CheckResourceTypeAccess(ctx, test.gvr, test.namespace)
		status := "✅"
		if !allowed || err != nil {
			status = "❌"
		}
		fmt.Printf("  %s %s/%s in %s\n", status, test.gvr.Resource, test.gvr.Group, test.namespace)
	}
}
