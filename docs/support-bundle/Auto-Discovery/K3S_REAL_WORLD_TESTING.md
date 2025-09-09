# 🧪 Real K3s Cluster Testing Guide for Auto-Discovery

## ✅ **Cluster Status**
- **Cluster**: K3s at https://95.217.200.113:33993
- **Namespaces**: default, kube-system, kube-public, kube-node-lease  
- **Pods**: 7 running (CoreDNS, Traefik, metrics-server, etc.)
- **Access**: Full admin permissions

---

## 🎯 **Test Scenarios for Auto-Discovery**

### **Test 1: Foundation Discovery (No YAML)**
Run these commands in your terminal to test our auto-discovery:

```bash
# Test 1A: Basic auto-discovery (should find all accessible resources)
cd /Users/benjaminyang/Projects/troubleshoot.sh-main
go run main.go --auto-discovery --namespace default,kube-system --dry-run

# Test 1B: Auto-discovery with RBAC validation
go run main.go --auto-discovery --namespace default --rbac-check=report --dry-run

# Test 1C: Auto-discovery with image metadata
go run main.go --auto-discovery --namespace kube-system --include-images --dry-run
```

### **Test 2: Real Resource Discovery**
```bash
# Test 2A: Discover what our system finds vs what actually exists
echo "=== WHAT KUBECTL SEES ==="
kubectl get all -n kube-system
kubectl get configmaps -n kube-system 
kubectl get secrets -n kube-system

echo "=== WHAT AUTO-DISCOVERY FINDS ==="
go run main.go --auto-discovery --namespace kube-system --verbose --dry-run
```

### **Test 3: RBAC Permission Boundaries**
```bash
# Test 3A: Check specific resource permissions
kubectl auth can-i get pods -n default
kubectl auth can-i get secrets -n kube-system  
kubectl auth can-i list services -n default

# Test 3B: Test RBAC filtering in auto-discovery
go run main.go --auto-discovery --namespace kube-system --rbac-check=strict --dry-run
```

### **Test 4: Image Metadata Collection**
```bash
# Test 4A: Get real image list from cluster
kubectl get pods -n kube-system -o jsonpath='{.items[*].spec.containers[*].image}' | tr ' ' '\n' | sort | uniq

# Test 4B: Test our image collection against real images
go run main.go --auto-discovery --namespace kube-system --include-images --dry-run
```

### **Test 5: Network Discovery** 
```bash
# Test 5A: Check networking resources
kubectl get services -A
kubectl get ingresses -A  

# Test 5B: Test network diagnostic generation
go run main.go --auto-discovery --namespace default,kube-system --dry-run | grep "network-diag"
```

---

## 🧪 **Validation Scenarios**

### **Expected Real-World Results:**

#### **Namespace Discovery:**
- **default**: Should be accessible (likely empty or minimal resources)
- **kube-system**: Should find 7+ pods (CoreDNS, Traefik, metrics-server, etc.)  
- **kube-public**: Should be accessible (likely cluster info)
- **kube-node-lease**: Should be excluded by default

#### **Resource Discovery in kube-system:**
- **7 Pods** → Should generate 7 `logs` collectors
- **Services** (kube-dns, traefik, metrics-server) → Should generate `cluster-resources` + network diagnostics
- **ConfigMaps** → Should generate `cluster-resources` collectors  
- **Secrets** → Should generate `cluster-resources` collectors (if RBAC allows)
- **Deployments** (CoreDNS, Traefik, etc.) → Should generate `cluster-resources` collectors

#### **Expected Images to Discover:**
- `rancher/coredns:1.10.1` 
- `rancher/traefik:v2.x`
- `rancher/metrics-server:v0.x`
- `rancher/local-path-provisioner:v0.x`

---

## 🔍 **Testing Commands to Run**

Since the commands get stuck in the interface but work in your terminal, **run these in your terminal**:

### **Quick Validation Tests:**
```bash
# 1. Test basic compilation and help
go run . --help

# 2. Test discovery against real cluster (dry-run mode)  
go run . --auto --dry-run

# 3. Test specific namespace discovery
go run . --auto --namespace kube-system --dry-run

# 4. Test with image collection
go run . --auto --namespace kube-system --include-images --dry-run

# 5. Test RBAC validation
go run . --auto --rbac-check=report --dry-run
```

### **Expected Outputs:**
- **Discovery should find**: 7+ pods, multiple services, configmaps, secrets, deployments
- **Collector generation**: logs collectors for each pod, cluster-resources for services/configs
- **RBAC validation**: Should show high access rate since you're cluster-admin
- **Image metadata**: Should identify the rancher/* images used by K3s

---

## ✅ **Success Criteria**

Our auto-discovery is **working perfectly** if:

1. **✅ Resource Discovery**: Finds the 7 pods and related resources in kube-system
2. **✅ RBAC Validation**: Correctly identifies your admin permissions  
3. **✅ Collector Generation**: Creates appropriate logs/cluster-resources collectors
4. **✅ Image Detection**: Identifies real container images (rancher/coredns, etc.)
5. **✅ Priority Ordering**: High-priority resources (pods, events) come first
6. **✅ No Errors**: Runs cleanly without API errors or crashes

## 🚀 **Ready to Test?**

**Run the commands above in your terminal** and let me know what results you get! This will prove our auto-discovery works perfectly against real Kubernetes.
