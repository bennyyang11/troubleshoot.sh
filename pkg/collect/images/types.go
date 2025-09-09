package images

import (
	"context"
	"time"
)

// ImageFacts represents comprehensive metadata about a container image
type ImageFacts struct {
	Repository string            `json:"repository"`
	Tag        string            `json:"tag"`
	Digest     string            `json:"digest"`
	Registry   string            `json:"registry"`
	Size       int64             `json:"size"`
	Created    time.Time         `json:"created"`
	Labels     map[string]string `json:"labels"`
	Platform   Platform          `json:"platform"`
	Layers     []LayerInfo       `json:"layers,omitempty"`
	Config     ImageConfig       `json:"config,omitempty"`
}

// Platform represents the target platform for an image
type Platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

// LayerInfo contains information about an image layer
type LayerInfo struct {
	Digest      string    `json:"digest"`
	Size        int64     `json:"size"`
	MediaType   string    `json:"mediaType"`
	URLs        []string  `json:"urls,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ImageConfig contains image configuration details
type ImageConfig struct {
	ExposedPorts map[string]struct{} `json:"exposedPorts,omitempty"`
	Env          []string            `json:"env,omitempty"`
	Entrypoint   []string            `json:"entrypoint,omitempty"`
	Cmd          []string            `json:"cmd,omitempty"`
	WorkingDir   string              `json:"workingDir,omitempty"`
	User         string              `json:"user,omitempty"`
	Volumes      map[string]struct{} `json:"volumes,omitempty"`
}

// RegistryClient defines the interface for interacting with container registries
type RegistryClient interface {
	GetImageFacts(ctx context.Context, imageRef string) (*ImageFacts, error)
	ResolveDigest(ctx context.Context, imageRef string) (string, error)
	ParseManifest(ctx context.Context, imageRef string) (*ManifestInfo, error)
	Authenticate(ctx context.Context, registry string, credentials *RegistryCredentials) error
	SupportsRegistry(registry string) bool
}

// ManifestInfo represents parsed image manifest data
type ManifestInfo struct {
	SchemaVersion int                    `json:"schemaVersion"`
	MediaType     string                 `json:"mediaType"`
	Config        ManifestConfig         `json:"config"`
	Layers        []ManifestLayer        `json:"layers"`
	Annotations   map[string]string      `json:"annotations,omitempty"`
	Platform      *Platform              `json:"platform,omitempty"`
}

// ManifestConfig represents the config section of an image manifest
type ManifestConfig struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

// ManifestLayer represents a layer in an image manifest
type ManifestLayer struct {
	MediaType   string            `json:"mediaType"`
	Size        int64             `json:"size"`
	Digest      string            `json:"digest"`
	URLs        []string          `json:"urls,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// RegistryCredentials contains authentication information for registries
type RegistryCredentials struct {
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	Token         string `json:"token,omitempty"`
	IdentityToken string `json:"identityToken,omitempty"`
	RegistryToken string `json:"registryToken,omitempty"`
}

// ImageCollectionOptions configures image metadata collection
type ImageCollectionOptions struct {
	IncludeManifests bool                           `json:"includeManifests"`
	IncludeLayers    bool                           `json:"includeLayers"`
	IncludeConfig    bool                           `json:"includeConfig"`
	Credentials      map[string]*RegistryCredentials `json:"credentials,omitempty"`
	Timeout          time.Duration                  `json:"timeout"`
	MaxConcurrency   int                            `json:"maxConcurrency"`
	RetryCount       int                            `json:"retryCount"`
	CacheEnabled     bool                           `json:"cacheEnabled"`
}

// ImageCollectionResult represents the result of image metadata collection
type ImageCollectionResult struct {
	Facts        map[string]*ImageFacts `json:"facts"`        // imageRef -> facts
	Errors       map[string]error       `json:"errors"`       // imageRef -> error
	Statistics   CollectionStatistics   `json:"statistics"`
	Duration     time.Duration          `json:"duration"`
	Timestamp    time.Time              `json:"timestamp"`
}

// CollectionStatistics contains metrics about the image collection process
type CollectionStatistics struct {
	TotalImages       int `json:"totalImages"`
	SuccessfulImages  int `json:"successfulImages"`
	FailedImages      int `json:"failedImages"`
	CacheHits         int `json:"cacheHits"`
	CacheMisses       int `json:"cacheMisses"`
	RegistriesAccessed int `json:"registriesAccessed"`
}

// FactsBuilder creates ImageFacts from registry data
type FactsBuilder interface {
	BuildFacts(ctx context.Context, imageRef string, manifest *ManifestInfo, config *ImageConfig) (*ImageFacts, error)
	ExtractImageReference(imageRef string) (registry, repository, tag string, err error)
	NormalizeImageReference(imageRef string) (string, error)
	SetProgressReporter(reporter ProgressReporter)
}

// DigestResolver resolves image tags to digests
type DigestResolver interface {
	ResolveTagToDigest(ctx context.Context, imageRef string) (string, error)
	ResolvePlatformDigest(ctx context.Context, imageRef string, platform Platform) (string, error)
	GetManifestList(ctx context.Context, imageRef string) (*ManifestList, error)
}

// ManifestList represents a multi-platform manifest list
type ManifestList struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MediaType     string               `json:"mediaType"`
	Manifests     []ManifestDescriptor `json:"manifests"`
}

// ManifestDescriptor describes a manifest in a manifest list
type ManifestDescriptor struct {
	MediaType string    `json:"mediaType"`
	Size      int64     `json:"size"`
	Digest    string    `json:"digest"`
	Platform  *Platform `json:"platform,omitempty"`
}

// ImageReference contains parsed components of an image reference
type ImageReference struct {
	Registry   string `json:"registry"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest,omitempty"`
	Original   string `json:"original"`
}

// CollectionError represents an error during image collection
type CollectionError struct {
	ImageRef string `json:"imageRef"`
	Type     string `json:"type"`     // "auth", "network", "manifest", "config"
	Message  string `json:"message"`
	Retryable bool  `json:"retryable"`
}

// CacheEntry represents a cached image facts entry
type CacheEntry struct {
	Facts     *ImageFacts `json:"facts"`
	Timestamp time.Time   `json:"timestamp"`
	TTL       time.Duration `json:"ttl"`
}

// ProgressReporter handles progress reporting for image operations
type ProgressReporter interface {
	Start(totalImages int)
	Update(completedImages int, currentImage string)
	Error(imageRef string, err error)
	Complete(result *ImageCollectionResult)
}
