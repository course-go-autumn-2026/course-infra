package cluster

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config", "cluster-state.json")
	expected := state{SchemaVersion: 1, ClusterID: "identity", KindVersion: KindVersion, Kubernetes: KubernetesVersion, KindConfigHash: "hash", Namespaces: []string{"tripgo-lab-01"}}
	if err := writeState(path, expected); err != nil {
		t.Fatal(err)
	}
	actual, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("state = %#v, expected %#v", actual, expected)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %v, error = %v", info.Mode(), err)
	}
}

func TestLoadStateAllowsAdditivePatchMinorFields(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	content := `{"schema_version":1,"cluster_id":"owned","kind_version":"v0.27.0","kubernetes_version":"v1.32.2","kind_config_sha256":"hash","future_patch_field":true}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadState(path)
	if err != nil {
		t.Fatalf("loadState() rejected additive patch/minor field: %v", err)
	}
	if got.ClusterID != "owned" {
		t.Fatalf("cluster ID = %q", got.ClusterID)
	}
}

func TestOperationLockRejectsConcurrentOwner(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "cluster.lock")
	first, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	second, err := acquireLock(path)
	if err == nil {
		second.close()
		t.Fatal("second lock unexpectedly succeeded")
	}
	first.close()
	third, err := acquireLock(path)
	if err != nil {
		t.Fatalf("stale lock file blocked recovery after owner exit: %v", err)
	}
	third.close()
}
