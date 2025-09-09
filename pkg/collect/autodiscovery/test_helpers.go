package autodiscovery

import (
	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// createTestScheme creates a runtime scheme with all the types we need for testing
func createTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	
	// Register core types
	corev1.AddToScheme(scheme)
	
	// Register apps types  
	appsv1.AddToScheme(scheme)
	
	// Register batch types
	batchv1.AddToScheme(scheme)
	
	// Register networking types
	networkingv1.AddToScheme(scheme)
	
	return scheme
}

// createTestDynamicClient creates a properly configured dynamic fake client
func createTestDynamicClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := createTestScheme()
	return dynamicfake.NewSimpleDynamicClient(scheme, objects...)
}
