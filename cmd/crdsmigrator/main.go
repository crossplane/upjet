// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	authv1 "k8s.io/api/authorization/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/crossplane/upjet/v2/pkg/config"
)

var (
	app = kingpin.New("crds-migrator", "A CLI tool to manually update CRD storage versions for storage version migration")

	// Global flags
	kubeconfig = app.Flag("kubeconfig", "Path to kubeconfig file. If not specified, uses in-cluster config or default kubeconfig location").String()

	// Update command
	updateCmd      = app.Command("update", "Update CRD status to reflect the current storage version")
	updateCRDNames = updateCmd.Flag("crd-names", "Comma-separated list of CRD:version pairs (e.g., 'buckets.s3.aws.upbound.io:v1beta2,users.iam.aws.upbound.io:v1beta1')").String()
	updateCRDFile  = updateCmd.Flag("crd-file", "Path to YAML file containing CRD to storage version mappings").String()
	updateRetries  = updateCmd.Flag("retries", "Number of retry attempts (must be > 0)").Default("10").Int()
	updateDuration = updateCmd.Flag("retry-duration", "Initial retry duration in seconds (must be > 0)").Default("1").Int()
	updateFactor   = updateCmd.Flag("retry-factor", "Retry backoff factor (must be > 1.0 for exponential growth)").Default("2.0").Float64()
	updateJitter   = updateCmd.Flag("retry-jitter", "Retry jitter between 0.0 and 1.0").Default("0.1").Float64()
	skipPermCheck  = updateCmd.Flag("skip-permission-check", "Skip permission check before updating CRD").Bool()
)

func main() {
	command := kingpin.MustParse(app.Parse(os.Args[1:]))

	if command == updateCmd.FullCommand() {
		if err := runUpdate(); err != nil {
			kingpin.FatalIfError(err, "Failed to update CRD storage")
		}
	}
}

func runUpdate() error { //nolint:gocyclo // easier to follow as a unit
	ctx := context.Background()

	// Parse CRD names and versions from flags
	crdVersionMap, err := parseCRDMappings(*updateCRDNames, *updateCRDFile)
	if err != nil {
		return errors.Wrap(err, "failed to parse CRD mappings")
	}

	if len(crdVersionMap) == 0 {
		return errors.New("no CRD mappings provided. Use --crd-names or --crd-file")
	}

	// Build Kubernetes client configuration
	cfg, err := buildKubeConfig(*kubeconfig)
	if err != nil {
		return errors.Wrap(err, "failed to build kube config")
	}

	// Register the CRD and authorization schemes
	if err := extv1.AddToScheme(scheme.Scheme); err != nil {
		return errors.Wrap(err, "failed to add extensions v1 to scheme")
	}
	if err := authv1.AddToScheme(scheme.Scheme); err != nil {
		return errors.Wrap(err, "failed to add authv1 to scheme")
	}

	// Create a Kubernetes client
	kube, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return errors.Wrap(err, "failed to create kube client")
	}

	// Validate and configure retry backoff
	if err := validateRetryConfig(*updateRetries, *updateDuration, *updateFactor, *updateJitter); err != nil {
		return errors.Wrap(err, "invalid retry configuration")
	}

	retryBackoff := wait.Backoff{
		Duration: time.Duration(*updateDuration) * time.Second,
		Factor:   *updateFactor,
		Jitter:   *updateJitter,
		Steps:    *updateRetries,
	}

	log.Println("Starting CRD storage version migration", "crd-count", len(crdVersionMap))

	// Process each CRD
	var failedCRDs []string
	for crdName, storageVersion := range crdVersionMap {
		log.Println("Processing CRD", "crd-name", crdName, "target-storage-version", storageVersion)

		// Check permissions before attempting the update
		if !*skipPermCheck {
			hasPermission, err := config.CheckCRDStatusUpdatePermission(ctx, kube, crdName)
			if err != nil {
				log.Println("Permission check failed", "crd-name", crdName, "error", err)
				failedCRDs = append(failedCRDs, crdName)
				continue
			}

			if !hasPermission {
				log.Println("WARNING: The current user does not have sufficient permissions to patch CRD status.\n"+
					"Required permissions: patch verb on customresourcedefinitions/status subresource",
					"crd-name", crdName)
				failedCRDs = append(failedCRDs, crdName)
				continue
			}
		}

		// Execute the CRD storage version update
		if err := config.UpdateCRDStorageVersion(ctx, kube, retryBackoff, crdName, storageVersion); err != nil {
			log.Println("Failed to update CRD storage version", "crd-name", crdName, "error", err)
			failedCRDs = append(failedCRDs, crdName)
			continue
		}

		log.Println("Successfully updated CRD storage version", "crd-name", crdName, "storage-version", storageVersion)
	}

	// Report results
	successCount := len(crdVersionMap) - len(failedCRDs)
	log.Println("CRD storage version migration completed", "total", len(crdVersionMap), "successful", successCount, "failed", len(failedCRDs))

	if len(failedCRDs) > 0 {
		log.Println("Failed CRDs", "crd-names", failedCRDs)
		return errors.Errorf("failed to update %d CRD(s): %v", len(failedCRDs), failedCRDs)
	}

	return nil
}

func parseCRDMappings(crdNamesFlag, crdFile string) (map[string]string, error) { //nolint:gocyclo // easier to follow as a unit
	// Validate that only one input method is used
	if crdNamesFlag != "" && crdFile != "" {
		return nil, errors.New("cannot use both --crd-names and --crd-file at the same time. Please use only one")
	}

	crdVersionMap := make(map[string]string)

	// Parse from comma-separated flag (format: "crd1:version1,crd2:version2")
	if crdNamesFlag != "" {
		for _, pair := range strings.Split(crdNamesFlag, ",") {
			trimmed := strings.TrimSpace(pair)
			if trimmed == "" {
				continue
			}

			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) != 2 {
				return nil, errors.Errorf("invalid CRD:version format: %q. Expected format: 'crd-name:storage-version'", trimmed)
			}

			crdName := strings.TrimSpace(parts[0])
			version := strings.TrimSpace(parts[1])

			if crdName == "" || version == "" {
				return nil, errors.Errorf("invalid CRD:version format: %q. Both CRD name and version must be non-empty", trimmed)
			}

			crdVersionMap[crdName] = version
		}
		return crdVersionMap, nil
	}

	// Parse from YAML file (format: crd-name: storage-version)
	if crdFile != "" {
		data, err := os.ReadFile(filepath.Clean(crdFile))
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read CRD file: %s", crdFile)
		}

		if err := yaml.Unmarshal(data, &crdVersionMap); err != nil {
			return nil, errors.Wrapf(err, "failed to parse YAML from file: %s", crdFile)
		}

		return crdVersionMap, nil
	}

	return nil, nil
}

func validateRetryConfig(retries, duration int, factor, jitter float64) error {
	if retries <= 0 {
		return errors.Errorf("retries must be greater than 0, got %d", retries)
	}
	if duration <= 0 {
		return errors.Errorf("retry-duration must be greater than 0, got %d", duration)
	}
	if factor <= 1.0 {
		return errors.Errorf("retry-factor must be greater than 1.0 for exponential backoff, got %.2f", factor)
	}
	if jitter < 0.0 || jitter > 1.0 {
		return errors.Errorf("retry-jitter must be between 0.0 and 1.0, got %.2f", jitter)
	}
	return nil
}

func buildKubeConfig(kubeconfigPath string) (*rest.Config, error) {
	// If kubeconfig path is not specified, use default location
	if kubeconfigPath == "" {
		// Try in-cluster config first
		if cfg, err := rest.InClusterConfig(); err == nil {
			return cfg, nil
		}

		// Fall back to default kubeconfig location
		if home := os.Getenv("HOME"); home != "" {
			kubeconfigPath = filepath.Join(home, ".kube", "config")
		}
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}
