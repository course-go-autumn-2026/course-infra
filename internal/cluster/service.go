package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/progress"
)

const failedStartCleanupLimit = 15 * time.Second

var (
	// ErrPrerequisite marks a missing Docker/cluster requirement.
	ErrPrerequisite = errors.New("cluster prerequisite is not satisfied")
	// ErrConflict marks a port or ownership conflict that must not be overwritten.
	ErrConflict       = errors.New("safe cluster conflict")
	configMapResource = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
)

// Status is the stable cluster status projection used by the CLI.
type Status struct {
	ClusterID         string
	Ready             bool
	RegistryReady     bool
	KindVersion       string
	Kubernetes        string
	KindConfigHash    string
	KnownNamespaces   []string
	RunningNamespaces []string
}

// EnvironmentResourceWarnings reports bounded capacity risks before a lab is
// started. They are warnings only: lifecycle operations remain permitted.
func (s *Service) EnvironmentResourceWarnings(ctx context.Context, lab, running int) []string {
	if lab < 2 {
		return nil
	}
	warnings := []string{fmt.Sprintf("Resource preflight: %d environment(s) are already running; lab %d includes a resource-heavy stack.", running, lab)}
	memory, err := s.runner.Run(ctx, "docker", "info", "--format", "{{.MemTotal}}")
	if err != nil {
		return append(warnings, "Warning: Docker memory capacity could not be measured; monitor Docker resources during startup.")
	}
	bytes, err := strconv.ParseInt(memory, 10, 64)
	if err != nil {
		return append(warnings, "Warning: Docker reported an invalid memory capacity; monitor Docker resources during startup.")
	}
	// Six GiB is the single-heavy-lab baseline. Each concurrently running
	// isolated environment adds a conservative two GiB allowance.
	recommended := int64(6+2*running) * 1024 * 1024 * 1024
	if bytes < recommended {
		warnings = append(warnings, fmt.Sprintf("Warning: Docker has %.1f GiB memory; approximately %.0f GiB is recommended for this parallel environment set.", float64(bytes)/(1024*1024*1024), float64(recommended)/(1024*1024*1024)))
	}
	return warnings
}

// Access is verified connection information for the owned Kubernetes cluster.
type Access struct {
	ClusterID string
	Config    *rest.Config
}

// Service manages the one owned kind cluster.
type Service struct {
	stderr           io.Writer
	runner           runner
	download         Downloader
	cacheDir         string
	configDir        string
	goos             string
	goarch           string
	portConflicts    func() []int
	kubernetesAccess func(context.Context, string, string, bool) (Access, error)
	cleanupTimeout   time.Duration
}

// NewDefault constructs a service using OS directories and process execution.
func NewDefault(stderr io.Writer) (*Service, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user cache directory: %w", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user config directory: %w", err)
	}
	service := &Service{
		stderr: stderr, runner: execRunner{}, download: Downloader{Client: &http.Client{Timeout: 2 * time.Minute}},
		cacheDir: filepath.Join(cacheDir, "tripgoctl"), configDir: filepath.Join(configDir, "tripgoctl"),
		goos: runtime.GOOS, goarch: runtime.GOARCH, portConflicts: checkPorts,
	}
	service.kubernetesAccess = service.kindKubernetesAccess
	return service, nil
}

func (s *Service) statePath() string { return filepath.Join(s.configDir, "cluster-state.json") }
func (s *Service) lockPath() string  { return filepath.Join(s.configDir, "cluster.lock") }

// Start creates or verifies the managed registry and kind cluster.
func (s *Service) Start(ctx context.Context) (Status, error) {
	progress.Report(ctx, progress.Stage, "Checking local prerequisites")
	lock, err := acquireLock(s.lockPath())
	if err != nil {
		return Status{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	defer lock.close()

	if err := s.checkPlatform(); err != nil {
		return Status{}, err
	}
	if err := s.checkDocker(ctx); err != nil {
		return Status{}, err
	}
	progress.Report(ctx, progress.Stage, "Preparing kind")
	kindPath, err := s.download.EnsureKind(ctx, s.cacheDir, s.goos, s.goarch)
	if err != nil {
		return Status{}, fmt.Errorf("%w: %w", ErrPrerequisite, err)
	}
	config, configHash, err := kindConfig()
	if err != nil {
		return Status{}, err
	}
	clusters, err := s.kindClusters(ctx, kindPath)
	if err != nil {
		return Status{}, err
	}
	if contains(clusters, Name) {
		progress.Report(ctx, progress.Stage, "Verifying existing cluster")
		return s.verifyExisting(ctx, configHash)
	}

	if err := s.removeOwnedOrphanRegistry(ctx); err != nil {
		return Status{}, err
	}
	conflicts := s.portConflicts()
	if len(conflicts) > 0 {
		return Status{}, fmt.Errorf("%w: required host ports are busy: %s", ErrConflict, joinInts(conflicts))
	}
	s.warnResources(ctx)

	clusterID, err := newClusterID()
	if err != nil {
		return Status{}, fmt.Errorf("create cluster identity: %w", err)
	}
	progress.Report(ctx, progress.Stage, "Starting local registry")
	if err := s.startRegistry(ctx, clusterID); err != nil {
		return Status{}, err
	}
	created := false
	defer func() {
		if !created {
			s.runFailedStartCleanup("docker", "rm", "-f", RegistryContainer)
		}
	}()

	configFile, err := os.CreateTemp("", "tripgo-kind-*.yaml")
	if err != nil {
		return Status{}, fmt.Errorf("create kind config: %w", err)
	}
	configPath := configFile.Name()
	defer func() { _ = os.Remove(configPath) }()
	defer func() { _ = configFile.Close() }()
	if err := configFile.Chmod(0o600); err != nil {
		return Status{}, err
	}
	if _, err := configFile.Write(config); err != nil {
		return Status{}, fmt.Errorf("write kind config: %w", err)
	}
	if err := configFile.Close(); err != nil {
		return Status{}, err
	}
	progress.Report(ctx, progress.Stage, "Creating Kubernetes cluster")
	if _, err := s.runner.Run(ctx, kindPath, "create", "cluster", "--config", configPath, "--wait", "120s"); err != nil {
		return Status{}, fmt.Errorf("%w: create kind cluster: %w", ErrPrerequisite, err)
	}
	if _, err := s.runner.Run(ctx, "docker", "network", "connect", "kind", RegistryContainer); err != nil && !strings.Contains(err.Error(), "already exists") {
		s.runFailedStartCleanup(kindPath, "delete", "cluster", "--name", Name)
		return Status{}, fmt.Errorf("connect local registry to kind network: %w", err)
	}
	markerCommand := fmt.Sprintf("mkdir -p %s && printf '%%s' %s > %s", filepath.Dir(markerPath), clusterID, markerPath)
	if _, err := s.runner.Run(ctx, "docker", "exec", ControlPlaneContainer, "sh", "-c", markerCommand); err != nil {
		s.runFailedStartCleanup(kindPath, "delete", "cluster", "--name", Name)
		return Status{}, fmt.Errorf("write cluster identity marker: %w", err)
	}
	progress.Report(ctx, progress.Stage, "Verifying Kubernetes access")
	if _, err := s.accessKubernetes(ctx, kindPath, clusterID, true); err != nil {
		s.runFailedStartCleanup(kindPath, "delete", "cluster", "--name", Name)
		return Status{}, err
	}
	value := state{SchemaVersion: 1, ClusterID: clusterID, KindVersion: KindVersion, Kubernetes: KubernetesVersion, KindConfigHash: configHash, Namespaces: []string{}}
	if err := writeState(s.statePath(), value); err != nil {
		s.runFailedStartCleanup(kindPath, "delete", "cluster", "--name", Name)
		return Status{}, err
	}
	created = true
	return statusFromState(value, true, true), nil
}

func (s *Service) runFailedStartCleanup(name string, arguments ...string) {
	limit := s.cleanupTimeout
	if limit <= 0 {
		limit = failedStartCleanupLimit
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	_, _ = s.runner.Run(ctx, name, arguments...)
}

// Inspect verifies ownership and returns current readiness without mutation.
func (s *Service) Inspect(ctx context.Context) (Status, error) {
	lock, err := acquireLock(s.lockPath())
	if err != nil {
		return Status{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	defer lock.close()
	if err := s.checkPlatform(); err != nil {
		return Status{}, err
	}
	if err := s.checkDocker(ctx); err != nil {
		return Status{}, err
	}
	value, err := loadState(s.statePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Status{}, fmt.Errorf("%w: cluster %s does not exist; run tripgoctl cluster start", ErrPrerequisite, Name)
		}
		return Status{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	_, configHash, err := kindConfig()
	if err != nil {
		return Status{}, err
	}
	return s.verifyStateAndContainers(ctx, value, configHash)
}

// EnvironmentLease holds the cluster operation lock across one environment operation.
type EnvironmentLease struct {
	service *Service
	lock    *operationLock
	state   state
	access  Access
}

// Access returns the verified API connection for this lease.
func (l *EnvironmentLease) Access() Access { return l.access }

// SetEnvironmentState updates the projection only for the cluster generation
// that created this lease.
func (l *EnvironmentLease) SetEnvironmentState(namespace string, known, running bool) error {
	current, err := loadState(l.service.statePath())
	if err != nil {
		return fmt.Errorf("%w: update environment state: %v", ErrPrerequisite, err)
	}
	if current.ClusterID != l.state.ClusterID {
		return fmt.Errorf("%w: cluster identity changed during environment operation", ErrConflict)
	}
	current.Namespaces = setMembership(current.Namespaces, namespace, known)
	current.RunningNamespaces = setMembership(current.RunningNamespaces, namespace, known && running)
	if err := writeState(l.service.statePath(), current); err != nil {
		return err
	}
	l.state = current
	return nil
}

// Close releases the cluster operation lock.
func (l *EnvironmentLease) Close() {
	if l.lock != nil {
		l.lock.close()
		l.lock = nil
	}
}

// BeginEnvironmentOperation verifies the owned cluster and holds its lock until
// the returned lease is closed.
func (s *Service) BeginEnvironmentOperation(ctx context.Context) (*EnvironmentLease, error) {
	lock, err := acquireLock(s.lockPath())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	fail := func(err error) (*EnvironmentLease, error) {
		lock.close()
		return nil, err
	}
	if err := s.checkPlatform(); err != nil {
		return fail(err)
	}
	if err := s.checkDocker(ctx); err != nil {
		return fail(err)
	}
	value, err := loadState(s.statePath())
	if err != nil {
		return fail(fmt.Errorf("%w: cluster %s state is unavailable: %v", ErrPrerequisite, Name, err))
	}
	_, configHash, err := kindConfig()
	if err != nil {
		return fail(err)
	}
	if _, err := s.verifyStateAndContainers(ctx, value, configHash); err != nil {
		return fail(err)
	}
	kindPath, err := s.download.EnsureKind(ctx, s.cacheDir, s.goos, s.goarch)
	if err != nil {
		return fail(fmt.Errorf("%w: %w", ErrPrerequisite, err))
	}
	access, err := s.accessKubernetes(ctx, kindPath, value.ClusterID, false)
	if err != nil {
		return fail(err)
	}
	return &EnvironmentLease{service: s, lock: lock, state: value, access: access}, nil
}

func (s *Service) accessKubernetes(ctx context.Context, kindPath, clusterID string, ensureMarker bool) (Access, error) {
	if s.kubernetesAccess != nil {
		return s.kubernetesAccess(ctx, kindPath, clusterID, ensureMarker)
	}
	return s.kindKubernetesAccess(ctx, kindPath, clusterID, ensureMarker)
}

func (s *Service) kindKubernetesAccess(ctx context.Context, kindPath, clusterID string, ensureMarker bool) (Access, error) {
	kubeconfig, err := s.runner.Run(ctx, kindPath, "get", "kubeconfig", "--name", Name)
	if err != nil {
		return Access{}, fmt.Errorf("%w: read kubeconfig from owned kind cluster: %w", ErrPrerequisite, err)
	}
	config, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		return Access{}, fmt.Errorf("%w: decode owned kind kubeconfig: %v", ErrPrerequisite, err)
	}
	config.Timeout = 30 * time.Second
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return Access{}, fmt.Errorf("create cluster identity client: %w", err)
	}
	resource := client.Resource(configMapResource).Namespace("kube-system")
	if ensureMarker {
		marker := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]any{
				"name": "tripgoctl-cluster-identity", "namespace": "kube-system",
				"labels": map[string]any{"app.kubernetes.io/managed-by": "tripgoctl", "app.kubernetes.io/part-of": "tripgo-local"},
			},
			"data": map[string]any{"cluster-id": clusterID},
		}}
		payload, marshalErr := json.Marshal(marker.Object)
		if marshalErr != nil {
			return Access{}, fmt.Errorf("encode cluster identity marker: %w", marshalErr)
		}
		if _, err := resource.Patch(ctx, marker.GetName(), types.ApplyPatchType, payload, metav1.PatchOptions{FieldManager: "tripgoctl"}); err != nil {
			return Access{}, fmt.Errorf("install cluster identity marker: %w", err)
		}
	} else {
		marker, getErr := resource.Get(ctx, "tripgoctl-cluster-identity", metav1.GetOptions{})
		if getErr != nil {
			return Access{}, fmt.Errorf("%w: read API cluster identity marker: %w", ErrConflict, getErr)
		}
		identity, _, _ := unstructured.NestedString(marker.Object, "data", "cluster-id")
		if identity != clusterID {
			return Access{}, fmt.Errorf("%w: Kubernetes API cluster identity does not match local state", ErrConflict)
		}
	}
	return Access{ClusterID: clusterID, Config: config}, nil
}

// Stop deletes only a cluster and registry with matching identity markers.
func (s *Service) Stop(ctx context.Context, expectedNamespaces []string) error {
	lock, err := acquireLock(s.lockPath())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	defer lock.close()
	if err := s.checkPlatform(); err != nil {
		return err
	}
	if err := s.checkDocker(ctx); err != nil {
		return err
	}
	value, stateErr := loadState(s.statePath())
	kindPath, kindErr := s.download.EnsureKind(ctx, s.cacheDir, s.goos, s.goarch)
	if kindErr != nil {
		return fmt.Errorf("%w: %w", ErrPrerequisite, kindErr)
	}
	clusters, err := s.kindClusters(ctx, kindPath)
	if err != nil {
		return err
	}
	clusterExists := contains(clusters, Name)
	registryExists, err := s.containerExists(ctx, RegistryContainer)
	if err != nil {
		return err
	}
	if !clusterExists && !registryExists {
		if err := os.Remove(s.statePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale cluster state: %w", err)
		}
		return nil
	}
	if stateErr != nil {
		return fmt.Errorf("%w: cannot prove ownership because cluster state is unavailable: %v", ErrConflict, stateErr)
	}
	expected := append([]string(nil), expectedNamespaces...)
	actual := append([]string(nil), value.Namespaces...)
	slices.Sort(expected)
	slices.Sort(actual)
	if !slices.Equal(expected, actual) {
		return fmt.Errorf("%w: environments changed after confirmation; run tripgoctl cluster stop again", ErrConflict)
	}
	if clusterExists {
		marker, markerErr := s.runner.Run(ctx, "docker", "exec", ControlPlaneContainer, "cat", markerPath)
		if markerErr != nil || marker != value.ClusterID {
			return fmt.Errorf("%w: cluster identity marker does not match local state", ErrConflict)
		}
		if _, err := s.runner.Run(ctx, kindPath, "delete", "cluster", "--name", Name); err != nil {
			return fmt.Errorf("delete kind cluster: %w", err)
		}
	}
	if registryExists {
		if err := s.verifyRegistryOwnership(ctx, value.ClusterID); err != nil {
			return err
		}
		if _, err := s.runner.Run(ctx, "docker", "rm", "-f", RegistryContainer); err != nil {
			return fmt.Errorf("delete local registry: %w", err)
		}
	}
	if err := os.Remove(s.statePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove cluster state: %w", err)
	}
	return nil
}

func (s *Service) checkPlatform() error {
	if _, ok := kindChecksums[s.goos+"/"+s.goarch]; !ok {
		return fmt.Errorf("%w: unsupported platform %s/%s", ErrPrerequisite, s.goos, s.goarch)
	}
	return nil
}

func (s *Service) checkDocker(ctx context.Context) error {
	if _, err := s.runner.Run(ctx, "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		return fmt.Errorf("%w: Docker daemon is unavailable: %w", ErrPrerequisite, err)
	}
	return nil
}

func (s *Service) kindClusters(ctx context.Context, kindPath string) ([]string, error) {
	output, err := s.runner.Run(ctx, kindPath, "get", "clusters")
	if err != nil {
		return nil, fmt.Errorf("list kind clusters: %w", err)
	}
	return strings.Fields(output), nil
}

func (s *Service) verifyExisting(ctx context.Context, configHash string) (Status, error) {
	value, err := loadState(s.statePath())
	if err != nil {
		return Status{}, fmt.Errorf("%w: cluster %s exists but owned state is unavailable: %v", ErrConflict, Name, err)
	}
	return s.verifyStateAndContainers(ctx, value, configHash)
}

func (s *Service) verifyStateAndContainers(ctx context.Context, value state, configHash string) (Status, error) {
	if value.KindVersion != KindVersion || value.Kubernetes != KubernetesVersion || value.KindConfigHash != configHash {
		return Status{}, fmt.Errorf("%w: existing cluster configuration differs from this tripgoctl version", ErrConflict)
	}
	marker, err := s.runner.Run(ctx, "docker", "exec", ControlPlaneContainer, "cat", markerPath)
	if err != nil || marker != value.ClusterID {
		return Status{}, fmt.Errorf("%w: cluster identity marker does not match local state", ErrConflict)
	}
	if err := s.verifyRegistryOwnership(ctx, value.ClusterID); err != nil {
		return Status{}, err
	}
	running, err := s.runner.Run(ctx, "docker", "inspect", "--format", "{{.State.Running}}", ControlPlaneContainer)
	if err != nil {
		return Status{}, fmt.Errorf("inspect kind control plane: %w", err)
	}
	registryRunning, err := s.runner.Run(ctx, "docker", "inspect", "--format", "{{.State.Running}}", RegistryContainer)
	if err != nil {
		return Status{}, fmt.Errorf("inspect local registry: %w", err)
	}
	return statusFromState(value, running == "true", registryRunning == "true"), nil
}

func (s *Service) startRegistry(ctx context.Context, clusterID string) error {
	arguments := []string{
		"run", "-d", "--restart=always", "--name", RegistryContainer,
		"-p", fmt.Sprintf("%s:%d:%d", catalog.HostListenAddress, catalog.LocalRegistryHostPort, catalog.LocalRegistryInnerPort),
		"--label", "tripgo.course/managed-by=tripgoctl",
		"--label", "tripgo.course/cluster-id=" + clusterID,
		registryImageReference(),
	}
	if _, err := s.runner.Run(ctx, "docker", arguments...); err != nil {
		return fmt.Errorf("%w: start managed local registry: %w", ErrPrerequisite, err)
	}
	return nil
}

func (s *Service) removeOwnedOrphanRegistry(ctx context.Context) error {
	exists, err := s.containerExists(ctx, RegistryContainer)
	if err != nil || !exists {
		return err
	}
	value, stateErr := loadState(s.statePath())
	if stateErr != nil {
		return fmt.Errorf("%w: registry container %s already exists and ownership cannot be proved", ErrConflict, RegistryContainer)
	}
	if err := s.verifyRegistryOwnership(ctx, value.ClusterID); err != nil {
		return err
	}
	if _, err := s.runner.Run(ctx, "docker", "rm", "-f", RegistryContainer); err != nil {
		return fmt.Errorf("remove orphan local registry: %w", err)
	}
	return nil
}

func (s *Service) verifyRegistryOwnership(ctx context.Context, clusterID string) error {
	labels, err := s.runner.Run(ctx, "docker", "inspect", "--format", `{{index .Config.Labels "tripgo.course/managed-by"}}|{{index .Config.Labels "tripgo.course/cluster-id"}}`, RegistryContainer)
	if err != nil {
		return fmt.Errorf("%w: managed local registry is missing: %w", ErrConflict, err)
	}
	if labels != "tripgoctl|"+clusterID {
		return fmt.Errorf("%w: registry container identity does not match local state", ErrConflict)
	}
	return nil
}

func (s *Service) containerExists(ctx context.Context, name string) (bool, error) {
	output, err := s.runner.Run(ctx, "docker", "ps", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}")
	if err != nil {
		return false, fmt.Errorf("inspect Docker containers: %w", err)
	}
	return output == name, nil
}

func (s *Service) warnResources(ctx context.Context) {
	memory, err := s.runner.Run(ctx, "docker", "info", "--format", "{{.MemTotal}}")
	if err == nil {
		bytes, parseErr := strconv.ParseInt(memory, 10, 64)
		if parseErr == nil && bytes < 6*1024*1024*1024 {
			message := fmt.Sprintf("Warning: Docker has %.1f GiB memory; full labs may require at least 6 GiB.", float64(bytes)/(1024*1024*1024))
			progress.Report(ctx, progress.Warning, message)
			if _, attached := progress.ReporterFromContext(ctx); !attached {
				_, _ = fmt.Fprintln(s.stderr, message)
			}
		}
	}
	var stats syscall.Statfs_t
	if err := syscall.Statfs(s.cacheDir, &stats); err == nil {
		free := uint64(stats.Bavail) * uint64(stats.Bsize)
		if free < 10*1024*1024*1024 {
			message := fmt.Sprintf("Warning: %.1f GiB free near the tripgoctl cache; image pulls may require more disk.", float64(free)/(1024*1024*1024))
			progress.Report(ctx, progress.Warning, message)
			if _, attached := progress.ReporterFromContext(ctx); !attached {
				_, _ = fmt.Fprintln(s.stderr, message)
			}
		}
	}
}

func checkPorts() []int {
	ports := []int{catalog.LocalRegistryHostPort}
	for lab := 1; lab <= 5; lab++ {
		resolved, _ := catalog.Ports(lab)
		for _, port := range resolved {
			ports = append(ports, port.HostPort)
		}
	}
	conflicts := make([]int, 0)
	for _, port := range ports {
		listener, err := net.Listen("tcp4", fmt.Sprintf("%s:%d", catalog.HostListenAddress, port))
		if err != nil {
			conflicts = append(conflicts, port)
			continue
		}
		_ = listener.Close()
	}
	return conflicts
}

func newClusterID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func statusFromState(value state, ready, registryReady bool) Status {
	return Status{
		ClusterID: value.ClusterID, Ready: ready, RegistryReady: registryReady,
		KindVersion: value.KindVersion, Kubernetes: value.Kubernetes, KindConfigHash: value.KindConfigHash,
		KnownNamespaces:   append([]string(nil), value.Namespaces...),
		RunningNamespaces: append([]string(nil), value.RunningNamespaces...),
	}
}

func setMembership(values []string, target string, present bool) []string {
	result := append([]string(nil), values...)
	index := slices.Index(result, target)
	if present && index < 0 {
		result = append(result, target)
	}
	if !present && index >= 0 {
		result = slices.Delete(result, index, index+1)
	}
	slices.Sort(result)
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = strconv.Itoa(value)
	}
	return strings.Join(parts, ", ")
}
