# Auto-Discovery System

The auto-discovery system provides intelligent, namespace-scoped collection of Kubernetes resources with RBAC awareness and configurable collector generation.

## Overview

The auto-discovery system consists of several key components:

- **Discoverer**: Main orchestrator that coordinates resource discovery and collector generation
- **RBAC Checker**: Validates permissions before attempting to collect resources
- **Namespace Scanner**: Enumerates resources within specified namespaces
- **Resource Expander**: Converts discovered resources into collector specifications
- **Config Manager**: Handles configuration loading and filtering rules

## Key Features

- **Namespace-scoped**: Respects namespace boundaries and permissions
- **RBAC-aware**: Only collects data the user has permission to access
- **Configurable**: Supports custom filtering and collector mapping rules
- **Deterministic**: Same cluster state produces consistent collection results
- **Extensible**: Easy to add new resource types and collector mappings

## Quick Start

```go
import (
    "context"
    "github.com/replicatedhq/troubleshoot/pkg/collect/autodiscovery"
    "k8s.io/client-go/tools/clientcmd"
)

// Load kubeconfig
config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
if err != nil {
    panic(err)
}

// Create discoverer
discoverer, err := autodiscovery.NewDiscoverer(config)
if err != nil {
    panic(err)
}

// Configure discovery options
opts := autodiscovery.DiscoveryOptions{
    Namespaces:    []string{"default", "my-app"},
    IncludeImages: true,
    RBACCheck:     true,
    MaxDepth:      3,
}

// Perform discovery
collectors, err := discoverer.Discover(context.TODO(), opts)
if err != nil {
    panic(err)
}

// collectors now contains auto-generated collector specifications
for _, collector := range collectors {
    fmt.Printf("Generated collector: %s (type: %s)\n", collector.Name, collector.Type)
}
```

## Configuration

The auto-discovery system supports comprehensive configuration through YAML or JSON files:

```yaml
defaultOptions:
  namespaces: [] # Empty means all accessible namespaces
  includeImages: true
  rbacCheck: true  
  maxDepth: 3

resourceFilters:
  - name: "exclude-system-secrets"
    matchGVRs:
      - group: ""
        version: "v1" 
        resource: "secrets"
    matchNamespaces: ["kube-system"]
    action: "exclude"

collectorMappings:
  - name: "database-logs"
    matchGVRs:
      - group: ""
        version: "v1"
        resource: "pods"
    collectorType: "logs"
    priority: 10
    parameters:
      maxAge: "7d"
      maxLines: 50000

excludes:
  - gvrs:
      - group: ""
        version: "v1"
        resource: "secrets"
    names: ["admin-password"]
    reason: "Contains sensitive credentials"
```

### Configuration Loading

```go
configManager := autodiscovery.NewConfigManager()
err := configManager.LoadFromFile("auto-discovery.yaml")
if err != nil {
    panic(err)
}

config := configManager.GetConfig()
options := configManager.GetDiscoveryOptions(&overrides)
```

## Resource Types

The system automatically discovers and generates collectors for:

### Core Resources
- Pods → `logs` collectors with targeted collection for problematic pods
- Services → `cluster-resources` collectors
- ConfigMaps → `cluster-resources` collectors  
- Secrets → `cluster-resources` collectors (with RBAC checks)
- Events → `cluster-resources` collectors (high priority)
- PersistentVolumeClaims → `cluster-resources` collectors

### Apps Resources
- Deployments → `cluster-resources` collectors (high priority)
- ReplicaSets → `cluster-resources` collectors
- StatefulSets → `cluster-resources` collectors (high priority)
- DaemonSets → `cluster-resources` collectors (high priority)

### Networking Resources  
- Ingresses → `cluster-resources` collectors
- NetworkPolicies → `cluster-resources` collectors
- Services → `run-pod` collectors for network diagnostics

### Batch Resources
- Jobs → `cluster-resources` collectors
- CronJobs → `cluster-resources` collectors

## Collector Generation Logic

The system uses intelligent heuristics to generate appropriate collectors:

### Log Collectors
- Generated for all pods in discovered namespaces
- Higher priority collectors created for pods with error indicators
- Configurable log retention and line limits

### Exec Collectors  
- Generated for pods running database, cache, or worker applications
- Executes diagnostic commands like `ps aux` for process information

### Copy Collectors
- Generated for pods running applications with important config files (nginx, databases)
- Copies `/etc/` directory contents by default

### Run-Pod Collectors
- Generated for namespaces with networking resources
- Creates network diagnostic pods using `netshoot` image
- Tests DNS resolution and cluster connectivity

## RBAC Integration

The system performs comprehensive RBAC validation:

```go
// Check individual resource access
allowed, err := rbacChecker.FilterByPermissions(ctx, resources)

// Check namespace access  
hasAccess, err := rbacChecker.CheckNamespaceAccess(ctx, "my-namespace")

// Check resource type access
canList, err := rbacChecker.CheckResourceTypeAccess(ctx, gvr, namespace)
```

## Integration with Support Bundle Collection

The auto-discovery system is designed to integrate with the `support-bundle collect --auto` command:

1. **Discovery Phase**: Discover resources in specified namespaces
2. **Permission Validation**: Filter resources based on RBAC permissions
3. **Collector Generation**: Generate appropriate collector specifications
4. **Collection Execution**: Execute collectors using existing collection engine
5. **Bundle Creation**: Package results into standard support bundle format

## Error Handling

The system is designed to be resilient:

- **Partial Failures**: If some resources can't be accessed, continue with others
- **RBAC Denials**: Skip unauthorized resources without failing entire discovery
- **Network Issues**: Retry transient failures, skip persistent ones
- **Malformed Resources**: Log warnings but continue processing

## Performance Considerations

- **Concurrent Discovery**: Namespace scanning happens in parallel
- **Lazy Evaluation**: Resources are only inspected when needed
- **Caching**: Kubernetes discovery API responses are cached
- **Rate Limiting**: Respects cluster API server rate limits

## Extension Points

### Custom Resource Mappings

Add support for custom resources:

```go
expander := NewResourceExpander()
expander.AddMapping("my-crd", CollectorMapping{
    CollectorType: "cluster-resources",
    Priority: int(PriorityHigh),
    ParameterBuilder: func(resource Resource) map[string]interface{} {
        return map[string]interface{}{
            "group": resource.GVR.Group,
            "version": resource.GVR.Version,
            "resource": resource.GVR.Resource,
        }
    },
})
```

### Custom Filters

Implement complex resource filtering:

```go
filter := ResourceFilter{
    LabelSelector: "app=my-app,environment=production",
    IncludeGVRs: []schema.GroupVersionResource{
        {Group: "", Version: "v1", Resource: "pods"},
        {Group: "apps", Version: "v1", Resource: "deployments"},
    },
}

resources, err := scanner.ScanNamespaces(ctx, namespaces, filter)
```

## Testing

The package includes comprehensive tests and examples:

```bash
go test ./pkg/collect/autodiscovery/...
go run ./pkg/collect/autodiscovery/example_test.go
```

## Future Enhancements

- **Image Metadata Collection**: Detailed image digest and vulnerability information
- **Custom Resource Discovery**: Automatic detection of CRDs and custom resources
- **Machine Learning Integration**: Intelligent collector selection based on cluster patterns
- **Multi-Cluster Support**: Discovery across multiple Kubernetes clusters
- **Streaming Collection**: Real-time resource discovery and collection
