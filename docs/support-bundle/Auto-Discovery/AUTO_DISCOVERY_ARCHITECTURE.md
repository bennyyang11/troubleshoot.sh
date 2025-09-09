# 🎯 Auto-Discovery Architecture: Two-Path System

## ✅ **You Are Correct!** 

The auto-discovery system **splits into 2 paths** that both build from the same foundation but offer different levels of customization.

---

## 🏗️ **PATH 1: Foundation Discovery (No YAML File)**

**Command**: `support-bundle collect --auto`

### **Foundation Resources (Always Discovered):**

#### **📦 Core Kubernetes Resources**
| Resource | GVR | Collector Type | Priority | Purpose |
|----------|-----|----------------|----------|---------|
| **Pods** | `v1/pods` | `logs` | **HIGH** | Container logs for troubleshooting |
| **Services** | `v1/services` | `cluster-resources` + `run-pod` | NORMAL | Service definitions + network diagnostics |
| **ConfigMaps** | `v1/configmaps` | `cluster-resources` | NORMAL | Configuration data |
| **Secrets** | `v1/secrets` | `cluster-resources` | NORMAL | Secret data (RBAC permitting) |
| **Events** | `v1/events` | `cluster-resources` | **HIGH** | Cluster events (critical for debugging) |
| **PersistentVolumeClaims** | `v1/persistentvolumeclaims` | `cluster-resources` | NORMAL | Storage claims |

#### **🚀 Application Workloads**
| Resource | GVR | Collector Type | Priority | Purpose |
|----------|-----|----------------|----------|---------|
| **Deployments** | `apps/v1/deployments` | `cluster-resources` | **HIGH** | App deployment specs |
| **ReplicaSets** | `apps/v1/replicasets` | `cluster-resources` | NORMAL | Replica set status |
| **StatefulSets** | `apps/v1/statefulsets` | `cluster-resources` | **HIGH** | Stateful app specs |
| **DaemonSets** | `apps/v1/daemonsets` | `cluster-resources` | **HIGH** | Node-level workloads |

#### **🌐 Networking Resources**
| Resource | GVR | Collector Type | Priority | Purpose |
|----------|-----|----------------|----------|---------|
| **Ingresses** | `networking.k8s.io/v1/ingresses` | `cluster-resources` | NORMAL | Ingress rules |
| **NetworkPolicies** | `networking.k8s.io/v1/networkpolicies` | `cluster-resources` | NORMAL | Network security |

#### **⚙️ Batch & Storage**
| Resource | GVR | Collector Type | Priority | Purpose |
|----------|-----|----------------|----------|---------|
| **Jobs** | `batch/v1/jobs` | `cluster-resources` | NORMAL | Batch job specs |
| **CronJobs** | `batch/v1/cronjobs` | `cluster-resources` | NORMAL | Scheduled jobs |
| **StorageClasses** | `storage.k8s.io/v1/storageclasses` | `cluster-resources` | LOW | Storage configs |

### **Foundation Behavior:**
- ✅ **Scans all accessible namespaces** (respects RBAC)
- ✅ **Excludes system namespaces** by default (`kube-system`, `kube-public`, `kube-node-lease`) 
- ✅ **Dependency resolution** up to 3 levels deep
- ✅ **Priority-based ordering** (HIGH priority collectors first)
- ✅ **RBAC enforcement** (only collects what you can access)
- ✅ **Network diagnostics** generated for namespaces with services/ingresses

---

## 🚀 **PATH 2: Enhanced Discovery (With YAML Configuration)**

**Command**: `support-bundle collect --auto --config my-config.yaml`

### **YAML Adds These Capabilities Beyond Foundation:**

#### **📋 Custom Resource Filters**
```yaml
resourceFilters:
  # Include custom resources (CRDs)
  - name: "istio-resources"
    action: "include"
    matchGVRs:
      - group: "networking.istio.io"
        version: "v1beta1"
        resource: "virtualservices"
      - group: "networking.istio.io"
        version: "v1beta1"  
        resource: "destinationrules"
  
  # Exclude development environments
  - name: "exclude-dev"
    action: "exclude"
    matchNamespaces: ["dev-*", "test-*"]
```

#### **⚙️ Custom Collector Mappings**
```yaml
collectorMappings:
  # Extended database troubleshooting
  - name: "database-deep-logs"
    matchGVRs: [{group: "", version: "v1", resource: "pods"}]
    collectorType: "logs"
    priority: 20
    parameters:
      maxAge: "168h"        # 7 days of logs
      maxLines: 100000      # 100k lines instead of 10k
      previous: true        # Include crashed container logs
      selector: ["app=database"]
      
  # Custom exec for database diagnostics  
  - name: "database-diagnostics"
    matchGVRs: [{group: "", version: "v1", resource: "pods"}]
    collectorType: "exec"
    priority: 15
    parameters:
      command: ["pg_dump", "--schema-only"]
      timeout: "60s"
    condition: "labels['app'] == 'postgresql'"
```

#### **🚫 Advanced Exclusions**
```yaml
excludes:
  # Exclude sensitive data
  - gvrs: [{group: "", version: "v1", resource: "secrets"}]
    names: ["admin-*", "root-password", "ssl-certs"]
    reason: "Sensitive credentials excluded for security"
    
  # Exclude monitoring overhead
  - namespaces: ["monitoring", "prometheus"]
    reason: "Monitoring infrastructure not relevant for app troubleshooting"
```

#### **✅ Advanced Inclusions**
```yaml
includes:
  # Always include custom application metrics
  - gvrs: [{group: "custom.company.com", version: "v1", resource: "appmetrics"}]
    priority: 25
    
  # Include cluster-wide networking config
  - gvrs: [{group: "networking.k8s.io", version: "v1", resource: "ingressclasses"}]
    priority: 10
```

---

## 📊 **Foundation Collector Types Explained**

### **📝 `logs` Collectors**
**Generated for**: All pods found  
**What they collect**: 
- Container stdout/stderr logs
- Previous container logs (if crashed)
- Configurable retention (default: 72h, max 10k lines)
- Automatic detection of error patterns

**Example Output**:
```yaml
- logs:
    name: auto-logs-pod-nginx-abc123
    namespace: production
    selector: ["name=nginx-abc123"]
    maxAge: "72h"
    maxLines: 10000
```

### **📊 `cluster-resources` Collectors**  
**Generated for**: Services, ConfigMaps, Deployments, etc.  
**What they collect**:
- Complete YAML/JSON specification of Kubernetes objects
- Current status and conditions
- Metadata, labels, annotations
- Related object references

**Example Output**:
```yaml
- clusterResources:
    name: auto-resources-services
    namespace: production
    include: 
      - group: ""
        version: "v1"
        resource: "services"
```

### **🏃 `run-pod` Collectors**
**Generated when**: Networking resources (services, ingresses) are found  
**What they collect**:
- Network connectivity tests
- DNS resolution checks
- Service reachability tests
- Load balancer health checks

**Example Output**:
```yaml
- runPod:
    name: auto-network-diag-production  
    namespace: production
    podSpec:
      containers:
      - name: diagnostic
        image: nicolaka/netshoot:latest
        command: ["sh", "-c", "nslookup kubernetes.default && curl -k https://kubernetes.default"]
```

---

## 🔍 **Practical Examples**

### **Foundation Path Example:**
```bash
support-bundle collect --auto --namespace "production,staging"
```
**Discovers**:
- 🔍 **67 pods** → 67 `logs` collectors
- 🔍 **12 services** → 1 `cluster-resources` + 2 `run-pod` network diagnostics  
- 🔍 **8 deployments** → 1 `cluster-resources` collector
- 🔍 **15 configmaps** → 1 `cluster-resources` collector
- 🔍 **5 secrets** → 1 `cluster-resources` collector (if RBAC allows)
- 🔍 **Events** → 1 `cluster-resources` collector
- **Total**: ~74 collectors, auto-prioritized

### **Enhanced YAML Path Example:**
```bash
support-bundle collect --auto --config comprehensive-config.yaml
```
**Foundation PLUS**:
- ➕ **Custom CRDs** (Istio, Prometheus, custom apps)
- ➕ **Extended log retention** (7 days instead of 3)  
- ➕ **Database-specific diagnostics** with exec collectors
- ➕ **Monitoring resource collection** (ServiceMonitors, PrometheusRules)
- ➖ **Excluded test/dev environments** 
- ➖ **Excluded sensitive secrets** by name patterns
- **Total**: ~120+ collectors, custom-prioritized

---

## 🎯 **Key Insights**

### **✅ Foundation is Comprehensive**
Even **without YAML**, you get:
- Complete application troubleshooting (pods, services, deployments)
- Kubernetes object specifications (configmaps, secrets, events)  
- Network diagnostics (connectivity, DNS, load balancer tests)
- Storage investigation (PVCs, storage classes)
- Batch job analysis (jobs, cronjobs)

### **🚀 YAML Adds Enterprise Features**
With YAML configuration:
- **Custom Resource Discovery** (CRDs, operators, service mesh)
- **Extended Data Collection** (longer logs, more commands, file copies)
- **Security-Aware Filtering** (exclude secrets by pattern, include only prod)
- **Conditional Logic** (collect X only if label Y exists)
- **Priority Customization** (make database logs highest priority)

### **🔄 Both Paths Share:**
- Same RBAC permission checking
- Same dependency resolution  
- Same namespace filtering
- Same collector specification format
- Same troubleshoot.sh integration

**This gives you powerful auto-discovery out-of-the-box, with enterprise customization when needed!** 🎯

Is this the architecture breakdown you were looking for?
