# cluster-api-provider-evroc

A Kubernetes Cluster API infrastructure provider for [Evroc Cloud](https://evroc.com), enabling declarative management of Kubernetes clusters on Evroc infrastructure.

## Overview

This provider implements the Cluster API specification to provision and manage Kubernetes clusters on Evroc Cloud. It handles:

- Network infrastructure (VPCs, subnets)
- Virtual machine lifecycle (create, update, delete)
- Control plane endpoint management
- Integration with RKE2 and kubeadm bootstrap providers

**Bootstrap Provider Status:**
- **RKE2** - ✅ **Recommended** - Stable, works out of the box
- **kubeadm** - ⚠️ **Experimental** - Known etcd stability issues (~6 minutes after init, see [Known Issues](#known-issues))

**Management Options:**
- **Standalone** - Use clusterctl and kubectl to manage clusters
- **Rancher Turtles** - ⚠️ **Experimental** - Manage clusters via Rancher UI (see [Rancher Turtles Integration](#rancher-turtles-integration-experimental))

## Prerequisites

- Go 1.24+
- Docker 17.03+
- kubectl 1.11.3+
- [kind](https://kind.sigs.k8s.io/) for local development
- [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl) CLI tool
- [evroc](https://docs.evroc.com) CLI authenticated and configured
- Access to Evroc Cloud with valid credentials at `~/.evroc/config.yaml`

## Quick Start

### Local Development (RKE2)

1. **Set up environment:**
```bash
# Configure test environment (edit values as needed)
source test/e2e/.env
```

2. **Create management cluster and install CAPI with RKE2:**
```bash
./test/e2e/e2e-test-rke2.sh setup-local
```

3. **Run the provider locally (separate terminal):**
```bash
make run
```

4. **Create a workload cluster:**
```bash
./test/e2e/e2e-test-rke2.sh create-cluster
```

5. **Access workload cluster:**
```bash
export KUBECONFIG=test/e2e/evroc-test-*-kubeconfig.yaml
kubectl get nodes
```

6. **Validate cluster creation:**
```bash
# Check all CAPI resources
kubectl get cluster,evroccluster,machines,evrocmachines -o wide

# Verify API server is accessible
CONTROL_PLANE_IP=$(kubectl get evroccluster -o jsonpath='{.items[0].spec.controlPlaneEndpoint.host}')
curl -k -v --connect-timeout 5 https://${CONTROL_PLANE_IP}:6443/livez

# Check workload cluster components
export KUBECONFIG=test/e2e/evroc-test-*-kubeconfig.yaml
kubectl get nodes
kubectl get pods -A
kubectl cluster-info
```

7. **Clean up:**
```bash
./test/e2e/e2e-test-rke2.sh cleanup
```

### Rancher Turtles Integration (Experimental)

Manage EVROC clusters through Rancher UI using CAPI. ⚠️ Requires manual ProviderID workaround (see Known Issues).

#### Quick Start

1. **Setup Rancher management cluster:**
```bash
./test/e2e/setup-rancher-turtles.sh
source test/e2e/.env  # Configure EVROC_REGION, EVROC_PROJECT, etc.
```

2. **Install and run EVROC provider:**
```bash
make install && kubectl apply -k config/rancher/
make run  # In separate terminal
```

3. **Create cluster with auto-import:**
```bash
export KIND_CLUSTER_NAME="rancher-mgmt"
./test/e2e/e2e-test-rancher.sh run
```

4. **Access Rancher UI:**
```bash
# URL: https://localhost
# Password:
kubectl get secret --namespace cattle-system bootstrap-secret -o go-template='{{.data.bootstrapPassword|base64decode}}{{ "\n" }}'
```

Navigate to **Cluster Management** to see your cluster.

**Monitor:** `./test/e2e/e2e-test-rancher.sh monitor`
**Cleanup:** `./test/e2e/e2e-test-rancher.sh cleanup-all`

#### Template Differences

Template `templates/cluster-template-rancher.yaml` adds:
- Auto-import label: `cluster-api.cattle.io/rancher-auto-import: "true"`
- Node CIDR sizing: `--node-cidr-mask-size=22` (1024 IPs for Rancher agents)
- Cloud provider: `cloudProviderName: external`

#### Known Issues

**CNI IP Exhaustion** - ✅ Resolved via `--node-cidr-mask-size=22` in template

**ProviderID Mismatch** - ⚠️ Requires manual workaround:
```bash
# After cluster creation
PROVIDER_ID=$(kubectl get machine <name> -o jsonpath='{.spec.providerID}')
kubectl --kubeconfig=<workload> patch node <name> -p '{"spec":{"providerID":"'$PROVIDER_ID'"}}'
kubectl --kubeconfig=<workload> taint node <name> node.cloudprovider.kubernetes.io/uninitialized:NoSchedule-
```
**Root Cause:** `cloudProviderName: external` requires EVROC Cloud Controller Manager (CCM) to set providerID and remove taint. CCM implementation planned.

**Files:** `test/e2e/setup-rancher-turtles.sh`, `test/e2e/e2e-test-rancher.sh`, `templates/cluster-template-rancher.yaml`

### Production Deployment

1. **Build and push provider image:**
```bash
make docker-build docker-push IMG=<registry>/cluster-api-provider-evroc:v0.1.0
```

2. **Deploy to management cluster:**
```bash
make deploy IMG=<registry>/cluster-api-provider-evroc:v0.1.0
```

3. **Generate cluster manifest (RKE2):**
```bash
# Set required environment variables
export EVROC_KUBECONFIG_B64=$(cat ~/.evroc/config.yaml | base64 | tr -d '\n')
export EVROC_REGION="eu-central-1"
export EVROC_PROJECT="your-project-id"
export EVROC_VPC_NAME="my-vpc"
export EVROC_SUBNET_NAME="my-subnet"
export EVROC_SUBNET_CIDR="10.0.1.0/24"
export CLUSTER_NAME="my-cluster"
export RKE2_VERSION="v1.31.4+rke2r1"

clusterctl generate cluster ${CLUSTER_NAME} \
  --from templates/cluster-template-rke2.yaml \
  --target-namespace default \
  --kubernetes-version ${RKE2_VERSION} \
  > cluster.yaml

kubectl apply -f cluster.yaml
```

> **Note:** For kubeadm bootstrap (experimental), use `templates/cluster-template.yaml` and `KUBERNETES_VERSION` instead.

4. **Monitor cluster creation:**
```bash
kubectl get cluster ${CLUSTER_NAME}
kubectl get evroccluster ${CLUSTER_NAME}
kubectl get machines -l cluster.x-k8s.io/cluster-name=${CLUSTER_NAME}
```

## Architecture

### Custom Resource Definitions (CRDs)

**EvrocCluster** - Defines cluster-wide infrastructure:
- Network configuration (VPC, subnets)
- Region and project settings
- Control plane endpoint
- Evroc credentials reference

**EvrocMachine** - Represents individual VMs:
- Machine type (e.g., c1a.s, m1a.l)
- Boot disk configuration
- Network attachments
- SSH keys

**EvrocMachineTemplate** - Template for creating machines:
- Used by KubeadmControlPlane and MachineDeployments
- Immutable spec for consistent machine creation

### Controllers

**EvrocClusterReconciler** (`internal/controller/evroccluster_controller.go:217`)
- Provisions VPCs and subnets
- Allocates public IP for control plane
- Sets Cluster API endpoint

**EvrocMachineReconciler** (`internal/controller/evrocmachine_controller.go`)
- Creates and manages VirtualMachine resources
- Handles bootstrap data and cloud-init
- Monitors machine status and updates CAPI Machine

**EvrocMachineTemplateReconciler** (`internal/controller/evrocmachinetemplate_controller.go`)
- Validates template specifications
- No-op controller (templates are immutable)

## Configuration

### Environment Variables

**Required:**
- `EVROC_REGION` - Evroc region (e.g., eu-central-1)
- `EVROC_PROJECT` - Project/ResourceGroup UUID
- `EVROC_KUBECONFIG_B64` - Base64-encoded Evroc kubeconfig

**Optional:**
- `EVROC_VPC_NAME` - VPC name (default: capi-test-vpc)
- `EVROC_SUBNET_NAME` - Subnet name (default: capi-test-subnet)
- `EVROC_SUBNET_CIDR` - Subnet CIDR (default: 10.0.1.0/24)
- `EVROC_CONTROL_PLANE_MACHINE_TYPE` - Control plane VM type (default: c1a.s)
- `EVROC_WORKER_MACHINE_TYPE` - Worker VM type (default: c1a.s)
- `EVROC_IMAGE_NAME` - OS image (default: ubuntu-minimal.24-04.1)
- `EVROC_DISK_SIZE` - Disk size in GB (default: 20)
- `EVROC_SSH_KEY` - Public SSH key for VM access
- `POD_CIDR` - Pod network CIDR (default: 192.168.0.0/16, kubeadm default: 10.244.0.0/16)
- `SERVICE_CIDR` - Service network CIDR (default: 10.96.0.0/12)
- `CONTROL_PLANE_MACHINE_COUNT` - Number of control plane nodes (default: 1)
- `WORKER_MACHINE_COUNT` - Number of worker nodes (default: 0)
- `RKE2_VERSION` - RKE2 version (default: v1.31.4+rke2r1, for RKE2 bootstrap)
- `KUBERNETES_VERSION` - Kubernetes version (default: v1.31.4, for kubeadm bootstrap)

### Credentials

The provider requires an Evroc OIDC kubeconfig stored in a Kubernetes secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: "${CLUSTER_NAME}-evroc-credentials"
  namespace: default
type: Opaque
data:
  config: <base64-encoded-kubeconfig>
```

Reference this secret in the EvrocCluster spec:
```yaml
spec:
  identitySecretName: "${CLUSTER_NAME}-evroc-credentials"
```

## Testing

### Unit Tests
```bash
make test
```

### E2E Tests (requires Evroc access)
```bash
# RKE2 (recommended, stable)
./test/e2e/e2e-test-rke2.sh run

# Or step-by-step for development
source test/e2e/.env
./test/e2e/e2e-test-rke2.sh setup-local     # One-time setup
make run                                     # Run provider locally
./test/e2e/e2e-test-rke2.sh create-cluster  # Create test cluster
./test/e2e/e2e-test-rke2.sh cleanup         # Clean up resources

# Rancher Turtles integration (experimental)
./test/e2e/setup-rancher-turtles.sh         # One-time setup
make install && make run                     # Install CRDs and run provider
./test/e2e/e2e-test-rancher.sh run          # Create cluster with auto-import

# kubeadm (experimental, has etcd issues)
./test/e2e/e2e-test.sh run
```

### Linting
```bash
make lint          # Run linters
make lint-fix      # Auto-fix issues
make lint-config   # Verify config
```

## Common Make Targets

```bash
make build                # Build manager binary
make run                  # Run locally (development)
make docker-build         # Build container image
make docker-push          # Push container image
make deploy              # Deploy to cluster
make undeploy            # Remove from cluster
make install             # Install CRDs
make uninstall           # Remove CRDs
make manifests           # Generate manifests
make generate            # Generate code
make test                # Run unit tests
make e2e-test            # Run E2E tests
make lint                # Run linters
make help                # Show all targets
```

## Cluster Validation

After creating a cluster, you can validate its status using the following commands:

### Validate Management Cluster Resources

```bash
# Switch to management cluster context
kubectl config use-context kind-capi-evroc-e2e

# Check CAPI cluster status
kubectl get cluster
kubectl get evroccluster

# Check machine resources
kubectl get machines -o wide
kubectl get evrocmachines -o wide

# Verify control plane endpoint
kubectl get evroccluster -o jsonpath='{.items[0].spec.controlPlaneEndpoint}'
```

### Validate Workload Cluster Accessibility

```bash
# Get control plane IP
CONTROL_PLANE_IP=$(kubectl get evroccluster -o jsonpath='{.items[0].spec.controlPlaneEndpoint.host}')

# Test API server connectivity (should return 401 Unauthorized)
curl -k -v --connect-timeout 5 https://${CONTROL_PLANE_IP}:6443/livez

# Access workload cluster with kubeconfig
export KUBECONFIG=test/e2e/evroc-test-*-kubeconfig.yaml

# Verify nodes
kubectl get nodes -o wide

# Check system pods
kubectl get pods -A

# Verify cluster info
kubectl cluster-info

# Check RKE2-specific components
kubectl get pods -n kube-system | grep rke2
```

### Validate Network Connectivity

```bash
# From management cluster
kubectl get evrocmachine -o jsonpath='{.items[0].status.addresses}'

# Test external connectivity to control plane
EXTERNAL_IP=$(kubectl get evrocmachine -o jsonpath='{.items[0].status.addresses[?(@.type=="ExternalIP")].address}')
ping -c 3 ${EXTERNAL_IP}
```

## Troubleshooting

### Makefile manifests/generate fails
**Symptom:** `make manifests` or `make install` fails with controller-gen errors about encountering struct fields without JSON tags

**Cause:** The controller-gen tool was scanning directories it shouldn't (like `research/` or other non-code directories).

**Solution:**
The Makefile has been updated to only scan `./api/...` and `./internal/...` directories instead of `./...`. If you encounter this issue:

1. Ensure your Makefile contains:
   ```makefile
   manifests: controller-gen
       $(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./api/...;./internal/..." output:crd:artifacts:config=config/crd/bases

   generate: controller-gen
       $(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/...;./internal/..."
   ```

2. Remove or move any non-Go code directories that might interfere with code generation

### Provider not starting
**Symptom:** Controller manager crashes or won't start

**Solutions:**
1. Check Evroc credentials secret exists:
   ```bash
   kubectl get secret <cluster-name>-evroc-credentials -n default
   ```

2. Verify kubeconfig is valid:
   ```bash
   kubectl get secret <cluster-name>-evroc-credentials -o jsonpath='{.data.config}' | base64 -d
   ```

3. Check controller logs:
   ```bash
   kubectl logs -n cluster-api-provider-evroc-system deployment/cluster-api-provider-evroc-controller-manager
   ```

### Cluster stuck in provisioning
**Symptom:** EvrocCluster shows `Ready: false`

**Solutions:**
1. Check cluster conditions:
   ```bash
   kubectl describe evroccluster <cluster-name>
   ```

2. Verify network resources:
   ```bash
   kubectl get evroccluster <cluster-name> -o yaml | grep -A 20 status
   ```

3. Check if VPC/subnet creation is supported in your region

### Machine not starting
**Symptom:** EvrocMachine stuck in "Provisioning"

**Solutions:**
1. Check machine status:
   ```bash
   kubectl describe evrocmachine <machine-name>
   ```

2. Verify bootstrap data was generated:
   ```bash
   kubectl get secret <machine-name>-bootstrap-data
   ```

3. Check VM status in Evroc (requires SSH access):
   ```bash
   source test/e2e/.env
   test/e2e/ssh-to-vm.sh <machine-name>
   # On VM, check cloud-init:
   sudo cloud-init status
   sudo cat /var/log/cloud-init-output.log
   ```

4. Check image availability:
   ```bash
   # List available images in your region
   evroc compute images list --region <region>
   ```

### No SSH access to VMs
**Symptom:** Cannot SSH to debug cluster issues

**Solution:** SSH keys must be configured before cluster creation:
1. Edit `test/e2e/.env` to set `EVROC_SSH_KEY`
2. Recreate the cluster
3. Use provided helper script:
   ```bash
   test/e2e/ssh-to-vm.sh <machine-name>
   ```

## Known Issues

### kubeadm Bootstrap Provider - etcd Stability Issues

**Status:** ⚠️ Experimental - Not recommended for production

**Symptom:** Clusters using `templates/cluster-template.yaml` (kubeadm bootstrap) experience etcd crashes approximately 6 minutes after `kubeadm init` completes, causing the cluster to enter a "death spiral" where etcd repeatedly crashes and restarts.

**Root Cause:** Under investigation. Likely related to:
- etcd probe timing in virtualized environments (KubeVirt/Evroc Cloud)
- Latency or I/O performance characteristics affecting etcd quorum/leader election
- Interaction between kubelet-finalize phase restarts and etcd initialization

**Debugging Attempts in Template:**
The `templates/cluster-template.yaml` includes extensive workarounds and debugging attempts:
- ✅ Increased etcd probe timeouts (360s initial delay, 60 failure threshold)
- ✅ Patched kube-apiserver probes to use localhost
- ✅ Multiple wait loops for control plane component stabilization
- ✅ 30-second stabilization delay before CNI installation
- ✅ Switched from Calico to Flannel CNI (simpler, less API dependencies)
- ❌ None resolved the ~6-minute etcd crash issue

**Workaround:** Use RKE2 bootstrap provider instead:
- Template: `templates/cluster-template-rke2.yaml`
- Test script: `test/e2e/e2e-test-rke2.sh`
- RKE2 has proven stable in production with no etcd issues

**References:**
- Kubernetes issue [#96886](https://github.com/kubernetes/kubernetes/issues/96886) - kubelet-finalize phase restarts
- etcd issue [#13340](https://github.com/etcd-io/etcd/issues/13340) - probe false positives
- CAPI RKE2 provider: [cluster-api-provider-rke2](https://github.com/rancher-sandbox/cluster-api-provider-rke2)

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make changes following DRY/KISS principles
4. Add/update tests as needed
5. Run `make lint test` to verify
6. Submit a pull request

## License

Copyright 2025. Licensed under the Apache License, Version 2.0.

See [LICENSE](LICENSE) for full license text.
