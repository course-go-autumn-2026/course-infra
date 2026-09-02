package generator_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/environment"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/generator"
)

const testPushImage = "localhost:5001/tripgo-push-service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var testOptions = generator.Options{
	TripgoctlVersion:   "v1.0.0",
	ClusterID:          "golden-cluster-id",
	PushImageReference: testPushImage,
}

func TestGoldenEnvironments(t *testing.T) {
	for lab := 1; lab <= 5; lab++ {
		t.Run(fmt.Sprintf("lab-%d", lab), func(t *testing.T) {
			t.Parallel()
			config := fixtureConfig(t, lab)
			first, err := generator.Generate(config, testOptions)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			second, err := generator.Generate(config, testOptions)
			if err != nil {
				t.Fatalf("second Generate() error = %v", err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatal("repeated generation produced a diff")
			}

			golden := filepath.Join("..", "..", "tests", "golden", fmt.Sprintf("lab-%d", lab), ".tripgo")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := writeGolden(golden, first); err != nil {
					t.Fatalf("update golden: %v", err)
				}
			}
			assertGolden(t, golden, first)
			assertKubernetesDocuments(t, first, config)
		})
	}
}

func TestWorkloadReplicaOverrideCoversLab3Deployments(t *testing.T) {
	t.Parallel()
	config := fixtureConfig(t, 3)
	zero := int64(0)
	options := testOptions
	options.WorkloadReplicas = &zero
	bundle, err := generator.Generate(config, options)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, file := range bundle.Files {
		counts[file.Name] = bytes.Count(file.Content, []byte("  replicas: 0\n"))
	}
	if counts["postgres.yaml"] != 1 || counts["observability.yaml"] != 4 || counts["push.yaml"] != 1 {
		t.Fatalf("zero replica counts = %#v", counts)
	}
}

func TestRedpandaClaimTemplateIsStableAcrossTripgoctlUpgrade(t *testing.T) {
	t.Parallel()
	config := fixtureConfig(t, 4)
	oldOptions := testOptions
	oldOptions.TripgoctlVersion = "v1.0.0"
	newOptions := testOptions
	newOptions.TripgoctlVersion = "v1.1.0"
	oldBundle, err := generator.Generate(config, oldOptions)
	if err != nil {
		t.Fatal(err)
	}
	newBundle, err := generator.Generate(config, newOptions)
	if err != nil {
		t.Fatal(err)
	}
	oldClaims := statefulSetClaimTemplates(t, oldBundle)
	newClaims := statefulSetClaimTemplates(t, newBundle)
	if !reflect.DeepEqual(oldClaims, newClaims) {
		t.Fatalf("volumeClaimTemplates changed across tripgoctl versions:\nold=%#v\nnew=%#v", oldClaims, newClaims)
	}
}

func TestLab4RendersStatefulRedpandaAndContinuousTopicReconciliation(t *testing.T) {
	t.Parallel()
	zero := int64(0)
	options := testOptions
	options.WorkloadReplicas = &zero
	bundle, err := generator.Generate(fixtureConfig(t, 4), options)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range bundle.Files {
		files[file.Name] = string(file.Content)
	}
	redpanda := files["redpanda.yaml"]
	topics := files["topics.yaml"]
	for _, expected := range []string{
		"kind: StatefulSet\nmetadata:\n  name: redpanda",
		"--advertise-kafka-addr=internal://redpanda:9092,external://localhost:24092",
		"name: redpanda-topic-reconciler",
		"replicas: 0",
	} {
		if !strings.Contains(redpanda, expected) {
			t.Errorf("redpanda manifest missing %q", expected)
		}
	}
	for _, expected := range []string{"trip.events.v1=3", "trip.commands.v1=3", "trip.commands.v1.dlq=1", "refusing destructive reduction", "while true"} {
		if !strings.Contains(topics, expected) {
			t.Errorf("topics manifest missing %q", expected)
		}
	}
}

func TestCustomEnvDoesNotLeakIntoManifests(t *testing.T) {
	t.Parallel()

	base := fixtureConfig(t, 1)
	custom := base
	custom.CustomEnv = []catalog.EnvVariable{{Name: "PRIVATE_TOKEN", Value: "do-not-store"}}
	baseBundle, err := generator.Generate(base, testOptions)
	if err != nil {
		t.Fatal(err)
	}
	customBundle, err := generator.Generate(custom, testOptions)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseBundle, customBundle) {
		t.Fatal("custom env changed manifests")
	}
}

func TestUnpublishedPushRequiresTrustedDigest(t *testing.T) {
	t.Parallel()

	config := fixtureConfig(t, 3)
	options := testOptions
	options.PushImageReference = ""
	_, err := generator.Generate(config, options)
	if err == nil || !strings.Contains(err.Error(), "local registry") {
		t.Fatalf("Generate() error = %v", err)
	}
	options.PushImageReference = "localhost:5001/tripgo-push-service:latest"
	_, err = generator.Generate(config, options)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("Generate() mutable image error = %v", err)
	}
}

func TestWriteIsOwnedAndRepeatable(t *testing.T) {
	t.Parallel()

	bundle, err := generator.Generate(fixtureConfig(t, 2), testOptions)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), ".tripgo")
	if err := generator.Write(target, bundle); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	stale := filepath.Join(target, "rendered", "stale.yaml")
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyLock := filepath.Join(target, "environment.lock")
	if err := os.WriteFile(legacyLock, []byte("# Generated by tripgoctl. Do not edit manually.\ncluster_id = \"old\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := generator.Write(target, bundle); err != nil {
		t.Fatalf("repeated Write() error = %v", err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale file still exists: %v", err)
	}
	if _, err := os.Stat(legacyLock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy environment.lock still exists: %v", err)
	}
	assertGolden(t, target, bundle)
}

func TestWritePreservesUnknownTripgoEntries(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), ".tripgo")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(target, "notes")
	if err := os.WriteFile(notes, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := generator.Write(target, generator.Bundle{}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	assertFile(t, notes, []byte("mine"))
}

func TestWriteRefusesSymlinkedRenderedDirectory(t *testing.T) {
	t.Parallel()

	bundle, err := generator.Generate(fixtureConfig(t, 1), testOptions)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	target := filepath.Join(directory, ".tripgo")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(directory, filepath.Join(target, "rendered")); err != nil {
		t.Fatal(err)
	}
	if err := generator.Write(target, bundle); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Write() error = %v", err)
	}
}

func statefulSetClaimTemplates(t *testing.T, bundle generator.Bundle) any {
	t.Helper()
	for _, file := range bundle.Files {
		if file.Name != "redpanda.yaml" {
			continue
		}
		decoder := yaml.NewDecoder(bytes.NewReader(file.Content))
		for {
			var resource map[string]any
			if err := decoder.Decode(&resource); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			if resource["kind"] != "StatefulSet" {
				continue
			}
			spec, _ := resource["spec"].(map[string]any)
			return spec["volumeClaimTemplates"]
		}
	}
	t.Fatal("Redpanda StatefulSet not found")
	return nil
}

func fixtureConfig(t *testing.T, lab int) environment.Config {
	t.Helper()
	path := filepath.Join("..", "..", "tests", "fixtures", "environments", fmt.Sprintf("lab-%d", lab), "environment.toml")
	config, err := environment.ParseFile(path)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return config
}

func writeGolden(target string, bundle generator.Bundle) error {
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(target, "rendered"), 0o700); err != nil {
		return err
	}
	for _, file := range bundle.Files {
		if err := os.WriteFile(filepath.Join(target, "rendered", file.Name), file.Content, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func assertGolden(t *testing.T, target string, bundle generator.Bundle) {
	t.Helper()
	for _, file := range bundle.Files {
		assertFile(t, filepath.Join(target, "rendered", file.Name), file.Content)
	}
	entries, err := os.ReadDir(filepath.Join(target, "rendered"))
	if err != nil {
		t.Fatalf("read rendered golden: %v", err)
	}
	if len(entries) != len(bundle.Files) {
		t.Fatalf("rendered file count = %d, expected %d", len(entries), len(bundle.Files))
	}
}

func assertFile(t *testing.T, path string, expected []byte) {
	t.Helper()
	// #nosec G304 -- test paths are constrained to fixtures or t.TempDir.
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run UPDATE_GOLDEN=1 go test ./internal/generator)", path, err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("golden mismatch: %s (run UPDATE_GOLDEN=1 go test ./internal/generator)", path)
	}
}

func assertKubernetesDocuments(t *testing.T, bundle generator.Bundle, config environment.Config) {
	t.Helper()
	namespace, _ := catalog.Namespace(config.Lab)
	for _, file := range bundle.Files {
		decoder := yaml.NewDecoder(bytes.NewReader(file.Content))
		for document := 1; ; document++ {
			var resource map[string]any
			err := decoder.Decode(&resource)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("decode %s document %d: %v", file.Name, document, err)
			}
			kind, _ := resource["kind"].(string)
			apiVersion, _ := resource["apiVersion"].(string)
			metadata, _ := resource["metadata"].(map[string]any)
			if kind == "" || apiVersion == "" || metadata == nil {
				t.Fatalf("%s document %d is not a Kubernetes resource", file.Name, document)
			}
			labels, _ := metadata["labels"].(map[string]any)
			annotations, _ := metadata["annotations"].(map[string]any)
			if labels["app.kubernetes.io/managed-by"] != "tripgoctl" || labels["tripgo.course/environment"] != namespace {
				t.Errorf("%s %s has incomplete ownership labels", file.Name, kind)
			}
			if annotations["tripgo.course/cluster-id"] != testOptions.ClusterID || annotations["tripgo.course/generator-version"] != testOptions.TripgoctlVersion {
				t.Errorf("%s %s has incomplete ownership annotations", file.Name, kind)
			}
			if kind != "Namespace" && metadata["namespace"] != namespace {
				t.Errorf("%s %s namespace = %v", file.Name, kind, metadata["namespace"])
			}
		}
	}
}
