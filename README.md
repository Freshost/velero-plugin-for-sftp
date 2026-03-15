# Velero Plugin for SFTP

[![CI](https://github.com/Freshost/velero-plugin-for-sftp/actions/workflows/ci.yml/badge.svg)](https://github.com/Freshost/velero-plugin-for-sftp/actions/workflows/ci.yml)

A [Velero](https://velero.io) ObjectStore plugin that stores backups on any SFTP server. Built for use with [Hetzner Storage Box](https://www.hetzner.com/storage/storage-box/) and similar SFTP-accessible storage.

Supports optional client-side encryption using [age](https://age-encryption.org/), so backup data is encrypted before it leaves the cluster.

## Compatibility

| Plugin Version | Velero Version |
|----------------|----------------|
| v0.1.x         | v1.17.x        |

## Features

- **Direct SFTP backup storage** -- no S3 proxy (MinIO, SeaweedFS) needed
- **Age encryption** -- optional client-side encryption with X25519 or post-quantum keys
- **Auto-reconnect** -- recovers from dropped SSH connections
- **Multi-arch** -- linux/amd64 and linux/arm64
- **Streaming I/O** -- no local disk buffering, data streams directly to the SFTP server

## Installation

```bash
velero install --plugins ghcr.io/freshost/velero-plugin-for-sftp:v0.1.0 ...
```

Or add to an existing Velero deployment:

```bash
velero plugin add ghcr.io/freshost/velero-plugin-for-sftp:v0.1.0
```

## Configuration

### 1. Create a credentials file

The credentials file is a YAML file containing your SFTP authentication details:

```yaml
user: u123456
privateKey: |
  -----BEGIN OPENSSH PRIVATE KEY-----
  b3BlbnNzaC1rZXktdjEAAAAA...
  -----END OPENSSH PRIVATE KEY-----
knownHosts: |
  [u123456.your-storagebox.de]:23 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA...
```

Password authentication is also supported:

```yaml
user: u123456
password: your-password
knownHosts: |
  [u123456.your-storagebox.de]:23 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA...
```

All supported fields:

| Field | Required | Description |
|---|---|---|
| `user` | Yes | SSH username |
| `privateKey` | Yes* | SSH private key (PEM format) |
| `password` | Yes* | SSH password |
| `privateKeyPassphrase` | No | Passphrase for encrypted private keys |
| `knownHosts` | No | SSH known_hosts entries for host key verification |

\* At least one of `privateKey` or `password` is required.

### 2. Create a Kubernetes Secret

```bash
kubectl create secret generic sftp-credentials \
  --namespace velero \
  --from-file=credentials=./sftp-credentials.yaml
```

### 3. Create a BackupStorageLocation

```yaml
apiVersion: velero.io/v1
kind: BackupStorageLocation
metadata:
  name: sftp
  namespace: velero
spec:
  provider: velero.io/sftp
  credential:
    name: sftp-credentials
    key: credentials
  objectStorage:
    bucket: velero-backups
  config:
    host: u123456.your-storagebox.de
    port: "23"
    basePath: /home/backups
```

### BSL Config Options

| Key | Required | Default | Description |
|---|---|---|---|
| `host` | Yes | | SFTP server hostname |
| `port` | No | `22` | SFTP server port |
| `basePath` | No | `/` | Root directory on the SFTP server |
| `timeout` | No | `30s` | SSH connection timeout (Go duration) |
| `insecureSkipHostKeyVerification` | No | `false` | Skip SSH host key verification |
| `encryptionKeyPath` | No | | Path to age identity file for encryption |

## Encryption

Backups can be encrypted client-side using [age](https://age-encryption.org/) before uploading to the SFTP server.

### Setup

Generate an age key:

```bash
age-keygen -o age-identity.txt
```

For post-quantum resistant encryption:

```bash
age-keygen -pq -o age-identity.txt
```

Create a Secret and mount it into the Velero deployment:

```bash
kubectl create secret generic age-encryption-key \
  --namespace velero \
  --from-file=identity=./age-identity.txt
```

Add the volume mount to your Velero deployment (e.g. via Helm values):

```yaml
# Helm values.yaml
configuration:
  extraVolumes:
    - name: age-key
      secret:
        secretName: age-encryption-key
  extraVolumeMounts:
    - name: age-key
      mountPath: /age
      readOnly: true
```

Then reference it in BSL config:

```yaml
config:
  encryptionKeyPath: /age/identity
```

### Decrypting backups manually

Download a backup file from the SFTP server and decrypt it locally:

```bash
age -d -i age-identity.txt backup.tar.gz > backup-decrypted.tar.gz
```

## Hetzner Storage Box Example

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: hetzner-sftp
  namespace: velero
stringData:
  credentials: |
    user: u123456
    privateKey: |
      -----BEGIN OPENSSH PRIVATE KEY-----
      b3BlbnNzaC1rZXktdjEAAAAA...
      -----END OPENSSH PRIVATE KEY-----
    knownHosts: |
      [u123456.your-storagebox.de]:23 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA...
---
apiVersion: velero.io/v1
kind: BackupStorageLocation
metadata:
  name: hetzner
  namespace: velero
spec:
  provider: velero.io/sftp
  credential:
    name: hetzner-sftp
    key: credentials
  objectStorage:
    bucket: velero-backups
  config:
    host: u123456.your-storagebox.de
    port: "23"
    basePath: /home
```

## Building from Source

```bash
make build      # build binary
make test       # run tests with coverage
make lint       # run linter
make container  # build Docker image
make ci         # verify modules + test
```

## Filing Issues

If you encounter a bug, have a feature request, or need help, please [open an issue](https://github.com/Freshost/velero-plugin-for-sftp/issues).

## License

Apache 2.0 -- see [LICENSE](LICENSE) for details.
