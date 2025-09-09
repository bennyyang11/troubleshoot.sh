package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// RBACValidationMode defines different RBAC validation strategies
type RBACValidationMode int

const (
	RBACValidationOff RBACValidationMode = iota
	RBACValidationBasic
	RBACValidationStrict
	RBACValidationReportMode
)

// RBACValidator handles RBAC validation for CLI commands  
type RBACValidator struct {
	kubeClient  kubernetes.Interface
	rbacChecker *autodiscovery.RBACChecker
	mode        RBACValidationMode
	report      *RBACValidationReport
}

// RBACValidationReport contains detailed RBAC validation results
type RBACValidationReport struct {
	Timestamp         time.Time                   `json:"timestamp"`
	Mode              string                      `json:"mode"`
	NamespacesChecked []string                    `json:"namespacesChecked"`
	TotalResources    int                         `json:"totalResources"`
	AccessibleResources int                       `json:"accessibleResources"`
	DeniedResources   int                         `json:"deniedResources"`
	ResourceResults   []RBACResourceResult        `json:"resourceResults"`
	NamespaceResults  []RBACNamespaceResult       `json:"namespaceResults"`
	Summary           RBACValidationSummary       `json:"summary"`
}

// RBACResourceResult represents RBAC check result for a specific resource type
type RBACResourceResult struct {
	GVR        schema.GroupVersionResource `json:"gvr"`
	Namespace  string                      `json:"namespace"`
	GetAllowed bool                        `json:"getAllowed"`
	ListAllowed bool                       `json:"listAllowed"`
	Error      string                      `json:"error,omitempty"`
}

// RBACNamespaceResult represents RBAC check result for a namespace
type RBACNamespaceResult struct {
	Namespace string `json:"namespace"`
	Allowed   bool   `json:"allowed"`
	Error     string `json:"error,omitempty"`
}

// RBACValidationSummary provides summary statistics
type RBACValidationSummary struct {
	AccessRate        float64 `json:"accessRate"`        // 0.0 - 1.0
	NamespaceAccess   int     `json:"namespaceAccess"`   // Number of accessible namespaces
	ResourceTypeAccess int    `json:"resourceTypeAccess"` // Number of accessible resource types
	Recommendations   []string `json:"recommendations"`
}

// NewRBACValidator creates a new RBAC validator
func NewRBACValidator(kubeClient kubernetes.Interface, mode RBACValidationMode) *RBACValidator {
	return &RBACValidator{
		kubeClient:  kubeClient,
		rbacChecker: autodiscovery.NewRBACChecker(kubeClient),
		mode:        mode,
		report: &RBACValidationReport{
			ResourceResults:  make([]RBACResourceResult, 0),
			NamespaceResults: make([]RBACNamespaceResult, 0),
		},
	}
}

// ValidateRBACAccess performs RBAC validation based on the specified mode
func (rv *RBACValidator) ValidateRBACAccess(ctx context.Context, namespaces []string) (*RBACValidationReport, error) {
	rv.report.Timestamp = time.Now()
	rv.report.Mode = rv.getModeString()
	rv.report.NamespacesChecked = namespaces

	switch rv.mode {
	case RBACValidationOff:
		return rv.generateOffReport(), nil
	case RBACValidationBasic:
		return rv.performBasicValidation(ctx, namespaces)
	case RBACValidationStrict:
		return rv.performStrictValidation(ctx, namespaces)
	case RBACValidationReportMode:
		return rv.performDetailedValidation(ctx, namespaces)
	default:
		return nil, fmt.Errorf("unknown RBAC validation mode: %d", rv.mode)
	}
}

// performBasicValidation checks basic namespace and resource access
func (rv *RBACValidator) performBasicValidation(ctx context.Context, namespaces []string) (*RBACValidationReport, error) {
	fmt.Printf("🔐 Performing basic RBAC validation...\n")

	// Check namespace access
	accessibleNamespaces, err := rv.rbacChecker.GetAccessibleNamespaces(ctx, namespaces)
	if err != nil {
		return nil, fmt.Errorf("failed to check namespace access: %w", err)
	}

	for _, ns := range namespaces {
		allowed := false
		for _, accessible := range accessibleNamespaces {
			if ns == accessible {
				allowed = true
				break
			}
		}
		
		rv.report.NamespaceResults = append(rv.report.NamespaceResults, RBACNamespaceResult{
			Namespace: ns,
			Allowed:   allowed,
		})
	}

	// Check basic resource types
	basicResources := []schema.GroupVersionResource{
		{Group: "", Version: "v1", Resource: "pods"},
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "apps", Version: "v1", Resource: "deployments"},
	}

	for _, gvr := range basicResources {
		for _, ns := range accessibleNamespaces {
			allowed, err := rv.rbacChecker.CheckResourceTypeAccess(ctx, gvr, ns)
			errorMsg := ""
			if err != nil {
				errorMsg = err.Error()
			}

			rv.report.ResourceResults = append(rv.report.ResourceResults, RBACResourceResult{
				GVR:         gvr,
				Namespace:   ns,
				GetAllowed:  allowed,
				ListAllowed: allowed, // Simplified for basic validation
				Error:       errorMsg,
			})

			if allowed {
				rv.report.AccessibleResources++
			} else {
				rv.report.DeniedResources++
			}
		}
	}

	rv.report.TotalResources = len(rv.report.ResourceResults)
	rv.generateSummary()

	return rv.report, nil
}

// performStrictValidation checks all resource types with detailed permissions
func (rv *RBACValidator) performStrictValidation(ctx context.Context, namespaces []string) (*RBACValidationReport, error) {
	fmt.Printf("🔒 Performing strict RBAC validation...\n")

	// First do basic validation
	if _, err := rv.performBasicValidation(ctx, namespaces); err != nil {
		return nil, err
	}

	// Get all supported resource types from discoverer
	discoverer := &autodiscovery.Discoverer{} // Just for getting resource types
	allGVRs := discoverer.GetSupportedResourceTypes()

	accessibleNamespaces, _ := rv.rbacChecker.GetAccessibleNamespaces(ctx, namespaces)

	// Check each resource type individually
	for _, gvr := range allGVRs {
		for _, ns := range accessibleNamespaces {
			// Check both get and list permissions separately
			getResult := rv.checkSinglePermission(ctx, gvr, ns, "get")
			listResult := rv.checkSinglePermission(ctx, gvr, ns, "list")

			rv.report.ResourceResults = append(rv.report.ResourceResults, RBACResourceResult{
				GVR:         gvr,
				Namespace:   ns,
				GetAllowed:  getResult.allowed,
				ListAllowed: listResult.allowed,
				Error:       combineErrors(getResult.error, listResult.error),
			})

			if getResult.allowed || listResult.allowed {
				rv.report.AccessibleResources++
			} else {
				rv.report.DeniedResources++
			}
		}
	}

	rv.report.TotalResources = len(rv.report.ResourceResults)
	rv.generateSummary()

	return rv.report, nil
}

// performDetailedValidation creates a comprehensive RBAC report
func (rv *RBACValidator) performDetailedValidation(ctx context.Context, namespaces []string) (*RBACValidationReport, error) {
	fmt.Printf("📊 Generating detailed RBAC validation report...\n")

	// Perform strict validation first
	if _, err := rv.performStrictValidation(ctx, namespaces); err != nil {
		return nil, err
	}

	// Add detailed recommendations based on findings
	rv.generateRecommendations()

	return rv.report, nil
}

// Helper functions

type permissionResult struct {
	allowed bool
	error   string
}

func (rv *RBACValidator) checkSinglePermission(ctx context.Context, gvr schema.GroupVersionResource, namespace, verb string) permissionResult {
	// This would use a more detailed permission check
	// For now, use the existing RBAC checker
	allowed, err := rv.rbacChecker.CheckResourceTypeAccess(ctx, gvr, namespace)
	
	errorMsg := ""
	if err != nil {
		errorMsg = err.Error()
	}

	return permissionResult{
		allowed: allowed,
		error:   errorMsg,
	}
}

func combineErrors(err1, err2 string) string {
	if err1 == "" && err2 == "" {
		return ""
	}
	if err1 == "" {
		return err2
	}
	if err2 == "" {
		return err1
	}
	return fmt.Sprintf("get: %s; list: %s", err1, err2)
}

func (rv *RBACValidator) generateSummary() {
	if rv.report.TotalResources > 0 {
		rv.report.Summary.AccessRate = float64(rv.report.AccessibleResources) / float64(rv.report.TotalResources)
	}

	// Count accessible namespaces
	for _, nsResult := range rv.report.NamespaceResults {
		if nsResult.Allowed {
			rv.report.Summary.NamespaceAccess++
		}
	}

	// Count accessible resource types
	resourceTypes := make(map[string]bool)
	for _, resResult := range rv.report.ResourceResults {
		if resResult.GetAllowed || resResult.ListAllowed {
			key := fmt.Sprintf("%s/%s", resResult.GVR.Group, resResult.GVR.Resource)
			resourceTypes[key] = true
		}
	}
	rv.report.Summary.ResourceTypeAccess = len(resourceTypes)
}

func (rv *RBACValidator) generateRecommendations() {
	recommendations := []string{}

	// Check access rate
	if rv.report.Summary.AccessRate < 0.5 {
		recommendations = append(recommendations, "Consider using a ServiceAccount with broader permissions for troubleshooting")
	}

	// Check namespace access
	if rv.report.Summary.NamespaceAccess == 0 {
		recommendations = append(recommendations, "No namespace access detected - verify cluster connection and authentication")
	}

	// Check for common missing permissions
	hasPodsAccess := false
	hasEventsAccess := false
	
	for _, resResult := range rv.report.ResourceResults {
		if resResult.GVR.Resource == "pods" && (resResult.GetAllowed || resResult.ListAllowed) {
			hasPodsAccess = true
		}
		if resResult.GVR.Resource == "events" && (resResult.GetAllowed || resResult.ListAllowed) {
			hasEventsAccess = true
		}
	}

	if !hasPodsAccess {
		recommendations = append(recommendations, "Add pods read access for application troubleshooting")
	}
	if !hasEventsAccess {
		recommendations = append(recommendations, "Add events read access for cluster event analysis")
	}

	rv.report.Summary.Recommendations = recommendations
}

func (rv *RBACValidator) generateOffReport() *RBACValidationReport {
	return &RBACValidationReport{
		Timestamp: time.Now(),
		Mode:      "off",
		Summary: RBACValidationSummary{
			AccessRate: 1.0, // Assume full access when validation is off
		},
	}
}

func (rv *RBACValidator) getModeString() string {
	switch rv.mode {
	case RBACValidationOff:
		return "off"
	case RBACValidationBasic:
		return "basic"
	case RBACValidationStrict:
		return "strict"
	case RBACValidationReportMode:
		return "report"
	default:
		return "unknown"
	}
}

// ParseRBACCheckFlag parses the --rbac-check flag value
func ParseRBACCheckFlag(rbacCheck string) (RBACValidationMode, error) {
	if rbacCheck == "" {
		return RBACValidationOff, nil
	}

	switch strings.ToLower(rbacCheck) {
	case "false", "off", "disable", "no":
		return RBACValidationOff, nil
	case "true", "on", "enable", "yes", "basic":
		return RBACValidationBasic, nil
	case "strict":
		return RBACValidationStrict, nil
	case "report", "detailed", "full":
		return RBACValidationReportMode, nil
	default:
		return RBACValidationOff, fmt.Errorf("invalid rbac-check mode: %s (valid: off, basic, strict, report)", rbacCheck)
	}
}

// PrintRBACValidationReport prints the RBAC validation report to console
func PrintRBACValidationReport(report *RBACValidationReport) {
	fmt.Printf("\n🔐 RBAC Validation Report\n")
	fmt.Printf("Mode: %s\n", report.Mode)
	fmt.Printf("Timestamp: %s\n", report.Timestamp.Format(time.RFC3339))
	
	if len(report.NamespacesChecked) > 0 {
		fmt.Printf("\n📍 Namespaces Checked: %s\n", strings.Join(report.NamespacesChecked, ", "))
	}

	fmt.Printf("\n📊 Access Summary:\n")
	fmt.Printf("  Total Resources: %d\n", report.TotalResources)
	fmt.Printf("  Accessible: %d\n", report.AccessibleResources)
	fmt.Printf("  Denied: %d\n", report.DeniedResources)
	fmt.Printf("  Access Rate: %.1f%%\n", report.Summary.AccessRate*100)

	if len(report.NamespaceResults) > 0 {
		fmt.Printf("\n🗂️ Namespace Access:\n")
		for _, nsResult := range report.NamespaceResults {
			status := "✅"
			if !nsResult.Allowed {
				status = "❌"
			}
			fmt.Printf("  %s %s", status, nsResult.Namespace)
			if nsResult.Error != "" {
				fmt.Printf(" (error: %s)", nsResult.Error)
			}
			fmt.Printf("\n")
		}
	}

	if len(report.ResourceResults) > 0 && report.Mode == "detailed" {
		fmt.Printf("\n📋 Resource Type Access:\n")
		
		// Group by resource type for cleaner output
		resourceGroups := make(map[string][]RBACResourceResult)
		for _, resResult := range report.ResourceResults {
			key := resResult.GVR.Resource
			resourceGroups[key] = append(resourceGroups[key], resResult)
		}

		for resourceType, results := range resourceGroups {
			allowedCount := 0
			for _, result := range results {
				if result.GetAllowed || result.ListAllowed {
					allowedCount++
				}
			}
			
			accessRate := float64(allowedCount) / float64(len(results)) * 100
			fmt.Printf("  %s: %.0f%% access (%d/%d namespaces)\n", 
				resourceType, accessRate, allowedCount, len(results))
		}
	}

	if len(report.Summary.Recommendations) > 0 {
		fmt.Printf("\n💡 Recommendations:\n")
		for _, rec := range report.Summary.Recommendations {
			fmt.Printf("  • %s\n", rec)
		}
	}

	fmt.Printf("\n")
}

// GetRBACValidationSummary returns a brief summary for CLI output
func (rv *RBACValidator) GetRBACValidationSummary(report *RBACValidationReport) string {
	if report.Mode == "off" {
		return "RBAC validation: disabled"
	}

	summary := []string{
		fmt.Sprintf("RBAC validation: %s", report.Mode),
		fmt.Sprintf("  Access rate: %.1f%%", report.Summary.AccessRate*100),
		fmt.Sprintf("  Accessible namespaces: %d", report.Summary.NamespaceAccess),
		fmt.Sprintf("  Accessible resource types: %d", report.Summary.ResourceTypeAccess),
	}

	if report.AccessibleResources == 0 {
		summary = append(summary, "  ⚠️  No resource access detected")
	} else if report.Summary.AccessRate < 0.5 {
		summary = append(summary, "  ⚠️  Limited access detected")
	} else {
		summary = append(summary, "  ✅ Good access level")
	}

	return strings.Join(summary, "\n")
}

// ValidateMinimumPermissions checks if minimum required permissions are available
func (rv *RBACValidator) ValidateMinimumPermissions(ctx context.Context, namespaces []string) error {
	// Check basic requirements for troubleshoot.sh to function
	requiredPermissions := []struct {
		gvr         schema.GroupVersionResource
		description string
		critical    bool
	}{
		{
			gvr:         schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			description: "pod access for application logs",
			critical:    true,
		},
		{
			gvr:         schema.GroupVersionResource{Group: "", Version: "v1", Resource: "events"},
			description: "events access for cluster diagnostics",
			critical:    true,
		},
		{
			gvr:         schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			description: "deployment access for workload analysis",
			critical:    false,
		},
	}

	var missingCritical []string
	var missingOptional []string

	for _, perm := range requiredPermissions {
		hasAccess := false
		
		for _, ns := range namespaces {
			if allowed, err := rv.rbacChecker.CheckResourceTypeAccess(ctx, perm.gvr, ns); err == nil && allowed {
				hasAccess = true
				break
			}
		}

		if !hasAccess {
			if perm.critical {
				missingCritical = append(missingCritical, perm.description)
			} else {
				missingOptional = append(missingOptional, perm.description)
			}
		}
	}

	if len(missingCritical) > 0 {
		return fmt.Errorf("missing critical permissions: %s", strings.Join(missingCritical, ", "))
	}

	if len(missingOptional) > 0 {
		fmt.Printf("Warning: missing optional permissions: %s\n", strings.Join(missingOptional, ", "))
	}

	return nil
}
