# Storage Architecture: S3 & Supabase Dual-Persistence

## Architecture Overview

Griffin separates operational metadata from heavy object payloads to maintain high database performance, cost efficiency, and strict tenant security:

```
+-------------------------------------------------------------+
|                      Griffin Clients                        |
|             (CLI / Web PWA / Subagents / JJ)                |
+-------------------------------------------------------------+
        |                                       |
        | 1. Upload Stream / Metadata           | 4. Direct Presigned Download
        v                                       v
+-----------------------+              +-----------------------+
|  Griffin HTTP API     |              |    AWS S3 Bucket      |
|  (StorageService)     | -----------> |   (Private gp3)       |
+-----------------------+  2. Stream   +-----------------------+
        |                  to S3                ^
        |                                       |
        v 3. Persist Metadata                   | Compensating Delete
+-----------------------+                       | (if DB fails)
| Supabase PostgreSQL   | ----------------------+
|  (artifacts table)    |
+-----------------------+
```

---

## 1. Provider-Neutral Storage Contract

The core storage package (`internal/storage`) defines a pluggable, provider-neutral contract:

```go
type Provider interface {
	Upload(ctx context.Context, input UploadInput) (*UploadOutput, error)
	Download(ctx context.Context, input DownloadInput) (*DownloadOutput, error)
	Delete(ctx context.Context, input DeleteInput) error
	Head(ctx context.Context, input HeadInput) (*ObjectInfo, error)
	List(ctx context.Context, input ListInput) (*ListOutput, error)
	PresignDownloadURL(ctx context.Context, input PresignInput) (*PresignOutput, error)
	Name() string
}
```

Two concrete implementations are provided:
1. `S3StorageProvider`: Production implementation utilizing AWS SDK for Go v2 (`github.com/aws/aws-sdk-go-v2/service/s3`).
2. `MockStorageProvider`: Thread-safe in-memory provider for unit tests and isolated local development without cloud dependencies.

---

## 2. Object Key Hierarchy & Task Scoping

All objects in Griffin are partitioned predictably:

- **Task-Scoped Outputs**:
  `tasks/{task_id}/outputs/{sanitized_filename}`
- **Global / Unscoped Artifacts**:
  `artifacts/{uuid}/{sanitized_filename}`

Filenames are sanitized using `filepath.Base` and space-normalization to eliminate path traversal vulnerabilities (`../../`).

---

## 3. Compensating Saga Pattern for Partial Failures

Cross-boundary transactions between object stores (AWS S3) and relational databases (PostgreSQL) cannot use two-phase commit (2PC). Griffin implements a **Compensating Saga Pattern**:

1. **Object Upload First**: The stream is buffered, its SHA-256 checksum and MIME type are computed on the fly, and the payload is uploaded to S3.
2. **Metadata Persistence**: The metadata record is written to PostgreSQL within the user's RLS tenant boundary.
3. **Compensating Rollback**: If PostgreSQL persistence fails (network partition, unique constraint violation, DB outage), `StorageService` immediately issues a compensating `DeleteObject` request against S3 using a detached background context. This prevents orphaned objects from accumulating silently in cloud storage.
4. **Idempotent Deletion**: When an artifact is deleted, the S3 object is removed first, followed by the PostgreSQL record. S3 404 (Not Found) errors are treated idempotently.

---

## 4. Multipart Streaming & Large Object Handling

For large files, streaming outputs, and memory conservation:
- `S3StorageProvider` leverages the AWS SDK v2 `manager.Uploader`.
- Chunks are configured with a 5 MB part size threshold.
- Small payloads (<5MB) execute in a single `PutObject` call.
- Streaming payloads ($\ge$ 5MB or unknown lengths) automatically utilize concurrent multipart uploads and initiate `AbortMultipartUpload` upon client disconnection or transfer error.

---

## 5. Security & Tenant Isolation

- **Private Objects Only**: S3 objects are never marked public. Bucket policies enforce private read-only access restricted to the application role.
- **Presigned URLs**: Time-limited presigned GET URLs (default 15 minutes) allow authorized clients to download files directly from S3 without passing bytes through the API server twice.
- **Database RLS Enforced**: Access to artifacts is strictly gated by PostgreSQL Row Level Security (`auth.uid() = user_id`), ensuring tenants cannot view, download, or presign artifacts belonging to other accounts.
