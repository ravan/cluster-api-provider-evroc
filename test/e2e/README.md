# End-to-End Testing Guide

This directory contains end-to-end (e2e) tests for the Cluster API Provider Evroc. The e2e tests create a real Kubernetes cluster on Evroc Cloud infrastructure, managed by a local kind management cluster running our CAPI provider.

## Overview

The e2e test flow:

1. **Create Management Cluster**: Sets up a local kind cluster
2. **Install Prerequisites**: Installs cert-manager and Cluster API core components
3. **Deploy Provider**: Builds and deploys the Evroc CAPI provider
4. **Create Workload Cluster**: Creates a real Kubernetes cluster on Evroc Cloud
5. **Validate**: Verifies the workload cluster is functional
6. **Cleanup**: Removes all resources

## Prerequisites

### Required Tools

Install the following tools before running e2e tests:

```bash
# kind - for local Kubernetes cluster
brew install kind  # macOS
# or
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.30.0/kind-darwin-arm64
chmod +x ./kind
sudo mv ./kind /usr/local/bin/kind

# kubectl - Kubernetes CLI
brew install kubectl

# clusterctl - Cluster API CLI
brew install clusterctl

# docker - Container runtime
brew install --cask docker

# evroc - Evroc Cloud CLI
# Follow instructions at https://docs.evroc.com/cli/installation
```

### Evroc Cloud Access

You need an Evroc Cloud account with:

1. **Valid credentials**: Login using `evroc login`
2. **API access**: Your config at `~/.evroc/config.yaml`
3. **Sufficient quota**: At least 3 VMs (1 control plane + 2 workers)

Verify your access:

```bash
evroc compute virtual-machines list
```

### Required Evroc Resources

Before running tests, ensure these resources exist or will be created:

#### Option 1: Use Existing Resources

```bash
# List available VPCs
evroc networking vpcs list

# List available disk images
evroc compute disk-images list

# List available machine types
evroc compute virtual-resource-types list
```

#### Option 2: Let the Provider Create Resources

The provider will create:
- VPC (if `EVROC_VPC_NAME` doesn't exist)
- Subnet (if `EVROC_SUBNET_NAME` doesn't exist)

You still need to specify:
- Valid machine types (e.g., `c1a.s`, `m1a.l`)
- Valid disk image (e.g., `ubuntu-minimal.24-04.1`)

## Quick Start

### 1. Configure Environment

Copy the environment template and fill in your values:

```bash
cd test/e2e
cp env.template .env
vi .env  # Edit with your values
```

Key configuration points in `.env`:

```bash
export CLUSTER_NAME="my-test-cluster"
export EVROC_VPC_NAME="my-vpc"
export EVROC_SUBNET_NAME="my-subnet"
export EVROC_SUBNET_CIDR="10.0.1.0/24"
export EVROC_CONTROL_PLANE_MACHINE_TYPE="c1a.s"
export EVROC_WORKER_MACHINE_TYPE="c1a.s"
export EVROC_IMAGE_NAME="ubuntu-minimal.24-04.1"

# SSH keys are auto-detected from ~/.ssh/id_ed25519 or ~/.ssh/id_rsa
# If you don't have these, generate one first:
# ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519
```

### 2. Run the E2E Test

From the repository root:

```bash
# Source your environment
source test/e2e/.env

# Run the full e2e test
make e2e-test
```

Or run the script directly:

```bash
./test/e2e/e2e-test.sh run
```

### 3. Access the Workload Cluster

After the test completes successfully:

```bash
# The kubeconfig is saved to:
export KUBECONFIG=test/e2e/my-test-cluster-kubeconfig.yaml

# Check cluster nodes
kubectl get nodes

# Check pods
kubectl get pods -A

# Deploy a test application
kubectl create deployment nginx --image=nginx
kubectl expose deployment nginx --port=80
```

### 4. Clean Up

When you're done testing:

```bash
# Clean up everything
make e2e-cleanup
```

Or:

```bash
./test/e2e/e2e-test.sh cleanup
```

## Make Targets

### `make e2e-test`

Runs the complete e2e test flow. This is the recommended way to run tests.

```bash
make e2e-test
```

### `make e2e-setup`

Sets up the management cluster but doesn't create a workload cluster. Useful for manual testing.

```bash
make e2e-setup

# Then manually create a cluster
kubectl apply -f my-cluster.yaml
```

### `make e2e-cleanup`

Cleans up both management and workload clusters.

```bash
make e2e-cleanup
```

### `make build-provider-image`

Builds the provider Docker image for local testing.

```bash
make build-provider-image
```

### `make generate-cluster`

Generates a cluster manifest from the template without creating it.

```bash
make generate-cluster CLUSTER_NAME=my-cluster
```

This creates `cluster-my-cluster.yaml` that you can inspect or modify before applying.

## Environment Variables

### Management Cluster

| Variable | Default | Description |
|----------|---------|-------------|
| `KIND_CLUSTER_NAME` | `capi-evroc-e2e` | Name of the kind management cluster |

### Workload Cluster

| Variable | Default | Description |
|----------|---------|-------------|
| `CLUSTER_NAME` | `evroc-test-cluster` | Name of the workload cluster |
| `KUBERNETES_VERSION` | `v1.31.4` | Kubernetes version to install |
| `CONTROL_PLANE_MACHINE_COUNT` | `1` | Number of control plane nodes |
| `WORKER_MACHINE_COUNT` | `1` | Number of worker nodes |

### Evroc Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `EVROC_CONFIG` | `~/.evroc/config.yaml` | Path to Evroc credentials |
| `EVROC_VPC_NAME` | `capi-test-vpc` | VPC name in Evroc Cloud |
| `EVROC_SUBNET_NAME` | `capi-test-subnet` | Subnet name |
| `EVROC_SUBNET_CIDR` | `10.0.1.0/24` | Subnet CIDR block |
| `EVROC_CONTROL_PLANE_MACHINE_TYPE` | `c1a.s` | Machine type for control plane |
| `EVROC_WORKER_MACHINE_TYPE` | `c1a.s` | Machine type for workers |
| `EVROC_IMAGE_NAME` | `ubuntu-minimal.24-04.1` | Disk image name |
| `EVROC_DISK_SIZE` | `20` | Disk size in GB |
| `EVROC_SSH_KEY` | (auto-detected) | SSH public key for VM access (auto-detected from ~/.ssh) |
| `EVROC_SSH_PRIVATE_KEY` | (auto-detected) | SSH private key path for connecting to VMs |

### Cluster Networking

| Variable | Default | Description |
|----------|---------|-------------|
| `POD_CIDR` | `192.168.0.0/16` | Pod network CIDR |
| `SERVICE_CIDR` | `10.96.0.0/12` | Service network CIDR |

## SSH Access to VMs

The provider supports SSH access to VMs for debugging and troubleshooting. SSH keys are configured during VM creation and added to the `evroc-user` account.

### Configuring SSH Access

SSH access is configured via the `.env` file. The configuration automatically detects your default SSH key.

#### Automatic Configuration (Recommended)

When you source the `.env` file, it will automatically detect and use your default SSH key:

```bash
source test/e2e/.env
```

The script checks for SSH keys in this order:
1. `~/.ssh/id_ed25519` (preferred)
2. `~/.ssh/id_rsa`

If found, it sets both:
- `EVROC_SSH_KEY`: The public key (added to VMs)
- `EVROC_SSH_PRIVATE_KEY`: The private key path (used for SSH connections)

#### Manual Configuration

If you want to use a specific key or don't have a default key:

1. **Generate a new SSH key pair** (if needed):

```bash
ssh-keygen -t ed25519 -C "evroc-e2e" -f ~/.ssh/id_ed25519
```

2. **Edit `.env`** to use your specific key:

```bash
# In test/e2e/.env, uncomment and modify:
export EVROC_SSH_KEY="$(cat ~/.ssh/my-custom-key.pub)"
export EVROC_SSH_PRIVATE_KEY="${HOME}/.ssh/my-custom-key"
```

**Important Notes:**
- The SSH key must be configured **before** creating VMs
- SSH keys cannot be added after VM creation
- The public key is added to the `evroc-user` account on all VMs
- Supported key types: ssh-rsa, ssh-ed25519, ecdsa variants

### Connecting to VMs

#### Option 1: Using the Helper Script (Recommended)

The repository includes a helper script that automatically discovers VM IP addresses and uses the SSH key from your `.env` configuration:

```bash
# First, source the .env file to load SSH key configuration
source test/e2e/.env

# Connect to a control plane machine (uses EVROC_SSH_PRIVATE_KEY automatically)
./test/e2e/ssh-to-vm.sh my-cluster-control-plane-xxxxx

# Connect to a worker machine
./test/e2e/ssh-to-vm.sh my-cluster-worker-xxxxx

# Execute a command on a VM
./test/e2e/ssh-to-vm.sh -c "sudo systemctl status kubelet" my-cluster-control-plane-xxxxx

# Override with a specific SSH key (if needed)
./test/e2e/ssh-to-vm.sh -k ~/.ssh/custom-key my-cluster-worker-xxxxx

# Connect to VM in a specific project
./test/e2e/ssh-to-vm.sh -p production my-cluster-control-plane-xxxxx
```

The helper script will:
- Automatically use `EVROC_SSH_PRIVATE_KEY` from your `.env` file
- Discover the VM's public IP from Kubernetes resources
- Connect using the correct username (`evroc-user`)

#### Option 2: Manual SSH Connection

1. Get the VM's public IP address:

```bash
# From EvrocMachine
kubectl get evrocmachine my-cluster-control-plane-xxxxx -o jsonpath='{.status.addresses[?(@.type=="ExternalIP")].address}'

# Or from VirtualMachine (in the Evroc project namespace)
kubectl get virtualmachine my-cluster-control-plane-xxxxx -n <project> -o jsonpath='{.status.networking.publicIPv4Address}'
```

2. SSH to the VM:

```bash
ssh -i ~/.ssh/evroc_vm_key evroc-user@<public-ip>
```

### Common SSH Tasks

#### Check kubelet status
```bash
./test/e2e/ssh-to-vm.sh my-cluster-control-plane-xxxxx -c "sudo systemctl status kubelet"
```

#### View cloud-init logs
```bash
./test/e2e/ssh-to-vm.sh my-cluster-control-plane-xxxxx -c "sudo cat /var/log/cloud-init-output.log"
```

#### Check cluster join status
```bash
./test/e2e/ssh-to-vm.sh my-cluster-control-plane-xxxxx -c "sudo journalctl -u kubelet -f"
```

#### Verify network connectivity
```bash
./test/e2e/ssh-to-vm.sh my-cluster-worker-xxxxx -c "ping -c 3 8.8.8.8"
```

### Troubleshooting SSH Issues

#### SSH key not working

Verify the key was configured before VM creation:

```bash
# Check if SSH key is set in EvrocMachine spec
kubectl get evrocmachine my-cluster-control-plane-xxxxx -o jsonpath='{.spec.sshKey}'
```

If empty, the key was not configured. You'll need to recreate the VM with SSH key configured.

#### Connection refused or timeout

1. Verify the VM has a public IP:
```bash
kubectl get evrocmachine my-cluster-control-plane-xxxxx -o jsonpath='{.status.addresses}'
```

2. Check security groups allow SSH (port 22):
```bash
kubectl get evrocmachine my-cluster-control-plane-xxxxx -o jsonpath='{.spec.securityGroups}'
```

3. Verify the VM is running:
```bash
kubectl get virtualmachine my-cluster-control-plane-xxxxx -n <project> -o jsonpath='{.status.virtualMachineStatus}'
```

#### Permission denied (publickey)

- Ensure you're using the correct SSH key: `-i ~/.ssh/evroc_vm_key`
- Verify you're using the correct username: `evroc-user` (not `root` or `ubuntu`)
- Confirm the public key matches what was configured in `EVROC_SSH_KEY`

## Local Development Workflow (Recommended)

For faster development iterations, you can run the operator locally instead of building and loading Docker images. This provides instant code changes and better debugging:

### Quick Start

```bash
# 1. Source environment variables
cd test/e2e
source .env

# 2. Set up the management cluster (one-time setup)
./e2e-test.sh setup-local

# 3. In a separate terminal, run the operator locally
cd ../..
make run

# 4. Create test clusters (can repeat this step as you develop)
cd test/e2e
source .env
./e2e-test.sh create-cluster

# 5. Clean up when done
./e2e-test.sh cleanup
```

### Benefits

- **Faster iterations**: No Docker build/load cycle (saves 2-3 minutes per iteration)
- **Instant code changes**: Just restart `make run` to test changes
- **Better debugging**: See logs directly in terminal, use debugger, etc.
- **Local breakpoints**: Use your IDE debugger with the running process

### Available Commands

```bash
./e2e-test.sh setup-local    # Set up kind cluster, CRDs, and CAPI components
./e2e-test.sh create-cluster # Create a workload cluster (requires operator running)
./e2e-test.sh cleanup        # Clean up all resources
./e2e-test.sh run            # Full e2e test with in-cluster operator (CI mode)
```

### Development Cycle

1. Make code changes
2. Stop `make run` (Ctrl+C)
3. Restart `make run`
4. Test with `./e2e-test.sh create-cluster`
5. Repeat

No need to rebuild images or reload into kind!

## Manual Testing Workflow

For development and debugging, you may want to run steps manually:

### Step 1: Create Management Cluster

```bash
kind create cluster --name capi-evroc-e2e
kubectl cluster-info
```

### Step 2: Install cert-manager

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml

# Wait for cert-manager
kubectl wait --for=condition=Available --timeout=5m \
  -n cert-manager deployment/cert-manager \
  deployment/cert-manager-cainjector \
  deployment/cert-manager-webhook
```

### Step 3: Build and Load Provider Image

```bash
# From repo root
make docker-build IMG=controller:v0.1.0
kind load docker-image controller:v0.1.0 --name capi-evroc-e2e
```

### Step 4: Initialize clusterctl

```bash
# Create clusterctl config
mkdir -p ~/.cluster-api
cat > ~/.cluster-api/clusterctl.yaml <<EOF
providers:
  - name: "evroc"
    url: "$(pwd)/config/default"
    type: "InfrastructureProvider"
EOF

# Initialize Cluster API
clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure evroc
```

### Step 5: Create Evroc Credentials Secret

```bash
kubectl create secret generic my-cluster-evroc-credentials \
  --from-file=config=${HOME}/.evroc/config.yaml

kubectl label secret my-cluster-evroc-credentials \
  "cluster.x-k8s.io/cluster-name=my-cluster"
```

### Step 6: Generate and Apply Cluster

```bash
# Set environment variables (see env.template)
source test/e2e/.env

# Generate cluster manifest
clusterctl generate cluster my-cluster \
  --from templates/cluster-template.yaml \
  --target-namespace default \
  --kubernetes-version v1.31.4 \
  > my-cluster.yaml

# Apply it
kubectl apply -f my-cluster.yaml
```

### Step 7: Watch Cluster Creation

```bash
# Watch cluster status
watch kubectl get cluster,evroccluster,machines

# Check controller logs
kubectl logs -n capi-evroc-system deployment/capi-evroc-controller-manager -f

# Get detailed status
kubectl describe cluster my-cluster
kubectl describe evroccluster my-cluster
```

### Step 8: Get Workload Cluster Kubeconfig

```bash
# Wait for kubeconfig secret
kubectl wait --for=jsonpath='{.data.value}' \
  --timeout=10m \
  secret/my-cluster-kubeconfig

# Save kubeconfig
kubectl get secret my-cluster-kubeconfig \
  -o jsonpath='{.data.value}' | base64 -d \
  > my-cluster-kubeconfig.yaml

# Access workload cluster
export KUBECONFIG=my-cluster-kubeconfig.yaml
kubectl get nodes
```

## Troubleshooting

### Provider Not Starting

**Symptoms**: Provider pods are in CrashLoopBackOff

**Solution**:
```bash
# Check provider logs
kubectl logs -n capi-evroc-system deployment/capi-evroc-controller-manager

# Verify image was loaded
docker images | grep controller

# Reload image
kind load docker-image controller:v0.1.0 --name capi-evroc-e2e

# Restart deployment
kubectl rollout restart -n capi-evroc-system deployment/capi-evroc-controller-manager
```

### Cluster Stuck in Provisioning

**Symptoms**: Cluster remains in "Provisioning" phase

**Solution**:
```bash
# Check machine status
kubectl get machines -o wide

# Check EvrocMachine status
kubectl get evrocmachines -o yaml

# Check controller logs
kubectl logs -n capi-evroc-system deployment/capi-evroc-controller-manager -f

# Check for errors in conditions
kubectl describe evrocmachine <machine-name>
```

### Evroc API Errors

**Symptoms**: Controller logs show Evroc API errors

**Solution**:
```bash
# Verify Evroc credentials
evroc compute virtual-machines list

# Check if credentials secret is correct
kubectl get secret my-cluster-evroc-credentials -o yaml

# Verify network resources exist
evroc networking vpcs list
evroc networking subnets list --vpc-name <vpc-name>

# Check quota
evroc quotas list
```

### VMs Created but Not Joining

**Symptoms**: VMs are running in Evroc but nodes don't join

**Solution**:
```bash
# SSH into VM (if SSH key was provided)
ssh evroc-user@<vm-ip>

# Check cloud-init logs
sudo cat /var/log/cloud-init.log
sudo cat /var/log/cloud-init-output.log

# Check kubelet status
sudo systemctl status kubelet
sudo journalctl -u kubelet -f

# Verify network connectivity
ping 8.8.8.8
curl -k https://<control-plane-ip>:6443
```

### Cannot Access Workload Cluster

**Symptoms**: `kubectl get nodes` times out

**Solution**:
```bash
# Verify control plane endpoint
kubectl get cluster my-cluster -o jsonpath='{.spec.controlPlaneEndpoint}'

# Check if control plane VM has public IP
kubectl get evrocmachine -o yaml | grep publicIP

# Verify kubeconfig is correct
cat my-cluster-kubeconfig.yaml

# Check if API server is running
ssh evroc-user@<control-plane-ip> sudo systemctl status kubelet
```

### Cleanup Issues

**Symptoms**: Resources remain after cleanup

**Solution**:
```bash
# Force delete cluster
kubectl delete cluster my-cluster --force --grace-period=0

# Manually delete Evroc resources
evroc compute virtual-machines delete <vm-name>
evroc networking subnets delete <subnet-name> --vpc-name <vpc-name>
evroc networking vpcs delete <vpc-name>

# Delete kind cluster
kind delete cluster --name capi-evroc-e2e

# Clean up local files
rm -f test/e2e/*.yaml test/e2e/.env
```

## Test Configuration

The e2e tests use the following configuration files:

- **`e2e-config.yaml`**: Default test configuration values
- **`env.template`**: Environment variable template
- **`e2e-test.sh`**: Main test script
- **`templates/cluster-template.yaml`**: Cluster manifest template

## CI/CD Integration

To run e2e tests in CI/CD:

```yaml
# Example GitHub Actions workflow
name: E2E Tests
on: [push, pull_request]

jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Evroc credentials
        run: |
          mkdir -p ~/.evroc
          echo "${{ secrets.EVROC_CONFIG }}" > ~/.evroc/config.yaml

      - name: Run E2E tests
        run: make e2e-test
        env:
          CLUSTER_NAME: ci-test-${{ github.run_id }}
          EVROC_VPC_NAME: ci-vpc
          EVROC_SUBNET_NAME: ci-subnet

      - name: Cleanup
        if: always()
        run: make e2e-cleanup
```

## Advanced Usage

### Testing Different Kubernetes Versions

```bash
KUBERNETES_VERSION=v1.30.0 make e2e-test
```

### Testing with Multiple Control Planes

```bash
CONTROL_PLANE_MACHINE_COUNT=3 make e2e-test
```

### Testing with Different Machine Types

```bash
EVROC_CONTROL_PLANE_MACHINE_TYPE=m1a.l \
EVROC_WORKER_MACHINE_TYPE=c1a.m \
make e2e-test
```

### Debugging Provider Code

```bash
# Build provider with debug symbols
make docker-build IMG=controller:debug

# Load into kind
kind load docker-image controller:debug --name capi-evroc-e2e

# Update deployment to use debug image
kubectl set image -n capi-evroc-system deployment/capi-evroc-controller-manager \
  manager=controller:debug

# Attach debugger or increase log verbosity
kubectl edit deployment -n capi-evroc-system capi-evroc-controller-manager
# Add: --v=4 to container args
```

## Resources

- [Cluster API Book](https://cluster-api.sigs.k8s.io/)
- [clusterctl Documentation](https://cluster-api.sigs.k8s.io/clusterctl/overview)
- [kind Documentation](https://kind.sigs.k8s.io/)
- [Evroc Cloud Documentation](https://docs.evroc.com/)

## Contributing

When adding new features to the provider, make sure to:

1. Update the cluster template if new fields are added
2. Add environment variables to `env.template`
3. Update this README with new configuration options
4. Test with `make e2e-test` before submitting PR
