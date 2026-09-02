package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/cluster"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/environment"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/lab"
)

func newDoctorCommand(deps Dependencies) *cobra.Command {
	var bundlePath string
	command := &cobra.Command{
		Use: "doctor", Short: "Check local prerequisites and produce safe diagnostics", Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.Doctor == nil {
				return errors.New("doctor dependency is required")
			}
			report := deps.Doctor.Diagnose(cmd.Context())
			for _, check := range report.Checks {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%-16s %-7s %s\n", check.Name, check.Status, check.Detail); err != nil {
					return err
				}
			}
			if bundlePath == "" {
				return nil
			}
			data, err := cluster.MarshalDiagnosticBundle(report)
			if err != nil {
				return err
			}
			path, err := filepath.Abs(bundlePath)
			if err != nil {
				return fmt.Errorf("resolve diagnostic bundle path: %w", err)
			}
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- explicit user output path; exclusive creation prevents symlink traversal.
			if err != nil {
				return fmt.Errorf("create diagnostic bundle (choose a new path): %w", err)
			}
			if _, err := file.Write(data); err != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return fmt.Errorf("write diagnostic bundle: %w", err)
			}
			if err := file.Sync(); err != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return fmt.Errorf("sync diagnostic bundle: %w", err)
			}
			if err := file.Close(); err != nil {
				_ = os.Remove(path)
				return fmt.Errorf("close diagnostic bundle: %w", err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Diagnostic bundle: %s\n", path)
			return err
		},
	}
	command.Flags().StringVar(&bundlePath, "bundle", "", "write a secret-free JSON bundle to a new file")
	return command
}

func newClusterCommand(deps Dependencies) *cobra.Command {
	clusterCommand := &cobra.Command{
		Use:   "cluster",
		Short: "Manage the tripgo-local kind cluster",
		Args:  noArgs,
	}

	start := &cobra.Command{
		Use: "start", Short: "Create or reconcile the local cluster", Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := deps.Cluster.Start(cmd.Context())
			if err != nil {
				return err
			}
			if err := printClusterStatus(cmd, status); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "\nNext: cd <lab-directory> && tripgoctl environment start")
			return err
		},
	}
	status := &cobra.Command{
		Use: "status", Short: "Show local cluster status", Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			current, err := deps.Cluster.Inspect(cmd.Context())
			if err != nil {
				return err
			}
			return printClusterStatus(cmd, current)
		},
	}
	var assumeYes bool
	stop := &cobra.Command{
		Use: "stop", Short: "Delete the local cluster and all lab data", Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			current, err := deps.Cluster.Inspect(cmd.Context())
			if err != nil {
				if errors.Is(err, cluster.ErrPrerequisite) {
					if stopErr := deps.Cluster.Stop(cmd.Context(), nil); stopErr != nil {
						return stopErr
					}
					_, writeErr := fmt.Fprintln(cmd.OutOrStdout(), "Cluster tripgo-local is already absent.")
					return writeErr
				}
				return err
			}
			namespaces := append([]string(nil), current.KnownNamespaces...)
			slices.Sort(namespaces)
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Cluster tripgo-local and its local registry will be deleted."); err != nil {
				return err
			}
			if len(namespaces) == 0 {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Environments: none detected"); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Environments:"); err != nil {
					return err
				}
				for _, namespace := range namespaces {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", namespace); err != nil {
						return err
					}
				}
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Warning: all environment PVC data and locally built images will be deleted."); err != nil {
				return err
			}
			if !assumeYes {
				if !deps.IsTerminal() {
					return &exitError{code: 2, err: errors.New("cluster stop requires --yes when stdin is not a terminal")}
				}
				if _, err := fmt.Fprint(cmd.OutOrStdout(), "Type yes to continue: "); err != nil {
					return err
				}
				answer, err := bufio.NewReader(deps.Stdin).ReadString('\n')
				if err != nil && strings.TrimSpace(answer) == "" {
					return fmt.Errorf("read confirmation: %w", err)
				}
				if !isExactYes(answer) {
					return errCanceled
				}
			}
			if err := deps.Cluster.Stop(cmd.Context(), namespaces); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Cluster tripgo-local and its local registry were deleted.")
			return err
		},
	}
	stop.Flags().BoolVar(&assumeYes, "yes", false, "skip destructive operation confirmation")

	clusterCommand.AddCommand(start, status, stop)
	return clusterCommand
}

func printClusterStatus(cmd *cobra.Command, status cluster.Status) error {
	state := "not ready"
	if status.Ready && status.RegistryReady {
		state = "ready"
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Cluster: %s\nStatus:  %s\nKind:    %s\nKubernetes: %s\nRegistry: localhost:5001 (%s)\nIdentity: %s\nPort mappings: %s\nEnvironments: %d known, %d running\n", cluster.Name, state, status.KindVersion, status.Kubernetes, readyWord(status.RegistryReady), status.ClusterID, status.KindConfigHash, len(status.KnownNamespaces), len(status.RunningNamespaces))
	return err
}

func readyWord(ready bool) string {
	if ready {
		return "ready"
	}
	return "not ready"
}

func newEnvironmentCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "environment", Short: "Manage isolated lab environments", Args: noArgs}

	start := &cobra.Command{Use: "start", Short: "Create or reconcile the current lab environment", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cwd, err := environmentWorkingDirectory(deps)
		if err != nil {
			return err
		}
		labNumber, err := deps.Environment.ConfiguredLab(cwd)
		if err != nil {
			return err
		}
		clusterStatus, err := deps.Cluster.Inspect(cmd.Context())
		if err != nil {
			return err
		}
		for _, warning := range deps.Cluster.EnvironmentResourceWarnings(cmd.Context(), labNumber, len(clusterStatus.RunningNamespaces)) {
			if _, err := fmt.Fprintln(cmd.ErrOrStderr(), warning); err != nil {
				return err
			}
		}
		status, err := deps.Environment.Start(cmd.Context(), cwd, deps.Build.Version)
		if err != nil {
			return err
		}
		if err := printEnvironmentStatus(cmd, status); err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "\nNext: tripgoctl connect")
		return err
	}}
	status := &cobra.Command{Use: "status", Short: "Show the current lab environment status", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cwd, err := environmentWorkingDirectory(deps)
		if err != nil {
			return err
		}
		current, err := deps.Environment.Status(cmd.Context(), cwd)
		if err != nil {
			return err
		}
		return printEnvironmentStatus(cmd, current)
	}}
	stop := &cobra.Command{Use: "stop", Short: "Stop workloads and preserve lab data", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cwd, err := environmentWorkingDirectory(deps)
		if err != nil {
			return err
		}
		current, err := deps.Environment.Stop(cmd.Context(), cwd, deps.Build.Version)
		if err != nil {
			return err
		}
		return printEnvironmentStatus(cmd, current)
	}}
	var assumeYes bool
	reset := &cobra.Command{Use: "reset", Short: "Delete the current lab environment and its data", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cwd, err := environmentWorkingDirectory(deps)
		if err != nil {
			return err
		}
		current, err := deps.Environment.Status(cmd.Context(), cwd)
		if err != nil && !errors.Is(err, cluster.ErrPrerequisite) {
			return err
		}
		namespace := current.Namespace
		if namespace == "" {
			config, parseErr := environment.ParseFile(cwd + "/environment.toml")
			if parseErr != nil {
				return parseErr
			}
			namespace, _ = catalog.Namespace(config.Lab)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Environment %s and all its PVC data will be deleted.\n", namespace); err != nil {
			return err
		}
		if !assumeYes {
			if !deps.IsTerminal() {
				return &exitError{code: 2, err: errors.New("environment reset requires --yes when stdin is not a terminal")}
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), "Type yes to continue: "); err != nil {
				return err
			}
			answer, readErr := bufio.NewReader(deps.Stdin).ReadString('\n')
			if readErr != nil && strings.TrimSpace(answer) == "" {
				return fmt.Errorf("read confirmation: %w", readErr)
			}
			if !isExactYes(answer) {
				return errCanceled
			}
		}
		if err := deps.Environment.Reset(cmd.Context(), cwd); err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Environment %s was reset.\n", namespace)
		return err
	}}
	reset.Flags().BoolVar(&assumeYes, "yes", false, "skip destructive operation confirmation")

	var follow bool
	var tail int64
	logs := &cobra.Command{Use: "logs <component>", Short: "Print logs for a component in the current lab", Args: exactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := environmentWorkingDirectory(deps)
		if err != nil {
			return err
		}
		return deps.Environment.Logs(cmd.Context(), cwd, args[0], follow, tail, cmd.OutOrStdout())
	}}
	logs.Flags().BoolVar(&follow, "follow", false, "follow new log records")
	logs.Flags().Int64Var(&tail, "tail", 100, "number of recent lines to show")

	list := &cobra.Command{Use: "list", Short: "List all lab environments", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		items, err := deps.Environment.List(cmd.Context())
		if err != nil {
			return err
		}
		if len(items) == 0 {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Environments: none")
			return err
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "LAB  NAMESPACE       STATE    COMPONENTS"); err != nil {
			return err
		}
		for _, item := range items {
			components := make([]string, 0, len(item.Components))
			for _, component := range item.Components {
				components = append(components, component.Name)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%02d   %-15s %-8s %s\n", item.Lab, item.Namespace, item.State, strings.Join(components, ",")); err != nil {
				return err
			}
		}
		return nil
	}}
	command.AddCommand(start, status, stop, reset, logs, list)
	return command
}

func newConnectCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{Use: "connect", Short: "Print endpoints for the current lab", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cwd, err := environmentWorkingDirectory(deps)
		if err != nil {
			return err
		}
		status, err := deps.Environment.Status(cmd.Context(), cwd)
		if err != nil {
			return err
		}
		if status.State != "ready" {
			return fmt.Errorf("%w: environment %s is %s; run tripgoctl environment start", cluster.ErrPrerequisite, status.Namespace, status.State)
		}
		postgres, _ := catalog.PortByName(status.Lab, "postgres")
		databaseURL := fmt.Sprintf("postgres://tripgo:tripgo@localhost:%d/tripgo?sslmode=disable", postgres.HostPort)
		var output strings.Builder
		fmt.Fprintf(&output, "Environment: %s (ready)\n\nCOMPONENT   ADDRESS\nPostgreSQL  localhost:%d\n", status.Namespace, postgres.HostPort)
		if status.Lab >= 2 {
			grafana, _ := catalog.PortByName(status.Lab, "grafana")
			otelGRPC, _ := catalog.PortByName(status.Lab, "otel-grpc")
			otelHTTP, _ := catalog.PortByName(status.Lab, "otel-http")
			jaeger, _ := catalog.PortByName(status.Lab, "jaeger")
			prometheus, _ := catalog.PortByName(status.Lab, "prometheus")
			fmt.Fprintf(&output, "Grafana     http://localhost:%d\nOTel gRPC   localhost:%d\nOTel HTTP   http://localhost:%d\nJaeger      http://localhost:%d\nPrometheus  http://localhost:%d\n", grafana.HostPort, otelGRPC.HostPort, otelHTTP.HostPort, jaeger.HostPort, prometheus.HostPort)
		}
		if status.Lab >= 3 {
			pushHTTP, _ := catalog.PortByName(status.Lab, "push-http")
			pushGRPC, _ := catalog.PortByName(status.Lab, "push-grpc")
			fmt.Fprintf(&output, "Push HTTP   http://localhost:%d\nPush admin  http://localhost:%d/admin/behaviour\nPush gRPC   localhost:%d\n", pushHTTP.HostPort, pushHTTP.HostPort, pushGRPC.HostPort)
		}
		if status.Lab >= 4 {
			kafka, _ := catalog.PortByName(status.Lab, "redpanda-kafka")
			console, _ := catalog.PortByName(status.Lab, "redpanda-console")
			fmt.Fprintf(&output, "Kafka       localhost:%d\nConsole     http://localhost:%d\n", kafka.HostPort, console.HostPort)
		}
		fmt.Fprintf(&output, "\nDATABASE_URL=%s\npsql %q\n", databaseURL, databaseURL)
		if status.Lab >= 3 {
			fmt.Fprintln(&output, "tripgoctl environment logs push-service --follow")
		}
		if status.Lab >= 4 {
			kafka, _ := catalog.PortByName(status.Lab, "redpanda-kafka")
			fmt.Fprintf(&output, "rpk topic list --brokers localhost:%d\ntripgoctl environment logs redpanda --follow\ntripgoctl environment logs redpanda-console --follow\ntripgoctl environment logs redpanda-topic-reconciler --follow\n", kafka.HostPort)
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), output.String())
		return err
	}}
}

func environmentWorkingDirectory(deps Dependencies) (string, error) {
	if deps.Environment == nil {
		return "", errors.New("environment lifecycle dependency is required")
	}
	cwd, err := deps.WorkingDirectory()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return cwd, nil
}

func isExactYes(answer string) bool {
	answer = strings.TrimSuffix(answer, "\n")
	answer = strings.TrimSuffix(answer, "\r")
	return answer == "yes"
}

func printEnvironmentStatus(cmd *cobra.Command, status lab.RuntimeStatus) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Environment: %s\nLab:         %02d\nStatus:      %s\n", status.Namespace, status.Lab, status.State); err != nil {
		return err
	}
	for _, component := range status.Components {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: desired=%d ready=%d image=%s\n", component.Name, component.Desired, component.Ready, component.ImageDigest); err != nil {
			return err
		}
	}
	return nil
}
