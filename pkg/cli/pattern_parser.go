package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// PatternParser handles parsing of exclusion/inclusion patterns from CLI flags and config
type PatternParser struct {
	exclusionPatterns []string
	inclusionPatterns []string
}

// NewPatternParser creates a new pattern parser
func NewPatternParser() *PatternParser {
	return &PatternParser{
		exclusionPatterns: make([]string, 0),
		inclusionPatterns: make([]string, 0),
	}
}

// ParseExclusionFlag parses the --exclude flag with various pattern formats
func (pp *PatternParser) ParseExclusionFlag(excludeFlag string) error {
	if excludeFlag == "" {
		return nil
	}

	// Support multiple patterns separated by semicolons or commas
	var patterns []string
	if strings.Contains(excludeFlag, ";") {
		patterns = strings.Split(excludeFlag, ";")
	} else {
		patterns = strings.Split(excludeFlag, ",")
	}

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			if err := pp.validatePattern(pattern); err != nil {
				return fmt.Errorf("invalid exclusion pattern '%s': %w", pattern, err)
			}
			pp.exclusionPatterns = append(pp.exclusionPatterns, pattern)
		}
	}

	return nil
}

// ParseInclusionFlag parses the --include flag with various pattern formats
func (pp *PatternParser) ParseInclusionFlag(includeFlag string) error {
	if includeFlag == "" {
		return nil
	}

	// Support multiple patterns separated by semicolons or commas
	var patterns []string
	if strings.Contains(includeFlag, ";") {
		patterns = strings.Split(includeFlag, ";")
	} else {
		patterns = strings.Split(includeFlag, ",")
	}

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			if err := pp.validatePattern(pattern); err != nil {
				return fmt.Errorf("invalid inclusion pattern '%s': %w", pattern, err)
			}
			pp.inclusionPatterns = append(pp.inclusionPatterns, pattern)
		}
	}

	return nil
}

// ConvertToResourceFilterRules converts parsed patterns to resource filter rules
func (pp *PatternParser) ConvertToResourceFilterRules() []autodiscovery.ResourceFilterRule {
	var rules []autodiscovery.ResourceFilterRule

	// Convert exclusion patterns
	for i, pattern := range pp.exclusionPatterns {
		rule, err := pp.patternToResourceFilter(pattern, "exclude", fmt.Sprintf("cli-exclude-%d", i))
		if err != nil {
			fmt.Printf("Warning: failed to convert exclusion pattern '%s': %v\n", pattern, err)
			continue
		}
		rules = append(rules, rule)
	}

	// Convert inclusion patterns
	for i, pattern := range pp.inclusionPatterns {
		rule, err := pp.patternToResourceFilter(pattern, "include", fmt.Sprintf("cli-include-%d", i))
		if err != nil {
			fmt.Printf("Warning: failed to convert inclusion pattern '%s': %v\n", pattern, err)
			continue
		}
		rules = append(rules, rule)
	}

	return rules
}

// validatePattern validates a pattern syntax
func (pp *PatternParser) validatePattern(pattern string) error {
	// Support various pattern formats:
	// 1. Resource type: "pods", "services", "deployments"
	// 2. Namespace: "ns:kube-system", "namespace:default"  
	// 3. Label selector: "label:app=web", "labels:env=production"
	// 4. GVR: "gvr:apps/v1/deployments"
	// 5. Wildcard: "kube-*", "*-system"
	// 6. Regex: "regex:^app-.*$"

	if pattern == "" {
		return fmt.Errorf("pattern cannot be empty")
	}

	// Check for valid pattern types
	if strings.Contains(pattern, ":") {
		parts := strings.SplitN(pattern, ":", 2)
		patternType := parts[0]
		value := parts[1]

		switch patternType {
		case "ns", "namespace":
			return pp.validateNamespacePattern(value)
		case "label", "labels":
			return pp.validateLabelPattern(value)
		case "gvr":
			return pp.validateGVRPattern(value)
		case "regex":
			return pp.validateRegexPattern(value)
		default:
			return fmt.Errorf("unknown pattern type: %s", patternType)
		}
	} else {
		// Simple resource name or wildcard pattern
		return pp.validateResourcePattern(pattern)
	}
}

func (pp *PatternParser) validateNamespacePattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("namespace pattern cannot be empty")
	}
	// Allow wildcards in namespace names
	if strings.Contains(pattern, "*") {
		// Validate wildcard pattern
		return pp.validateWildcardPattern(pattern)
	}
	return nil
}

func (pp *PatternParser) validateLabelPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("label pattern cannot be empty")
	}
	
	// Basic label selector validation
	// Format: key=value, key!=value, key, !key
	if !strings.Contains(pattern, "=") && !strings.Contains(pattern, "!") {
		// Simple key existence check
		if strings.Contains(pattern, " ") {
			return fmt.Errorf("label key cannot contain spaces")
		}
	} else {
		// Key-value selector
		// In a full implementation, this would use Kubernetes label selector parsing
		if strings.Count(pattern, "=") > 1 && !strings.Contains(pattern, ",") {
			return fmt.Errorf("multiple label selectors should be comma-separated")
		}
	}
	
	return nil
}

func (pp *PatternParser) validateGVRPattern(pattern string) error {
	// Format: group/version/resource or version/resource or resource
	parts := strings.Split(pattern, "/")
	
	switch len(parts) {
	case 1:
		// Just resource
		return pp.validateResourceName(parts[0])
	case 2:
		// version/resource (core resources)
		return pp.validateResourceName(parts[1])
	case 3:
		// group/version/resource
		return pp.validateResourceName(parts[2])
	default:
		return fmt.Errorf("invalid GVR format, expected group/version/resource")
	}
}

func (pp *PatternParser) validateRegexPattern(pattern string) error {
	_, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid regex: %w", err)
	}
	return nil
}

func (pp *PatternParser) validateResourcePattern(pattern string) error {
	// Allow wildcards in resource patterns
	if strings.Contains(pattern, "*") {
		return pp.validateWildcardPattern(pattern)
	}
	return pp.validateResourceName(pattern)
}

func (pp *PatternParser) validateWildcardPattern(pattern string) error {
	// Basic wildcard validation
	if strings.Count(pattern, "*") > 2 {
		return fmt.Errorf("too many wildcards in pattern")
	}
	
	// Don't allow only asterisk
	if pattern == "*" {
		return fmt.Errorf("pattern cannot be only '*'")
	}
	
	return nil
}

func (pp *PatternParser) validateResourceName(resource string) error {
	if resource == "" {
		return fmt.Errorf("resource name cannot be empty")
	}
	
	// Basic validation for Kubernetes resource names
	if strings.Contains(resource, " ") {
		return fmt.Errorf("resource name cannot contain spaces")
	}
	
	return nil
}

// patternToResourceFilter converts a pattern string to a ResourceFilterRule
func (pp *PatternParser) patternToResourceFilter(pattern, action, name string) (autodiscovery.ResourceFilterRule, error) {
	rule := autodiscovery.ResourceFilterRule{
		Name:   name,
		Action: action,
	}

	if strings.Contains(pattern, ":") {
		parts := strings.SplitN(pattern, ":", 2)
		patternType := parts[0]
		value := parts[1]

		switch patternType {
		case "ns", "namespace":
			rule.MatchNamespaces = []string{value}
		case "label", "labels":
			rule.LabelSelector = value
		case "gvr":
			gvr, err := pp.parseGVR(value)
			if err != nil {
				return rule, fmt.Errorf("invalid GVR: %w", err)
			}
			rule.MatchGVRs = []schema.GroupVersionResource{gvr}
		case "regex":
			// For regex patterns, we'd need to implement custom filtering
			// For now, store as namespace selector
			rule.MatchNamespaces = []string{value}
		}
	} else {
		// Simple resource name pattern
		gvr := pp.resourceNameToGVR(pattern)
		rule.MatchGVRs = []schema.GroupVersionResource{gvr}
	}

	return rule, nil
}

func (pp *PatternParser) parseGVR(gvrString string) (schema.GroupVersionResource, error) {
	parts := strings.Split(gvrString, "/")
	
	switch len(parts) {
	case 1:
		// Just resource (assume core/v1)
		return schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: parts[0],
		}, nil
	case 2:
		// version/resource (assume core group)
		return schema.GroupVersionResource{
			Group:    "",
			Version:  parts[0],
			Resource: parts[1],
		}, nil
	case 3:
		// group/version/resource
		return schema.GroupVersionResource{
			Group:    parts[0],
			Version:  parts[1],
			Resource: parts[2],
		}, nil
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("invalid GVR format")
	}
}

func (pp *PatternParser) resourceNameToGVR(resource string) schema.GroupVersionResource {
	// Map common resource names to their proper GVRs
	resourceMap := map[string]schema.GroupVersionResource{
		"pods":                  {Group: "", Version: "v1", Resource: "pods"},
		"services":              {Group: "", Version: "v1", Resource: "services"},
		"configmaps":            {Group: "", Version: "v1", Resource: "configmaps"},
		"secrets":               {Group: "", Version: "v1", Resource: "secrets"},
		"events":                {Group: "", Version: "v1", Resource: "events"},
		"persistentvolumeclaims": {Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
		"nodes":                 {Group: "", Version: "v1", Resource: "nodes"},
		"deployments":           {Group: "apps", Version: "v1", Resource: "deployments"},
		"replicasets":           {Group: "apps", Version: "v1", Resource: "replicasets"},
		"statefulsets":          {Group: "apps", Version: "v1", Resource: "statefulsets"},
		"daemonsets":            {Group: "apps", Version: "v1", Resource: "daemonsets"},
		"jobs":                  {Group: "batch", Version: "v1", Resource: "jobs"},
		"cronjobs":              {Group: "batch", Version: "v1", Resource: "cronjobs"},
		"ingresses":             {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		"networkpolicies":       {Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
	}

	if gvr, exists := resourceMap[resource]; exists {
		return gvr
	}

	// Default to core v1 resource
	return schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: resource,
	}
}

// GetPatternsHelp returns help text for pattern syntax
func GetPatternsHelp() string {
	return `
Pattern Syntax Help:

Resource Types:
  pods, services, deployments, configmaps, secrets, events, jobs, etc.

Namespace Patterns:
  ns:kube-system          - Specific namespace
  namespace:default       - Specific namespace
  ns:app-*               - Wildcard namespace pattern

Label Selectors:
  label:app=web          - Resource with specific label
  labels:env=production,tier=frontend  - Multiple labels

GVR (Group/Version/Resource):
  gvr:apps/v1/deployments    - Specific resource type
  gvr:v1/pods               - Core resource (empty group)
  gvr:networking.k8s.io/v1/ingresses - Full GVR

Regex Patterns:
  regex:^app-.*$         - Regex pattern for names

Examples:
  --exclude "kube-*,ns:kube-system,secrets"
  --include "label:app=myapp,deployments,services"
  --exclude "regex:.*-test$;ns:test-*"
`
}

// PatternMatchResult represents the result of pattern matching
type PatternMatchResult struct {
	Matched    bool   `json:"matched"`
	Pattern    string `json:"pattern"`
	MatchType  string `json:"matchType"` // "namespace", "resource", "label", "regex"
	Reason     string `json:"reason"`
}

// TestPatternMatching tests pattern matching against sample resources for validation
func (pp *PatternParser) TestPatternMatching(sampleResources []autodiscovery.Resource) []PatternMatchResult {
	var results []PatternMatchResult

	rules := pp.ConvertToResourceFilterRules()
	
	for _, resource := range sampleResources {
		for _, rule := range rules {
			matched := pp.testRuleAgainstResource(rule, resource)
			
			results = append(results, PatternMatchResult{
				Matched:   matched,
				Pattern:   fmt.Sprintf("%s:%s", rule.Action, rule.Name),
				MatchType: pp.getRuleMatchType(rule),
				Reason:    pp.getMatchReason(rule, resource, matched),
			})
		}
	}

	return results
}

func (pp *PatternParser) testRuleAgainstResource(rule autodiscovery.ResourceFilterRule, resource autodiscovery.Resource) bool {
	// Test GVR match
	if len(rule.MatchGVRs) > 0 {
		found := false
		for _, gvr := range rule.MatchGVRs {
			if resource.GVR == gvr {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Test namespace match
	if len(rule.MatchNamespaces) > 0 {
		found := false
		for _, ns := range rule.MatchNamespaces {
			if matchPattern(ns, resource.Namespace) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Test label selector
	if rule.LabelSelector != "" {
		// Simplified label matching for testing
		return pp.testLabelSelector(rule.LabelSelector, resource.Labels)
	}

	return true
}

func (pp *PatternParser) getRuleMatchType(rule autodiscovery.ResourceFilterRule) string {
	if len(rule.MatchGVRs) > 0 {
		return "resource"
	}
	if len(rule.MatchNamespaces) > 0 {
		return "namespace"
	}
	if rule.LabelSelector != "" {
		return "label"
	}
	return "unknown"
}

func (pp *PatternParser) getMatchReason(rule autodiscovery.ResourceFilterRule, resource autodiscovery.Resource, matched bool) string {
	if matched {
		return fmt.Sprintf("Resource %s/%s matched %s pattern", resource.Namespace, resource.Name, rule.Action)
	} else {
		return fmt.Sprintf("Resource %s/%s did not match %s pattern", resource.Namespace, resource.Name, rule.Action)
	}
}

func (pp *PatternParser) testLabelSelector(selector string, labels map[string]string) bool {
	// Simplified label selector testing
	// In a full implementation, this would use k8s.io/apimachinery/pkg/labels
	
	if labels == nil {
		labels = make(map[string]string)
	}

	// Handle simple key=value selectors
	if strings.Contains(selector, "=") {
		pairs := strings.Split(selector, ",")
		for _, pair := range pairs {
			pair = strings.TrimSpace(pair)
			if strings.Contains(pair, "=") {
				kv := strings.SplitN(pair, "=", 2)
				key := strings.TrimSpace(kv[0])
				value := strings.TrimSpace(kv[1])
				
				if labels[key] != value {
					return false
				}
			}
		}
		return true
	}

	// Handle simple key existence
	key := strings.TrimSpace(selector)
	_, exists := labels[key]
	return exists
}

// matchPattern tests if a value matches a pattern (supports wildcards)
func matchPattern(pattern, value string) bool {
	if pattern == value {
		return true
	}

	// Handle wildcard patterns
	if strings.Contains(pattern, "*") {
		// Convert shell-style wildcard to regex
		regexPattern := strings.ReplaceAll(pattern, "*", ".*")
		regexPattern = "^" + regexPattern + "$"
		
		matched, err := regexp.MatchString(regexPattern, value)
		if err != nil {
			return false
		}
		return matched
	}

	return false
}

// GetExclusionPatterns returns the parsed exclusion patterns
func (pp *PatternParser) GetExclusionPatterns() []string {
	return pp.exclusionPatterns
}

// GetInclusionPatterns returns the parsed inclusion patterns
func (pp *PatternParser) GetInclusionPatterns() []string {
	return pp.inclusionPatterns
}

// ClearPatterns clears all parsed patterns
func (pp *PatternParser) ClearPatterns() {
	pp.exclusionPatterns = make([]string, 0)
	pp.inclusionPatterns = make([]string, 0)
}

// GeneratePatternExamples creates example patterns for documentation
func GeneratePatternExamples() []PatternExample {
	return []PatternExample{
		{
			Name:        "Exclude system namespaces",
			Pattern:     "--exclude 'ns:kube-*'",
			Description: "Exclude all namespaces starting with 'kube-'",
		},
		{
			Name:        "Include only application pods",
			Pattern:     "--include 'label:app=myapp,pods'",
			Description: "Include only pods with label app=myapp",
		},
		{
			Name:        "Exclude secrets from system namespaces",
			Pattern:     "--exclude 'secrets;ns:kube-system'",
			Description: "Exclude secrets and everything from kube-system",
		},
		{
			Name:        "Include specific resource types",
			Pattern:     "--include 'gvr:apps/v1/deployments,gvr:v1/services'",
			Description: "Include only deployments and services",
		},
		{
			Name:        "Exclude test resources",
			Pattern:     "--exclude 'regex:.*-test$'",
			Description: "Exclude resources ending with '-test'",
		},
	}
}

// PatternExample represents an example pattern usage
type PatternExample struct {
	Name        string `json:"name"`
	Pattern     string `json:"pattern"`
	Description string `json:"description"`
}
