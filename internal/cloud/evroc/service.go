/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package evroc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-logr/logr"
	computev1 "github.com/ravan/cluster-api-provider-evroc/api/v1alpha1/compute"
	networkingv1 "github.com/ravan/cluster-api-provider-evroc/api/v1alpha1/networking"
	infrav1 "github.com/ravan/cluster-api-provider-evroc/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// evrocScheme is a shared scheme with Evroc API types registered
	evrocScheme     *runtime.Scheme
	evrocSchemeOnce sync.Once
)

// getEvrocScheme returns a scheme with Evroc API types registered, initializing it once
func getEvrocScheme() *runtime.Scheme {
	evrocSchemeOnce.Do(func() {
		evrocScheme = runtime.NewScheme()
		_ = computev1.AddToScheme(evrocScheme)
		_ = networkingv1.AddToScheme(evrocScheme)
	})
	return evrocScheme
}

// Service provides access to the Evroc cloud infrastructure API.
// It wraps a Kubernetes client configured to communicate with the Evroc API server.
type Service struct {
	client.Client
	log logr.Logger
}

// New creates a new Evroc Service instance configured with credentials from the EvrocCluster.
// It retrieves the identity secret, loads the kubeconfig, and creates a client configured
// to communicate with the Evroc API server for the specified project.
func New(ctx context.Context, c client.Client, evrocCluster *infrav1.EvrocCluster, log logr.Logger) (*Service, error) {
	log.Info("Creating new evroc service")

	// Get the identity secret containing the kubeconfig
	secret := &corev1.Secret{}
	secretName := types.NamespacedName{
		Namespace: evrocCluster.Namespace,
		Name:      evrocCluster.Spec.IdentitySecretName,
	}
	if err := c.Get(ctx, secretName, secret); err != nil {
		return nil, fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	// Extract kubeconfig from secret
	// Try 'config' first (matches our template), then 'kubeconfig' for compatibility
	kubeconfigData, ok := secret.Data["config"]
	if !ok {
		kubeconfigData, ok = secret.Data["kubeconfig"]
		if !ok {
			return nil, fmt.Errorf("secret %s does not contain 'config' or 'kubeconfig' data", secretName)
		}
	}

	// Write kubeconfig to a file (required by some client-go operations)
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home dir: %w", err)
	}
	kubeconfigPath := filepath.Join(home, ".kube", "evroc-config")
	if err := os.WriteFile(kubeconfigPath, kubeconfigData, 0600); err != nil {
		return nil, fmt.Errorf("failed to write kubeconfig to temp file: %w", err)
	}

	// Load the kubeconfig
	cfg, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig data: %w", err)
	}

	// Override server URL to include project path
	if evrocCluster.Spec.Project != "" {
		for key, cluster := range cfg.Clusters {
			cluster.Server = fmt.Sprintf("%s/clusters/root:%s", cluster.Server, evrocCluster.Spec.Project)
			cfg.Clusters[key] = cluster
		}
	}

	// Create REST config
	restConfig, err := clientcmd.NewDefaultClientConfig(*cfg, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create rest config: %w", err)
	}

	// Create the controller-runtime client with the shared evroc scheme
	evrocClient, err := client.New(restConfig, client.Options{
		Scheme: getEvrocScheme(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create evroc client: %w", err)
	}

	return &Service{
		Client: evrocClient,
		log:    log,
	}, nil
}
