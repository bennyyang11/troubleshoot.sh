package autodiscovery

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// AutoCollector defines the interface for auto-discovery functionality
type AutoCollector interface {
	Discover(ctx context.Context, opts DiscoveryOptions) ([]CollectorSpec, error)
	ValidatePermissions(ctx context.Context, resources []Resource) ([]Resource, error)
}

// DiscoveryOptions configures the auto-discovery process
type DiscoveryOptions struct {
	Namespaces    []string `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
	IncludeImages bool     `json:"includeImages,omitempty" yaml:"includeImages,omitempty"`
	RBACCheck     bool     `json:"rbacCheck,omitempty" yaml:"rbacCheck,omitempty"`
	MaxDepth      int      `json:"maxDepth,omitempty" yaml:"maxDepth,omitempty"`
}

// CollectorSpec represents a generated collector specification
// This will be converted to the appropriate troubleshoot.sh/v1beta3 collector type
type CollectorSpec struct {
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	Namespace  string                 `json:"namespace,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Priority   int                    `json:"priority,omitempty"`
}

// Resource represents a Kubernetes resource discovered during auto-discovery
type Resource struct {
	GVR       schema.GroupVersionResource `json:"gvr"`
	Namespace string                      `json:"namespace"`
	Name      string                      `json:"name"`
	Labels    map[string]string           `json:"labels,omitempty"`
	OwnerRefs []metav1.OwnerReference     `json:"ownerRefs,omitempty"`
}

// DiscoveryResult encapsulates the results of the discovery process
type DiscoveryResult struct {
	Collectors []CollectorSpec       `json:"collectors"`
	Resources  []Resource            `json:"resources"`
	Metadata   DiscoveryMetadata     `json:"metadata"`
	Errors     []DiscoveryError      `json:"errors,omitempty"`
}

// DiscoveryMetadata contains information about the discovery process
type DiscoveryMetadata struct {
	Timestamp     time.Time `json:"timestamp"`
	ClusterInfo   string    `json:"clusterInfo"`
	TotalResources int      `json:"totalResources"`
	FilteredResources int   `json:"filteredResources"`
	Duration      time.Duration `json:"duration"`
}

// DiscoveryError represents an error that occurred during discovery
type DiscoveryError struct {
	Resource    Resource `json:"resource"`
	Message     string   `json:"message"`
	Recoverable bool     `json:"recoverable"`
}

// ResourceFilter defines criteria for filtering discovered resources
type ResourceFilter struct {
	IncludeGVRs []schema.GroupVersionResource `json:"includeGVRs,omitempty"`
	ExcludeGVRs []schema.GroupVersionResource `json:"excludeGVRs,omitempty"`
	LabelSelector string                      `json:"labelSelector,omitempty"`
	NamespaceSelector string                  `json:"namespaceSelector,omitempty"`
}

// CollectorPriority defines priority levels for different collector types
type CollectorPriority int

const (
	PriorityLow CollectorPriority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)
