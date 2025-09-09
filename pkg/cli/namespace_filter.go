package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NamespaceFilter handles namespace filtering for CLI commands
type NamespaceFilter struct {
	kubeClient    kubernetes.Interface
	includeList   []string
	excludeList   []string
	labelSelector string
	regexPattern  *regexp.Regexp
}

// NewNamespaceFilter creates a new namespace filter
func NewNamespaceFilter(kubeClient kubernetes.Interface) *NamespaceFilter {
	return &NamespaceFilter{
		kubeClient: kubeClient,
	}
}

// ParseNamespaceFlag parses various namespace flag formats
func (nf *NamespaceFilter) ParseNamespaceFlag(namespaceFlag string) error {
	if namespaceFlag == "" {
		return nil
	}

	// Handle different namespace flag formats:
	// 1. Simple comma-separated: "ns1,ns2,ns3"
	// 2. Include/exclude patterns: "include:ns1,ns2;exclude:system-*"
	// 3. Label selectors: "label:env=production"
	// 4. Regex patterns: "regex:app-.*"

	if strings.Contains(namespaceFlag, ";") {
		// Complex pattern with include/exclude
		return nf.parseComplexPattern(namespaceFlag)
	} else if strings.Contains(namespaceFlag, ":") {
		// Single pattern type
		return nf.parseSinglePattern(namespaceFlag)
	} else {
		// Simple comma-separated list
		nf.includeList = strings.Split(namespaceFlag, ",")
		return nil
	}
}

func (nf *NamespaceFilter) parseComplexPattern(pattern string) error {
	parts := strings.Split(pattern, ";")
	for _, part := range parts {
		if err := nf.parseSinglePattern(part); err != nil {
			return fmt.Errorf("invalid pattern %s: %w", part, err)
		}
	}
	return nil
}

func (nf *NamespaceFilter) parseSinglePattern(pattern string) error {
	if !strings.Contains(pattern, ":") {
		return fmt.Errorf("pattern must have format 'type:value'")
	}

	parts := strings.SplitN(pattern, ":", 2)
	patternType := parts[0]
	value := parts[1]

	switch patternType {
	case "include":
		nf.includeList = append(nf.includeList, strings.Split(value, ",")...)
	case "exclude":
		nf.excludeList = append(nf.excludeList, strings.Split(value, ",")...)
	case "label":
		nf.labelSelector = value
	case "regex":
		regex, err := regexp.Compile(value)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
		nf.regexPattern = regex
	default:
		return fmt.Errorf("unknown pattern type: %s", patternType)
	}

	return nil
}

// FilterNamespaces applies the filter to discover valid namespaces
func (nf *NamespaceFilter) FilterNamespaces(ctx context.Context) ([]string, error) {
	// If specific namespaces are included, validate they exist and are accessible
	if len(nf.includeList) > 0 {
		return nf.validateIncludedNamespaces(ctx)
	}

	// Otherwise, discover all accessible namespaces and apply filters
	return nf.discoverAndFilterNamespaces(ctx)
}

func (nf *NamespaceFilter) validateIncludedNamespaces(ctx context.Context) ([]string, error) {
	var validNamespaces []string
	
	for _, nsName := range nf.includeList {
		nsName = strings.TrimSpace(nsName)
		if nsName == "" {
			continue
		}

		// Check if namespace exists and is accessible
		_, err := nf.kubeClient.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
		if err != nil {
			fmt.Printf("Warning: namespace %s not accessible: %v\n", nsName, err)
			continue
		}

		// Apply exclusion filters
		if nf.isExcluded(nsName) {
			fmt.Printf("Info: namespace %s excluded by filter\n", nsName)
			continue
		}

		validNamespaces = append(validNamespaces, nsName)
	}

	return validNamespaces, nil
}

func (nf *NamespaceFilter) discoverAndFilterNamespaces(ctx context.Context) ([]string, error) {
	// Get all namespaces
	var namespaceList []string

	if nf.labelSelector != "" {
		// Use label selector
		listOptions := metav1.ListOptions{LabelSelector: nf.labelSelector}
		nsList, err := nf.kubeClient.CoreV1().Namespaces().List(ctx, listOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to list namespaces with label selector: %w", err)
		}
		
		for _, ns := range nsList.Items {
			namespaceList = append(namespaceList, ns.Name)
		}
	} else {
		// Get all namespaces
		nsList, err := nf.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list namespaces: %w", err)
		}
		
		for _, ns := range nsList.Items {
			namespaceList = append(namespaceList, ns.Name)
		}
	}

	// Apply filters
	var filteredNamespaces []string
	for _, nsName := range namespaceList {
		if nf.passesFilters(nsName) {
			filteredNamespaces = append(filteredNamespaces, nsName)
		}
	}

	return filteredNamespaces, nil
}

func (nf *NamespaceFilter) isExcluded(namespace string) bool {
	for _, excludePattern := range nf.excludeList {
		if matched, _ := filepath.Match(excludePattern, namespace); matched {
			return true
		}
	}
	return false
}

func (nf *NamespaceFilter) passesFilters(namespace string) bool {
	// Check exclusion patterns first
	if nf.isExcluded(namespace) {
		return false
	}

	// Apply regex filter if specified
	if nf.regexPattern != nil {
		if !nf.regexPattern.MatchString(namespace) {
			return false
		}
	}

	return true
}

// ValidateNamespaceFlags validates CLI namespace flag combinations
func ValidateNamespaceFlags(namespaceFlag string, auto bool) error {
	if !auto && namespaceFlag != "" {
		return fmt.Errorf("--namespace can only be used with --auto flag")
	}

	if namespaceFlag != "" {
		// Basic validation of namespace flag format
		if strings.Contains(namespaceFlag, " ") {
			return fmt.Errorf("namespace flag cannot contain spaces")
		}

		// Validate pattern syntax
		testFilter := NewNamespaceFilter(nil)
		if err := testFilter.ParseNamespaceFlag(namespaceFlag); err != nil {
			return fmt.Errorf("invalid namespace pattern: %w", err)
		}
	}

	return nil
}

// ParseNamespaceList parses a simple comma-separated namespace list
func ParseNamespaceList(namespaceFlag string) []string {
	if namespaceFlag == "" {
		return []string{}
	}

	namespaces := strings.Split(namespaceFlag, ",")
	var cleaned []string
	
	for _, ns := range namespaces {
		ns = strings.TrimSpace(ns)
		if ns != "" {
			cleaned = append(cleaned, ns)
		}
	}

	return cleaned
}

// GetDefaultNamespaces returns default namespaces for auto-discovery
func GetDefaultNamespaces() []string {
	return []string{
		"default",
		"kube-system", // Include system namespace for cluster diagnostics
	}
}

// GetRecommendedExcludedNamespaces returns namespaces typically excluded from collection
func GetRecommendedExcludedNamespaces() []string {
	return []string{
		"kube-node-lease",
		"kube-public",
		"local-path-storage",
	}
}
