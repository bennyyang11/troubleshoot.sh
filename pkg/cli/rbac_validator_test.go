package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	authv1 "k8s.io/api/authorization/v1"
)

func TestParseRBACCheckFlag(t *testing.T) {
	tests := []struct {
		flag     string
		expected RBACValidationMode
		wantErr  bool
	}{
		{"", RBACValidationOff, false},
		{"false", RBACValidationOff, false},
		{"off", RBACValidationOff, false},
		{"no", RBACValidationOff, false},
		{"true", RBACValidationBasic, false},
		{"basic", RBACValidationBasic, false},
		{"yes", RBACValidationBasic, false},
		{"strict", RBACValidationStrict, false},
		{"report", RBACValidationReportMode, false},
		{"detailed", RBACValidationReportMode, false},
		{"invalid", RBACValidationOff, true},
	}

	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			result, err := ParseRBACCheckFlag(tt.flag)

			if tt.wantErr && err == nil {
				t.Errorf("Expected error for flag '%s' but got none", tt.flag)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error for flag '%s': %v", tt.flag, err)
			}

			if result != tt.expected {
				t.Errorf("Expected mode %d for flag '%s', got %d", tt.expected, tt.flag, result)
			}
		})
	}
}

func TestRBACValidator_ValidateRBACAccess(t *testing.T) {
	// Create mock client that allows access to some resources
	kubeClient := kubernetesfake.NewSimpleClientset()
	kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		req := action.(ktesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
		attrs := req.Spec.ResourceAttributes
		
		// Allow access to default namespace and basic resources
		allowed := attrs.Namespace == "default" && 
			(attrs.Resource == "pods" || attrs.Resource == "services" || attrs.Resource == "events")
		
		return true, &authv1.SelfSubjectAccessReview{
			Status: authv1.SubjectAccessReviewStatus{
				Allowed: allowed,
			},
		}, nil
	})

	tests := []struct {
		name       string
		mode       RBACValidationMode
		namespaces []string
		expectErr  bool
	}{
		{
			name:       "off mode",
			mode:       RBACValidationOff,
			namespaces: []string{"default"},
			expectErr:  false,
		},
		{
			name:       "basic mode with accessible namespace",
			mode:       RBACValidationBasic,
			namespaces: []string{"default"},
			expectErr:  false,
		},
		{
			name:       "strict mode",
			mode:       RBACValidationStrict,
			namespaces: []string{"default", "kube-system"},
			expectErr:  false,
		},
		{
			name:       "report mode",
			mode:       RBACValidationReportMode,
			namespaces: []string{"default"},
			expectErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewRBACValidator(kubeClient, tt.mode)
			ctx := context.Background()

			report, err := validator.ValidateRBACAccess(ctx, tt.namespaces)

			if tt.expectErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.expectErr {
				if report == nil {
					t.Errorf("Report should not be nil")
				} else {
					// Verify report structure
					if report.Mode == "" {
						t.Errorf("Report should have mode")
					}
					if report.Timestamp.IsZero() {
						t.Errorf("Report should have timestamp")
					}

					// For modes other than "off", should have namespace results
					if tt.mode != RBACValidationOff {
						if len(report.NamespaceResults) == 0 {
							t.Errorf("Report should have namespace results for mode %s", validator.getModeString())
						}
					}
				}
			}
		})
	}
}

func TestRBACValidator_ValidateMinimumPermissions(t *testing.T) {
	tests := []struct {
		name        string
		setupClient func() *kubernetesfake.Clientset
		expectError bool
	}{
		{
			name: "sufficient permissions",
			setupClient: func() *kubernetesfake.Clientset {
				client := kubernetesfake.NewSimpleClientset()
				client.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
					// Allow access to pods and events (critical permissions)
					req := action.(ktesting.CreateAction).GetObject().(*authv1.SelfSubjectAccessReview)
					attrs := req.Spec.ResourceAttributes
					
					allowed := attrs.Resource == "pods" || attrs.Resource == "events"
					
					return true, &authv1.SelfSubjectAccessReview{
						Status: authv1.SubjectAccessReviewStatus{
							Allowed: allowed,
						},
					}, nil
				})
				return client
			},
			expectError: false,
		},
		{
			name: "missing critical permissions",
			setupClient: func() *kubernetesfake.Clientset {
				client := kubernetesfake.NewSimpleClientset()
				client.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
					// Deny all access
					return true, &authv1.SelfSubjectAccessReview{
						Status: authv1.SubjectAccessReviewStatus{
							Allowed: false,
						},
					}, nil
				})
				return client
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setupClient()
			validator := NewRBACValidator(client, RBACValidationBasic)
			
			ctx := context.Background()
			err := validator.ValidateMinimumPermissions(ctx, []string{"default"})

			if tt.expectError && err == nil {
				t.Errorf("Expected error for insufficient permissions but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestRBACValidator_GetRBACValidationSummary(t *testing.T) {
	validator := NewRBACValidator(nil, RBACValidationBasic)

	tests := []struct {
		name     string
		report   *RBACValidationReport
		expected []string // Strings that should be in the summary
	}{
		{
			name: "off mode",
			report: &RBACValidationReport{
				Mode: "off",
			},
			expected: []string{"disabled"},
		},
		{
			name: "good access",
			report: &RBACValidationReport{
				Mode: "basic",
				Summary: RBACValidationSummary{
					AccessRate:         0.8,
					NamespaceAccess:    3,
					ResourceTypeAccess: 15,
				},
				AccessibleResources: 24,
			},
			expected: []string{"basic", "80.0%", "Good access level"},
		},
		{
			name: "limited access",
			report: &RBACValidationReport{
				Mode: "strict",
				Summary: RBACValidationSummary{
					AccessRate:         0.3,
					NamespaceAccess:    1,
					ResourceTypeAccess: 5,
				},
				AccessibleResources: 6,
			},
			expected: []string{"strict", "30.0%", "Limited access detected"},
		},
		{
			name: "no access",
			report: &RBACValidationReport{
				Mode: "report",
				Summary: RBACValidationSummary{
					AccessRate:         0.0,
					NamespaceAccess:    0,
					ResourceTypeAccess: 0,
				},
				AccessibleResources: 0,
			},
			expected: []string{"report", "0.0%", "No resource access detected"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := validator.GetRBACValidationSummary(tt.report)

			for _, expected := range tt.expected {
				if !strings.Contains(summary, expected) {
					t.Errorf("Summary should contain '%s': %s", expected, summary)
				}
			}
		})
	}
}

func TestPrintRBACValidationReport(t *testing.T) {
	report := &RBACValidationReport{
		Timestamp:         time.Now(),
		Mode:             "basic",
		NamespacesChecked: []string{"default", "app"},
		TotalResources:    10,
		AccessibleResources: 7,
		DeniedResources:   3,
		NamespaceResults: []RBACNamespaceResult{
			{Namespace: "default", Allowed: true},
			{Namespace: "app", Allowed: true},
			{Namespace: "kube-system", Allowed: false, Error: "access denied"},
		},
		ResourceResults: []RBACResourceResult{
			{
				GVR:         schema.GroupVersionResource{Resource: "pods"},
				Namespace:   "default",
				GetAllowed:  true,
				ListAllowed: true,
			},
			{
				GVR:         schema.GroupVersionResource{Resource: "secrets"},
				Namespace:   "default",
				GetAllowed:  false,
				ListAllowed: false,
				Error:       "permission denied",
			},
		},
		Summary: RBACValidationSummary{
			AccessRate:         0.7,
			NamespaceAccess:    2,
			ResourceTypeAccess: 8,
			Recommendations:    []string{"Consider using broader permissions"},
		},
	}

	// Test that printing doesn't error
	// In a real test, we might capture stdout to verify output format
	PrintRBACValidationReport(report)
	
	// This test mainly verifies the function doesn't panic or error
	// More sophisticated testing would capture and verify output
}

// Benchmark RBAC validation performance
func BenchmarkRBACValidator_ValidateRBACAccess(b *testing.B) {
	kubeClient := kubernetesfake.NewSimpleClientset()
	kubeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.SelfSubjectAccessReview{
			Status: authv1.SubjectAccessReviewStatus{
				Allowed: true,
			},
		}, nil
	})

	validator := NewRBACValidator(kubeClient, RBACValidationBasic)
	namespaces := []string{"default", "app", "production"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := validator.ValidateRBACAccess(ctx, namespaces)
		if err != nil {
			b.Fatalf("RBAC validation failed: %v", err)
		}
	}
}
