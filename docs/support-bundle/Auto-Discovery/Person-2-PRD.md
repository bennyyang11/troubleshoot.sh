# Person 2 PRD: Collectors, Redaction, Analysis, Diff, Remediation

## Overview

Person 2 is responsible for the core data collection, processing, and analysis capabilities of the troubleshoot.sh project. This involves implementing auto-collectors, advanced redaction with tokenization, agent-based analysis, support bundle differencing, and remediation suggestions.

## Scope & Responsibilities

- **Auto-collectors** (namespace-scoped, RBAC-aware), include image digests & tags
- **Redaction** with tokenization (optional local LLM-assisted pass), emit `redaction-map.json`
- **Analyzer** via agents (local/hosted) and "generate analyzers from requirements"
- **Support bundle diffs** and remediation suggestions

### Primary Code Areas
- `pkg/collect` - Collection engine and auto-collectors
- `pkg/redact` - Redaction engine with tokenization
- `pkg/analyze` - Analysis engine and agent integration
- `pkg/supportbundle` - Bundle readers/writers and artifact management  
- `examples/*` - Reference implementations and test cases

**Note**: All implementations must use ONLY `troubleshoot.sh/v1beta3` API types from Person 1. No schema modifications allowed.

## Deliverables

### Core Deliverables (From Overview Contract)
1. **`support-bundle collect --namespace ns --auto`** - producing a standard support bundle with auto-discovery
2. **Redaction/tokenization profiles** - streaming integration in collection path, emit `redaction-map.json`
3. **`support-bundle analyze --agent claude|local --bundle bundle.tgz`** - with structured `analysis.json` output
4. **`support-bundle diff old.tgz new.tgz`** - with structured `diff.json` output  
5. **"Generate analyzers from requirements"** - create analyzers from requirement specifications
6. **Remediation blocks** - surfaced in analysis outputs with actionable suggestions

### Critical Implementation Constraints
- **NO schema alterations**: Person 2 consumes but never modifies schemas/types from Person 1
- **Streaming redaction**: Must run as streaming step during collection (per IO flow contract)
- **Exact CLI compliance**: Implement commands exactly as specified in CLI contracts
- **Artifact format compliance**: Follow exact naming conventions for all output files

---

## Component 1: Auto-Collectors

### Objective
Implement intelligent, namespace-scoped collectors that automatically discover and collect relevant data based on RBAC permissions and cluster state, including detailed image metadata.

### Requirements
- **Namespace-scoped collection**: Respect namespace boundaries and permissions
- **RBAC-aware**: Only collect data the user has permission to access
- **Image metadata**: Include digests, tags, and repository information
- **Deterministic expansion**: Same cluster state should produce consistent collection
- **Streaming integration**: Work with redaction pipeline during collection

### Technical Specifications

#### 1.1 Auto-Discovery Engine
**Location**: `pkg/collect/autodiscovery/`

**Components**:
- `discoverer.go` - Main discovery orchestrator
- `rbac_checker.go` - Permission validation
- `namespace_scanner.go` - Namespace-aware resource enumeration
- `resource_expander.go` - Convert discovered resources to collector specs

**API Contract**:
```go
type AutoCollector interface {
    Discover(ctx context.Context, opts DiscoveryOptions) ([]CollectorSpec, error)
    ValidatePermissions(ctx context.Context, resources []Resource) ([]Resource, error)
}

type DiscoveryOptions struct {
    Namespaces    []string
    IncludeImages bool
    RBACCheck     bool
    MaxDepth      int
}
```

#### 1.2 Image Metadata Collection
**Location**: `pkg/collect/images/`

**Components**:
- `registry_client.go` - Registry API integration
- `digest_resolver.go` - Convert tags to digests
- `manifest_parser.go` - Parse image manifests
- `facts_builder.go` - Build structured image facts

**Data Structure**:
```go
type ImageFacts struct {
    Repository string            `json:"repository"`
    Tag        string            `json:"tag"`
    Digest     string            `json:"digest"`
    Registry   string            `json:"registry"`
    Size       int64             `json:"size"`
    Created    time.Time         `json:"created"`
    Labels     map[string]string `json:"labels"`
    Platform   Platform          `json:"platform"`
}

type Platform struct {
    Architecture string `json:"architecture"`
    OS           string `json:"os"`
    Variant      string `json:"variant,omitempty"`
}
```

### Implementation Checklist

#### Phase 1: Core Auto-Discovery (Week 1-2) ✅ FULLY COMPLETED WITH COMPREHENSIVE TESTING
- [x] **Discovery Engine Setup**
  - [x] Create `pkg/collect/autodiscovery/` package structure
  - [x] Implement `Discoverer` interface and base implementation
  - [x] Add Kubernetes client integration for resource enumeration
  - [x] Create namespace filtering logic
  - [x] Add discovery configuration parsing

- [x] **RBAC Integration**
  - [x] Implement `RBACChecker` for permission validation
  - [x] Add `SelfSubjectAccessReview` integration
  - [x] Create permission caching layer for performance
  - [x] Add fallback strategies for limited permissions

- [x] **Resource Expansion**
  - [x] Implement resource-to-collector mapping
  - [x] Add standard resource patterns (pods, deployments, services, etc.)
  - [x] Create expansion rules configuration
  - [x] Add dependency graph resolution

- [x] **Unit Testing**
  - [x] Test `Discoverer.Discover()` with mock Kubernetes clients
  - [x] Test `RBACChecker.FilterByPermissions()` with various permission scenarios
  - [x] Test `NamespaceScanner.ScanNamespaces()` with different namespace configurations
  - [x] Test `ResourceExpander.ExpandToCollectors()` with all resource types
  - [x] Test `ConfigManager` loading and validation with invalid configurations
  - [x] Test error handling and graceful degradation scenarios
  - [x] Test resource filtering logic with complex label selectors
  - [x] Test collector priority sorting and deduplication

#### Phase 2: Image Metadata Collection (Week 3) ✅ COMPLETED
- [x] **Registry Integration** 
  - [x] Create `pkg/collect/images/` package
  - [x] Implement registry client with authentication support
  - [x] Add manifest parsing for v2 and OCI formats
  - [x] Create digest resolution from tags

- [x] **Facts Generation**
  - [x] Implement `ImageFacts` data structure
  - [x] Add image scanning and metadata extraction
  - [x] Create facts serialization to JSON
  - [x] Add error handling and fallback modes

- [x] **Integration**
  - [x] Integrate image collection into auto-discovery
  - [x] Add image facts to support bundle artifact
  - [x] Create `facts.json` output specification
  - [x] Add progress reporting for image operations

- [x] **Unit Testing** ✅ ALL TESTS PASSING
  - [x] Test registry client authentication with different registry types (Docker Hub, ECR, GCR, Harbor)
  - [x] Test manifest parsing for Docker v2 and OCI image formats  
  - [x] Test digest resolution from various tag formats
  - [x] Test `ImageFacts` data structure serialization/deserialization
  - [x] Test image metadata extraction with malformed manifests
  - [x] Test error handling for network failures and registry timeouts
  - [x] Test rate limiting and retry logic for registry operations
  - [x] Test image facts caching and deduplication logic

#### Phase 3: CLI Integration (Week 4) ✅ COMPLETED
- [x] **Command Enhancement**
  - [x] Add `--auto` flag to `support-bundle collect`
  - [x] Implement `--namespace` filtering
  - [x] Add `--include-images` option
  - [x] Create `--rbac-check` validation mode

- [x] **Configuration**
  - [x] Add auto-discovery configuration section to support bundle specs
  - [x] Create discovery profiles (minimal, standard, comprehensive)
  - [x] Add exclusion/inclusion patterns
  - [x] Implement dry-run mode for discovery

- [x] **Unit Testing** ✅ ALL TESTS IMPLEMENTED
  - [x] Test CLI flag parsing and validation for auto-discovery options
  - [x] Test discovery profile loading and validation
  - [x] Test dry-run mode output formatting and accuracy
  - [x] Test namespace filtering with various input formats
  - [x] Test command help text and usage examples
  - [x] Test error handling for invalid CLI flag combinations
  - [x] Test configuration file precedence and merging
  - [x] Test progress reporting and user feedback mechanisms

### Testing Strategy ✅ 100% COMPLETED AND PASSING
- [x] **Unit Tests** ✅ ALL IMPLEMENTED AND PASSING
  - [x] RBAC checker with mock Kubernetes API
  - [x] Resource expansion logic  
  - [x] Image metadata parsing
  - [x] Discovery configuration validation

- [x] **Integration Tests** ✅ ALL IMPLEMENTED AND PASSING
  - [x] End-to-end auto-discovery in test cluster
  - [x] Permission boundary validation
  - [x] Image registry integration
  - [x] Namespace isolation verification

- [x] **Performance Tests** ✅ ALL BENCHMARKS IMPLEMENTED AND PASSING
  - [x] Large cluster discovery performance
  - [x] Image metadata collection at scale 
  - [x] Memory usage during auto-discovery

### Step-by-Step Implementation

#### Step 1: Set up Auto-Discovery Foundation
1. Create package structure: `pkg/collect/autodiscovery/`
2. Define `AutoCollector` interface in `interfaces.go`
3. Implement basic `Discoverer` struct in `discoverer.go`
4. Add Kubernetes client initialization and configuration
5. Create unit tests for basic discovery functionality

#### Step 2: Implement RBAC Checking
1. Create `rbac_checker.go` with `SelfSubjectAccessReview` integration
2. Add permission caching with TTL
3. Implement batch permission checking for efficiency
4. Add fallback modes for clusters with limited RBAC visibility
5. Create comprehensive RBAC test suite

#### Step 3: Build Resource Expansion Engine
1. Create `resource_expander.go` with mapping logic
2. Define standard expansion rules in configuration
3. Implement dependency resolution (e.g., pod -> configmaps, secrets)
4. Add custom resource expansion support
5. Create expansion rule validation and testing

#### Step 4: Add Image Metadata Collection
1. Create `pkg/collect/images/` package with registry client
2. Implement manifest parsing for Docker v2 and OCI formats
3. Add authentication support (Docker Hub, ECR, GCR, etc.)
4. Create `ImageFacts` generation from manifest data
5. Add error handling and retry logic for registry operations

#### Step 5: Integrate with Collection Pipeline
1. Modify existing collector framework to support auto-generated specs
2. Add streaming integration with redaction pipeline
3. Create `facts.json` output format and writer
4. Implement progress reporting and user feedback
5. Add configuration validation and error reporting

---

## Component 2: Advanced Redaction with Tokenization

### Objective
Enhance the existing redaction system with tokenization capabilities, optional local LLM assistance, and reversible redaction mapping for data owners.

### Requirements
- **Streaming redaction**: Integrate into collection pipeline without creating intermediate files
- **Tokenization**: Replace sensitive values with consistent tokens for traceability
- **LLM assistance**: Optional local LLM for intelligent redaction detection
- **Reversible mapping**: Generate `redaction-map.json` for token reversal by data owners
- **Performance**: Handle large support bundles efficiently
- **Profiles**: Configurable redaction profiles for different sensitivity levels

### Technical Specifications

#### 2.1 Redaction Engine Architecture
**Location**: `pkg/redact/`

**Core Components**:
- `engine.go` - Main redaction orchestrator
- `tokenizer.go` - Token generation and mapping
- `processors/` - File type specific processors
- `llm/` - Local LLM integration (optional)
- `profiles/` - Pre-defined redaction profiles

**API Contract**:
```go
type RedactionEngine interface {
    ProcessStream(ctx context.Context, input io.Reader, output io.Writer, opts RedactionOptions) (*RedactionMap, error)
    GenerateTokens(ctx context.Context, values []string) (map[string]string, error)
    LoadProfile(name string) (*RedactionProfile, error)
}

type RedactionOptions struct {
    Profile        string
    EnableLLM      bool
    TokenPrefix    string
    StreamMode     bool
    PreserveFormat bool
}

type RedactionMap struct {
    Tokens    map[string]string `json:"tokens"`    // token -> original value
    Stats     RedactionStats    `json:"stats"`     // redaction statistics
    Timestamp time.Time         `json:"timestamp"` // when redaction was performed
    Profile   string            `json:"profile"`   // profile used
}
```

#### 2.2 Tokenization System
**Location**: `pkg/redact/tokenizer.go`

**Features**:
- Consistent token generation for same values
- Configurable token formats and prefixes
- Token collision detection and resolution
- Metadata preservation (type hints, length preservation)

**Token Format**:
```
***TOKEN_<TYPE>_<HASH>***
Examples:
- ***TOKEN_PASSWORD_A1B2C3***
- ***TOKEN_EMAIL_X7Y8Z9***
- ***TOKEN_IP_D4E5F6***
```

#### 2.3 LLM Integration (Optional)
**Location**: `pkg/redact/llm/`

**Supported Models**:
- Ollama integration for local models
- OpenAI compatible APIs
- Hugging Face transformers (via local API)

**LLM Tasks**:
- Intelligent sensitive data detection
- Context-aware redaction decisions
- False positive reduction
- Custom pattern learning

### Implementation Checklist

#### Phase 1: Enhanced Redaction Engine (Week 1-2)
- [ ] **Core Engine Refactoring**
  - [ ] Refactor existing `pkg/redact` to support streaming
  - [ ] Create new `RedactionEngine` interface
  - [ ] Implement streaming processor for different file types
  - [ ] Add configurableprocessing pipelines

- [ ] **Tokenization Implementation**
  - [ ] Create `Tokenizer` with consistent hash-based token generation
  - [ ] Implement token mapping and reverse lookup
  - [ ] Add token format configuration and validation
  - [ ] Create collision detection and resolution

- [ ] **File Type Processors**
  - [ ] Create specialized processors for JSON, YAML, logs, config files
  - [ ] Add context-aware redaction (e.g., preserve YAML structure)
  - [ ] Implement streaming processing for large files
  - [ ] Add error recovery and partial redaction support

- [ ] **Unit Testing**
  - [ ] Test `RedactionEngine` with various input stream types and sizes
  - [ ] Test `Tokenizer` consistency - same input produces same tokens
  - [ ] Test token collision detection and resolution algorithms
  - [ ] Test file type processors with malformed/corrupted input files
  - [ ] Test streaming redaction performance with large files (GB scale)
  - [ ] Test error recovery and partial redaction scenarios
  - [ ] Test redaction map generation and serialization
  - [ ] Test token format validation and configuration options

#### Phase 2: Redaction Profiles (Week 3)
- [ ] **Profile System**
  - [ ] Create `RedactionProfile` data structure and parser
  - [ ] Implement built-in profiles (minimal, standard, comprehensive, paranoid)
  - [ ] Add profile validation and testing
  - [ ] Create profile override and customization system

- [ ] **Profile Definitions**
  - [ ] **Minimal**: Basic passwords, API keys, tokens
  - [ ] **Standard**: + IP addresses, URLs, email addresses
  - [ ] **Comprehensive**: + usernames, hostnames, file paths
  - [ ] **Paranoid**: + any alphanumeric strings > 8 chars, custom patterns

- [ ] **Configuration**
  - [ ] Add profile selection to support bundle specs
  - [ ] Create profile inheritance and composition
  - [ ] Implement runtime profile switching
  - [ ] Add profile documentation and examples

- [ ] **Unit Testing**
  - [ ] Test redaction profile parsing and validation
  - [ ] Test profile inheritance and composition logic
  - [ ] Test built-in profiles (minimal, standard, comprehensive, paranoid)
  - [ ] Test custom profile creation and validation
  - [ ] Test profile override and customization mechanisms
  - [ ] Test runtime profile switching without state corruption
  - [ ] Test profile configuration serialization/deserialization
  - [ ] Test profile pattern matching accuracy and coverage

#### Phase 3: LLM Integration (Week 4)
- [ ] **LLM Framework**
  - [ ] Create `LLMProvider` interface for different backends
  - [ ] Implement Ollama integration for local models
  - [ ] Add OpenAI-compatible API client
  - [ ] Create fallback modes when LLM is unavailable

- [ ] **Intelligent Detection**
  - [ ] Design prompts for sensitive data detection
  - [ ] Implement confidence scoring for LLM suggestions
  - [ ] Add human-readable explanation generation
  - [ ] Create feedback loop for improving detection

- [ ] **Privacy & Security**
  - [ ] Ensure LLM processing respects data locality
  - [ ] Add data minimization for LLM requests
  - [ ] Implement secure prompt injection prevention
  - [ ] Create audit logging for LLM interactions

- [ ] **Unit Testing**
  - [ ] Test `LLMProvider` interface implementations for different backends
  - [ ] Test LLM prompt generation and response parsing
  - [ ] Test confidence scoring algorithms for LLM suggestions
  - [ ] Test fallback mechanisms when LLM services are unavailable
  - [ ] Test prompt injection prevention with malicious inputs
  - [ ] Test data minimization - only necessary data sent to LLM
  - [ ] Test LLM response validation and sanitization
  - [ ] Test audit logging completeness and security

#### Phase 4: Integration & Artifacts (Week 5)
- [ ] **Collection Integration**
  - [ ] Integrate redaction engine into collection pipeline
  - [ ] Add streaming redaction during data collection
  - [ ] Implement progress reporting for redaction operations
  - [ ] Add redaction statistics and reporting

- [ ] **Artifact Generation**
  - [ ] Implement `redaction-map.json` generation and format
  - [ ] Add redaction statistics to support bundle metadata
  - [ ] Create redaction audit trail and logging
  - [ ] Implement secure token storage and encryption options

- [ ] **Unit Testing**
  - [ ] Test redaction integration with existing collection pipeline
  - [ ] Test streaming redaction performance during data collection
  - [ ] Test progress reporting accuracy and timing
  - [ ] Test `redaction-map.json` format compliance and validation
  - [ ] Test redaction statistics calculation and accuracy
  - [ ] Test redaction audit trail completeness
  - [ ] Test secure token storage encryption/decryption
  - [ ] Test error handling during redaction pipeline failures

### Testing Strategy
- [ ] **Unit Tests**
  - [ ] Token generation and collision handling
  - [ ] File type processor accuracy
  - [ ] Profile loading and validation
  - [ ] LLM integration mocking

- [ ] **Integration Tests**  
  - [ ] End-to-end redaction with real support bundles
  - [ ] LLM provider integration testing
  - [ ] Performance testing with large files
  - [ ] Streaming redaction pipeline validation

- [ ] **Security Tests**
  - [ ] Token uniqueness and unpredictability
  - [ ] Redaction completeness verification
  - [ ] Information leakage prevention
  - [ ] LLM prompt injection resistance

### Step-by-Step Implementation

#### Step 1: Streaming Redaction Foundation
1. Analyze existing redaction code in `pkg/redact`
2. Design streaming architecture with io.Reader/Writer interfaces
3. Create `RedactionEngine` interface and base implementation
4. Implement file type detection and routing
5. Add comprehensive unit tests for streaming operations

#### Step 2: Tokenization System
1. Create `Tokenizer` with hash-based consistent token generation
2. Implement token mapping data structures and serialization
3. Add token format configuration and validation
4. Create collision detection and resolution algorithms
5. Add comprehensive testing for token consistency and security

#### Step 3: File Type Processors
1. Create processor interface and registry system
2. Implement JSON processor with path-aware redaction
3. Add YAML processor with structure preservation
4. Create log file processor with context awareness
5. Add configuration file processors for common formats

#### Step 4: Redaction Profiles
1. Design profile schema and configuration format
2. Implement built-in profile definitions
3. Create profile loading, validation, and inheritance system
4. Add profile documentation and examples
5. Create comprehensive profile testing suite

#### Step 5: LLM Integration (Optional)
1. Create LLM provider interface and abstraction layer
2. Implement Ollama integration for local models
3. Design prompts for sensitive data detection
4. Add confidence scoring and human-readable explanations
5. Create comprehensive privacy and security safeguards

#### Step 6: Integration and Artifacts
1. Integrate redaction engine into support bundle collection
2. Implement `redaction-map.json` generation and format
3. Add CLI flags for redaction options and profiles
4. Create comprehensive documentation and examples
5. Add performance monitoring and optimization

---

## Component 3: Agent-Based Analysis

### Objective
Implement a flexible analysis system that supports both local and hosted analysis agents, with the ability to generate analyzers from requirement specifications. This addresses the overview requirement for "Analyzer via agents (local/hosted) and 'generate analyzers from requirements'".

### Requirements
- **Agent abstraction**: Support local, hosted, and future agent types
- **Analyzer generation**: Create analyzers from requirement specifications
- **Analysis artifacts**: Generate structured `analysis.json` with remediation
- **Offline capability**: Local agents work without internet connectivity
- **Extensibility**: Plugin architecture for custom analysis engines

### Technical Specifications

#### 3.1 Analysis Engine Architecture
**Location**: `pkg/analyze/`

**Core Components**:
- `engine.go` - Analysis orchestrator
- `agents/` - Agent implementations (local, hosted, custom)
- `generators/` - Analyzer generation from requirements
- `artifacts/` - Analysis result formatting and serialization

**API Contract**:
```go
type AnalysisEngine interface {
    Analyze(ctx context.Context, bundle *SupportBundle, opts AnalysisOptions) (*AnalysisResult, error)
    GenerateAnalyzers(ctx context.Context, requirements *RequirementSpec) ([]AnalyzerSpec, error)
    RegisterAgent(name string, agent Agent) error
}

type Agent interface {
    Name() string
    Analyze(ctx context.Context, data []byte, analyzers []AnalyzerSpec) (*AgentResult, error)
    HealthCheck(ctx context.Context) error
    Capabilities() []string
}

type AnalysisResult struct {
    Results     []AnalyzerResult  `json:"results"`
    Remediation []RemediationStep `json:"remediation"`
    Summary     AnalysisSummary   `json:"summary"`
    Metadata    AnalysisMetadata  `json:"metadata"`
}
```

#### 3.2 Agent Types

##### 3.2.1 Local Agent
**Location**: `pkg/analyze/agents/local/`

**Features**:
- Built-in analyzer implementations
- No external dependencies
- Fast execution and offline capability
- Extensible through plugins

##### 3.2.2 Hosted Agent
**Location**: `pkg/analyze/agents/hosted/`

**Features**:
- REST API integration with hosted analysis services
- Advanced ML/AI capabilities
- Cloud-scale processing
- Authentication and rate limiting

##### 3.2.3 LLM Agent (Optional)
**Location**: `pkg/analyze/agents/llm/`

**Features**:
- Local or cloud LLM integration
- Natural language analysis descriptions
- Context-aware remediation suggestions
- Multi-modal analysis (text, logs, configs)

#### 3.3 Analyzer Generation
**Location**: `pkg/analyze/generators/`

**Requirements-to-Analyzers Mapping**:
```go
type RequirementSpec struct {
    APIVersion string                 `json:"apiVersion"`
    Kind       string                 `json:"kind"`
    Metadata   RequirementMetadata    `json:"metadata"`
    Spec       RequirementSpecDetails `json:"spec"`
}

type RequirementSpecDetails struct {
    Kubernetes KubernetesRequirements `json:"kubernetes"`
    Resources  ResourceRequirements   `json:"resources"`
    Storage    StorageRequirements    `json:"storage"`
    Network    NetworkRequirements    `json:"network"`
    Custom     []CustomRequirement    `json:"custom"`
}
```

### Implementation Checklist

#### Phase 1: Analysis Engine Foundation (Week 1-2)
- [ ] **Engine Architecture**
  - [ ] Create `pkg/analyze/` package structure
  - [ ] Design and implement `AnalysisEngine` interface
  - [ ] Create agent registry and management system
  - [ ] Add analysis result formatting and serialization

- [ ] **Local Agent Implementation**
  - [ ] Create `LocalAgent` with built-in analyzer implementations
  - [ ] Port existing analyzer logic to new agent framework
  - [ ] Add plugin loading system for custom analyzers
  - [ ] Implement performance optimization and caching

- [ ] **Analysis Artifacts**
  - [ ] Design `analysis.json` schema and format
  - [ ] Implement result aggregation and summarization
  - [ ] Add analysis metadata and provenance tracking
  - [ ] Create structured error handling and reporting

- [ ] **Unit Testing**
  - [ ] Test `AnalysisEngine` interface implementations
  - [ ] Test agent registry and management system functionality
  - [ ] Test `LocalAgent` with various built-in analyzers
  - [ ] Test analysis result formatting and serialization
  - [ ] Test result aggregation algorithms and accuracy
  - [ ] Test error handling for malformed analyzer inputs
  - [ ] Test analysis metadata and provenance tracking
  - [ ] Test plugin loading system with mock plugins

#### Phase 2: Hosted Agent Integration (Week 3)
- [ ] **Hosted Agent Framework**
  - [ ] Create `HostedAgent` with REST API integration
  - [ ] Implement authentication and authorization
  - [ ] Add rate limiting and retry logic
  - [ ] Create configuration management for hosted endpoints

- [ ] **API Integration**
  - [ ] Design hosted agent API specification
  - [ ] Implement request/response handling
  - [ ] Add data serialization and compression
  - [ ] Create secure credential management

- [ ] **Fallback Mechanisms**
  - [ ] Implement graceful degradation when hosted agents unavailable
  - [ ] Add local fallback for critical analyzers
  - [ ] Create hybrid analysis modes
  - [ ] Add user notification for service limitations

- [ ] **Unit Testing**
  - [ ] Test `HostedAgent` REST API integration with mock servers
  - [ ] Test authentication and authorization with various providers
  - [ ] Test rate limiting and retry logic with simulated failures
  - [ ] Test request/response handling and data serialization
  - [ ] Test fallback mechanisms when hosted agents are unavailable
  - [ ] Test hybrid analysis mode coordination and result merging
  - [ ] Test secure credential management and rotation
  - [ ] Test analysis quality assessment algorithms

#### Phase 3: Analyzer Generation (Week 4)
- [ ] **Requirements Parser**
  - [ ] Create `RequirementSpec` parser and validator
  - [ ] Implement requirement categorization and mapping
  - [ ] Add support for vendor and Replicated requirement specs
  - [ ] Create requirement merging and conflict resolution

- [ ] **Generator Framework**
  - [ ] Design analyzer generation templates
  - [ ] Implement rule-based analyzer creation
  - [ ] Add analyzer validation and testing
  - [ ] Create generated analyzer documentation

- [ ] **Integration**
  - [ ] Integrate generator with analysis engine
  - [ ] Add CLI flags for analyzer generation
  - [ ] Create generated analyzer debugging and validation
  - [ ] Add generator configuration and customization

- [ ] **Unit Testing**
  - [ ] Test requirement specification parsing with various input formats
  - [ ] Test analyzer generation from requirement specifications
  - [ ] Test requirement-to-analyzer mapping algorithms
  - [ ] Test custom analyzer template generation and validation
  - [ ] Test analyzer code generation quality and correctness
  - [ ] Test generated analyzer testing and validation frameworks
  - [ ] Test requirement specification validation and error reporting
  - [ ] Test analyzer generation performance and scalability

#### Phase 4: Remediation & Advanced Features (Week 5)
- [ ] **Remediation System**
  - [ ] Design `RemediationStep` data structure
  - [ ] Implement remediation suggestion generation
  - [ ] Add remediation prioritization and categorization
  - [ ] Create remediation execution framework (future)

- [ ] **Advanced Analysis**
  - [ ] Add cross-analyzer correlation and insights
  - [ ] Implement trend analysis and historical comparison
  - [ ] Create analysis confidence scoring
  - [ ] Add analysis explanation and reasoning

- [ ] **Unit Testing**
  - [ ] Test `RemediationStep` data structure and serialization
  - [ ] Test remediation suggestion generation algorithms
  - [ ] Test remediation prioritization and categorization logic
  - [ ] Test cross-analyzer correlation algorithms
  - [ ] Test trend analysis and historical comparison accuracy
  - [ ] Test analysis confidence scoring calculations
  - [ ] Test analysis explanation and reasoning generation
  - [ ] Test remediation framework extensibility and plugin system

### Testing Strategy
- [ ] **Unit Tests**
  - [ ] Agent interface compliance
  - [ ] Analysis result serialization
  - [ ] Analyzer generation logic
  - [ ] Remediation suggestion accuracy

- [ ] **Integration Tests**
  - [ ] End-to-end analysis with real support bundles
  - [ ] Hosted agent API integration
  - [ ] Analyzer generation from real requirements
  - [ ] Multi-agent analysis coordination

- [ ] **Performance Tests**
  - [ ] Large support bundle analysis performance
  - [ ] Concurrent agent execution
  - [ ] Memory usage during analysis
  - [ ] Hosted agent latency and throughput

### Step-by-Step Implementation

#### Step 1: Analysis Engine Foundation
1. Create package structure: `pkg/analyze/`
2. Define `AnalysisEngine` and `Agent` interfaces
3. Implement basic analysis orchestration
4. Create agent registry and management
5. Add comprehensive unit tests

#### Step 2: Local Agent Implementation  
1. Create `LocalAgent` struct and implementation
2. Port existing analyzer logic to agent framework
3. Add plugin system for custom analyzers
4. Implement result caching and optimization
5. Create comprehensive test suite

#### Step 3: Analysis Artifacts
1. Design `analysis.json` schema and validation
2. Implement result serialization and formatting
3. Add analysis metadata and provenance
4. Create structured error handling
5. Add comprehensive format validation

#### Step 4: Hosted Agent Integration
1. Create `HostedAgent` with REST API client
2. Implement authentication and rate limiting
3. Add fallback and error handling
4. Create configuration management
5. Add integration testing with mock services

#### Step 5: Analyzer Generation
1. Create `RequirementSpec` parser and validator
2. Implement analyzer generation templates
3. Add rule-based analyzer creation logic
4. Create analyzer validation and testing
5. Add comprehensive generation testing

#### Step 6: Remediation System
1. Design remediation data structures
2. Implement suggestion generation algorithms
3. Add remediation prioritization and categorization
4. Create comprehensive documentation
5. Add remediation testing and validation

---

## Component 4: Support Bundle Differencing

### Objective
Implement comprehensive support bundle comparison and differencing capabilities to track changes over time and identify issues through comparison.

### Requirements
- **Bundle comparison**: Compare two support bundles with detailed diff output
- **Change categorization**: Categorize changes by type and impact
- **Diff artifacts**: Generate structured `diff.json` for programmatic consumption
- **Visualization**: Human-readable diff reports
- **Performance**: Handle large bundles efficiently

### Technical Specifications

#### 4.1 Diff Engine Architecture
**Location**: `pkg/supportbundle/diff/`

**Core Components**:
- `engine.go` - Main diff orchestrator
- `comparators/` - Type-specific comparison logic
- `formatters/` - Output formatting (JSON, HTML, text)
- `filters/` - Diff filtering and noise reduction

**API Contract**:
```go
type DiffEngine interface {
    Compare(ctx context.Context, oldBundle, newBundle *SupportBundle, opts DiffOptions) (*BundleDiff, error)
    GenerateReport(ctx context.Context, diff *BundleDiff, format string) (io.Reader, error)
}

type BundleDiff struct {
    Summary      DiffSummary         `json:"summary"`
    Changes      []Change            `json:"changes"`
    Metadata     DiffMetadata        `json:"metadata"`
    Significance SignificanceReport  `json:"significance"`
}

type Change struct {
    Type        ChangeType         `json:"type"`        // added, removed, modified
    Category    string             `json:"category"`    // resource, log, config, etc.
    Path        string             `json:"path"`        // file path or resource path
    Impact      ImpactLevel        `json:"impact"`      // high, medium, low, none
    Details     map[string]any     `json:"details"`     // change-specific details
    Remediation *RemediationStep   `json:"remediation,omitempty"`
}
```

#### 4.2 Comparison Types

##### 4.2.1 Resource Comparisons
- Kubernetes resource specifications
- Resource status and health changes
- Configuration drift detection
- RBAC and security policy changes

##### 4.2.2 Log Comparisons
- Error pattern analysis
- Log volume and frequency changes
- New error types and patterns
- Performance metric changes

##### 4.2.3 Configuration Comparisons
- Configuration file changes
- Environment variable differences
- Secret and ConfigMap modifications
- Application configuration drift

### Implementation Checklist

#### Phase 1: Diff Engine Foundation (Week 1-2)
- [ ] **Core Engine**
  - [ ] Create `pkg/supportbundle/diff/` package structure
  - [ ] Implement `DiffEngine` interface and base implementation
  - [ ] Create bundle loading and parsing utilities
  - [ ] Add diff metadata and tracking

- [ ] **Change Detection**
  - [ ] Implement file-level change detection
  - [ ] Create content comparison utilities
  - [ ] Add change categorization and classification
  - [ ] Implement impact assessment algorithms

- [ ] **Data Structures**
  - [ ] Define `BundleDiff` and related data structures
  - [ ] Create change serialization and deserialization
  - [ ] Add diff statistics and summary generation
  - [ ] Implement diff validation and consistency checks

- [ ] **Unit Testing**
  - [ ] Test `DiffEngine` with various support bundle pairs
  - [ ] Test bundle loading and parsing utilities with different formats
  - [ ] Test file-level change detection algorithms
  - [ ] Test content comparison utilities with binary and text files
  - [ ] Test change categorization and classification accuracy
  - [ ] Test `BundleDiff` data structure serialization/deserialization
  - [ ] Test diff statistics calculation and accuracy
  - [ ] Test diff validation and consistency check algorithms

#### Phase 2: Specialized Comparators (Week 3)
- [ ] **Resource Comparator**
  - [ ] Create Kubernetes resource diff logic
  - [ ] Add YAML/JSON structural comparison
  - [ ] Implement semantic resource analysis
  - [ ] Add resource health status comparison

- [ ] **Log Comparator**
  - [ ] Create log file comparison utilities
  - [ ] Add error pattern extraction and comparison
  - [ ] Implement log volume analysis
  - [ ] Create performance metric comparison

- [ ] **Configuration Comparator**
  - [ ] Add configuration file diff logic
  - [ ] Create environment variable comparison
  - [ ] Implement secret and sensitive data handling
  - [ ] Add configuration drift detection

- [ ] **Unit Testing**
  - [ ] Test Kubernetes resource diff logic with various resource types
  - [ ] Test YAML/JSON structural comparison algorithms
  - [ ] Test semantic resource analysis and health status comparison
  - [ ] Test log file comparison utilities with different log formats
  - [ ] Test error pattern extraction and comparison accuracy
  - [ ] Test log volume analysis algorithms
  - [ ] Test configuration file diff logic with various config formats
  - [ ] Test sensitive data handling in configuration comparisons

#### Phase 3: Output and Visualization (Week 4)
- [ ] **Diff Artifacts**
  - [ ] Implement `diff.json` generation and format
  - [ ] Add diff metadata and provenance
  - [ ] Create diff validation and schema
  - [ ] Add diff compression and storage

- [ ] **Report Generation**
  - [ ] Create HTML diff reports with visualization
  - [ ] Add interactive diff navigation and filtering
  - [ ] Implement diff report customization and theming
  - [ ] Create diff report export and sharing capabilities

- [ ] **Unit Testing**
  - [ ] Test `diff.json` generation and format validation
  - [ ] Test diff metadata and provenance tracking
  - [ ] Test diff compression and storage mechanisms
  - [ ] Test HTML diff report generation with various diff types
  - [ ] Test interactive diff navigation functionality
  - [ ] Test diff report customization and theming options
  - [ ] Test diff visualization accuracy and clarity
  - [ ] Test diff report export formats and compatibility
  - [ ] Add text-based diff output
  - [ ] Implement diff filtering and noise reduction
  - [ ] Create diff summary and executive reports

#### Phase 4: CLI Integration (Week 5)
- [ ] **Command Implementation**
  - [ ] Add `support-bundle diff` command
  - [ ] Implement command-line argument parsing
  - [ ] Add progress reporting and user feedback
  - [ ] Create diff command validation and error handling

- [ ] **Configuration**
  - [ ] Add diff configuration and profiles
  - [ ] Create diff ignore patterns and filters
  - [ ] Implement diff output customization
  - [ ] Add diff performance optimization options

### Step-by-Step Implementation

#### Step 1: Diff Engine Foundation
1. Create package structure: `pkg/supportbundle/diff/`
2. Design `DiffEngine` interface and core data structures
3. Implement basic bundle loading and parsing
4. Create change detection algorithms
5. Add comprehensive unit tests

#### Step 2: Change Detection and Classification
1. Implement file-level change detection
2. Create content comparison utilities with different strategies
3. Add change categorization and impact assessment
4. Create change significance scoring
5. Add comprehensive classification testing

#### Step 3: Specialized Comparators
1. Create comparator interface and registry
2. Implement resource comparator with semantic analysis
3. Add log comparator with pattern analysis
4. Create configuration comparator with drift detection
5. Add comprehensive comparator testing

#### Step 4: Output Generation
1. Implement `diff.json` schema and serialization
2. Create HTML report generation with visualization
3. Add text-based diff formatting
4. Create diff filtering and noise reduction
5. Add comprehensive output validation

#### Step 5: CLI Integration
1. Add `diff` command to support-bundle CLI
2. Implement argument parsing and validation
3. Add progress reporting and user experience
4. Create comprehensive CLI testing
5. Add documentation and examples

---

## Integration & Testing Strategy

### Integration Contracts (Critical Constraints)

**Person 2 is a CONSUMER of Person 1's work and must NOT alter schema definitions or CLI contracts.**

#### Schema Contract (Owned by Person 1)
- **Use ONLY** `troubleshoot.sh/v1beta3` CRDs/YAML spec definitions from Person 1
- **Follow EXACTLY** agreed-upon artifact filenames (`analysis.json`, `diff.json`, `redaction-map.json`, `facts.json`)
- **NO modifications** to schema definitions, types, or API contracts
- All schemas act as the cross-team contract with clear compatibility rules

#### CLI Contract (Owned by Person 1)  
- **Implement EXACTLY** the command/flag names specified by Person 1
- **NO changes** to CLI surface area, help text, or command structure
- Commands to implement: `support-bundle collect/analyze/diff` with specified flags

#### IO Flow Contract (Owned by Person 2)
- **Collect/analyze/diff operations** read and write ONLY via defined schemas and filenames  
- **Redaction runs as streaming step** during collection (no intermediate files)
- All input/output must conform to Person 1's schema specifications

#### Golden Samples Contract
- Use checked-in example specs and artifacts for contract testing
- Ensure changes don't break consumers or violate schema contracts
- Maintain backward compatibility with existing artifact formats

### Cross-Component Integration

#### Collection → Redaction Pipeline
```go
// Example integration flow
func CollectWithRedaction(ctx context.Context, opts CollectionOptions) (*SupportBundle, error) {
    // 1. Auto-discover collectors
    collectors, err := autoCollector.Discover(ctx, opts.DiscoveryOptions)
    if err != nil {
        return nil, err
    }
    
    // 2. Collect with streaming redaction
    bundle := &SupportBundle{}
    for _, collector := range collectors {
        data, err := collector.Collect(ctx)
        if err != nil {
            continue
        }
        
        redactedData, redactionMap, err := redactionEngine.ProcessStream(ctx, data, opts.RedactionOptions)
        if err != nil {
            return nil, err
        }
        
        bundle.AddFile(collector.OutputPath(), redactedData)
        bundle.AddRedactionMap(redactionMap)
    }
    
    return bundle, nil
}
```

#### Analysis → Remediation Integration
```go
// Example analysis to remediation flow
func AnalyzeWithRemediation(ctx context.Context, bundle *SupportBundle) (*AnalysisResult, error) {
    // 1. Run analysis
    result, err := analysisEngine.Analyze(ctx, bundle, opts)
    if err != nil {
        return nil, err
    }
    
    // 2. Generate remediation suggestions
    for i, analyzerResult := range result.Results {
        if analyzerResult.IsFail() {
            remediation, err := generateRemediation(ctx, analyzerResult)
            if err == nil {
                result.Results[i].Remediation = remediation
            }
        }
    }
    
    return result, nil
}
```

### Comprehensive Testing Strategy

#### Unit Testing Requirements
- [ ] **Coverage Target**: >80% code coverage for all components
- [ ] **Mock Dependencies**: Mock all external dependencies (K8s API, registries, LLM APIs)
- [ ] **Error Scenarios**: Test all error paths and edge cases
- [ ] **Performance**: Unit benchmarks for critical paths

#### Integration Testing Requirements  
- [ ] **End-to-End Flows**: Complete collection → redaction → analysis → diff workflows
- [ ] **Real Cluster Testing**: Integration with actual Kubernetes clusters
- [ ] **Large Bundle Testing**: Performance with multi-GB support bundles
- [ ] **Network Conditions**: Testing with limited/intermittent connectivity

#### Performance Testing Requirements
- [ ] **Memory Usage**: Monitor memory consumption during large operations
- [ ] **CPU Utilization**: Profile CPU usage for optimization opportunities
- [ ] **I/O Performance**: Test with large files and slow storage
- [ ] **Concurrency**: Test multi-threaded operations and race conditions

#### Security Testing Requirements
- [ ] **Redaction Completeness**: Verify no sensitive data leakage
- [ ] **Token Security**: Ensure token unpredictability and uniqueness
- [ ] **Access Control**: Verify RBAC enforcement
- [ ] **Input Validation**: Test against malicious inputs

### Golden Sample Testing
- [ ] **Reference Bundles**: Create standard test support bundles
- [ ] **Expected Outputs**: Define expected analysis, diff, and redaction outputs
- [ ] **Regression Testing**: Automated comparison against golden outputs
- [ ] **Schema Validation**: Ensure all outputs conform to schemas

---

## Documentation Requirements

### User Documentation
- [ ] **Collection Guide**: How to use auto-collectors and namespace scoping
- [ ] **Redaction Guide**: Redaction profiles, tokenization, and LLM integration
- [ ] **Analysis Guide**: Agent configuration and remediation interpretation  
- [ ] **Diff Guide**: Bundle comparison workflows and interpretation

### Developer Documentation
- [ ] **API Documentation**: Go doc comments for all public APIs
- [ ] **Architecture Guide**: Component interaction and data flow
- [ ] **Extension Guide**: How to add custom agents, analyzers, and processors
- [ ] **Performance Guide**: Optimization techniques and benchmarks

### Configuration Documentation
- [ ] **Schema Reference**: Complete reference for all configuration options
- [ ] **Profile Examples**: Example redaction and analysis profiles
- [ ] **Integration Examples**: Sample integrations with CI/CD and monitoring

---

## Timeline & Milestones

### Month 1: Foundation
- **Week 1-2**: Auto-collectors and RBAC integration
- **Week 3-4**: Advanced redaction with tokenization

### Month 2: Advanced Features
- **Week 5-6**: Agent-based analysis system
- **Week 7-8**: Support bundle differencing

### Month 3: Integration & Polish
- **Week 9-10**: Cross-component integration and testing
- **Week 11-12**: Documentation, optimization, and release preparation

### Key Milestones
- [ ] **M1**: Auto-discovery working with RBAC (Week 2)
- [ ] **M2**: Streaming redaction with tokenization (Week 4)  
- [ ] **M3**: Local and hosted agents functional (Week 6)
- [ ] **M4**: Bundle diffing and remediation (Week 8)
- [ ] **M5**: Full integration and testing complete (Week 10)
- [ ] **M6**: Documentation and release ready (Week 12)

---

## Success Criteria

### Functional Requirements
- [ ] `support-bundle collect --namespace ns --auto` produces complete bundles
- [ ] Redaction with tokenization works with streaming pipeline
- [ ] Analysis generates structured results with remediation
- [ ] Bundle diffing produces actionable comparison reports

### Performance Requirements
- [ ] Auto-discovery completes in <30 seconds for typical clusters
- [ ] Redaction processes 1GB+ bundles without memory issues
- [ ] Analysis completes in <2 minutes for standard bundles
- [ ] Diff generation completes in <1 minute for bundle pairs

### Quality Requirements
- [ ] >80% code coverage with comprehensive tests
- [ ] Zero critical security vulnerabilities
- [ ] Complete API documentation and user guides
- [ ] Successful integration with Person 1's schema and CLI contracts

---

## Final Integration Testing Phase

After all components are implemented and unit tested, conduct comprehensive integration testing to verify the complete system works together:

### **End-to-End Integration Testing**

#### **1. Complete Workflow Testing**
- [ ] Test full `support-bundle collect --namespace ns --auto` workflow
- [ ] Test auto-discovery → collection → redaction → analysis → diff pipeline
- [ ] Test CLI integration with real Kubernetes clusters
- [ ] Test support bundle generation with all auto-discovered collectors
- [ ] Test complete artifact generation (bundle.tgz, facts.json, redaction-map.json, analysis.json)

#### **2. Cross-Component Integration**
- [ ] Test auto-discovery integration with image metadata collection
- [ ] Test streaming redaction integration with collection pipeline
- [ ] Test analysis engine integration with auto-discovered collectors and redacted data
- [ ] Test support bundle diff functionality with complete bundles
- [ ] Test remediation suggestions integration with analysis results

#### **3. Real-World Scenario Testing**
- [ ] Test against real Kubernetes clusters with various configurations
- [ ] Test with different RBAC permission levels and restrictions
- [ ] Test with various application types (web apps, databases, microservices)
- [ ] Test with large clusters (1000+ pods, 100+ namespaces)
- [ ] Test with different container registries (Docker Hub, ECR, GCR, Harbor)

#### **4. Performance and Reliability Integration**
- [ ] Test end-to-end performance with large, complex clusters
- [ ] Test system reliability with network failures and API errors
- [ ] Test memory usage and resource consumption across all components
- [ ] Test concurrent operations and thread safety
- [ ] Test scalability limits and graceful degradation under load

#### **5. Security and Privacy Integration**
- [ ] Test RBAC enforcement across the entire pipeline
- [ ] Test redaction effectiveness with real sensitive data
- [ ] Test token reversibility and data owner access to redaction maps
- [ ] Test LLM integration security and data locality compliance
- [ ] Test audit trail completeness across all operations

#### **6. User Experience Integration**
- [ ] Test CLI usability and help documentation
- [ ] Test configuration file examples and documentation
- [ ] Test error messages and user feedback across all components
- [ ] Test progress reporting and operation status visibility
- [ ] Test troubleshoot.sh ecosystem integration and compatibility

#### **7. Artifact and Output Integration**
- [ ] Test support bundle format compliance and compatibility
- [ ] Test analysis.json schema validation and tool compatibility
- [ ] Test diff.json format and visualization integration
- [ ] Test redaction-map.json usability and token reversal
- [ ] Test facts.json integration with analysis and visualization tools

---

This PRD provides a comprehensive roadmap for Person 2's implementation work. Each component builds on the others and integrates through well-defined contracts and interfaces. The detailed checklists and step-by-step instructions should provide clear guidance for implementation while maintaining flexibility for technical decisions during development.
