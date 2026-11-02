# AWS S3 Storage Configuration Guide

## Overview

Griffin utilizes AWS S3 as its primary durable object and file storage backend. Operational metadata, ownership, checksums, and tenant isolation policies remain stored in Supabase PostgreSQL, while file payloads (reports, generated documents, logs, diffs, images, and data exports) reside in private S3 buckets.

---

## 1. Authentication & Credentials

Griffin adheres to the principle of least privilege and strict credential isolation. **Permanent AWS access keys must never be committed to source code or embedded in configuration files.**

### Production Compute (AWS EC2 Cloud Runner)
- **Role**: `GriffinEC2WorkerRole` attached via an EC2 Instance Profile.
- The Go backend automatically retrieves dynamic, short-lived STS credentials from the EC2 Instance Metadata Service (IMDSv2).
- No environment variables or credentials files are required on the host.

### Local Development Compute
- Developers use the AWS CLI with a named profile:
  ```bash
  aws configure --profile griffin-dev
  ```
- Set `AWS_PROFILE=griffin-dev` in `.env`.
- Griffin's `storage.LoadAWSConfig` resolves the credential chain and verifies that valid keys are loaded (`creds.HasKeys()`). If credentials fail to resolve, the engine **fails fast** with `ErrAuthenticationFailed` rather than falling back to anonymous or public access.

---

## 2. Environment Variables

| Variable | Type | Default | Description |
|---|---|---|---|
| `STORAGE_PROVIDER` | string | `s3` | Object storage provider (`s3` for AWS, `mock` for in-memory isolated tests). |
| `S3_BUCKET` | string | `griffin-artifacts` | Target AWS S3 bucket name. |
| `S3_REGION` | string | `ap-south-1` | AWS region where the bucket is hosted. Defaults to `AWS_REGION` if unset. |
| `S3_ENDPOINT` | string | `""` | Custom S3 endpoint URL (used for LocalStack, MinIO, or private VPC endpoints). |
| `S3_FORCE_PATH_STYLE` | bool | `false` | Enable path-style addressing (`https://endpoint/bucket/key`). Useful for MinIO/LocalStack. |
| `AWS_PROFILE` | string | `""` | Named AWS CLI profile for local development. Leave empty in EC2 production. |
| `S3_PRESIGN_EXPIRY_MINUTES` | int | `15` | Default validity window (in minutes) for presigned download URLs. |

---

## 3. Recommended IAM Policies

### EC2 Worker Role Policy (`GriffinS3WorkerPolicy`)
Attach this IAM policy to `GriffinEC2WorkerRole`:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "GriffinS3BucketAccess",
      "Effect": "Allow",
      "Action": [
        "s3:ListBucket",
        "s3:GetBucketLocation"
      ],
      "Resource": "arn:aws:s3:::griffin-artifacts"
    },
    {
      "Sid": "GriffinS3ObjectAccess",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:AbortMultipartUpload",
        "s3:DeleteObject"
      ],
      "Resource": "arn:aws:s3:::griffin-artifacts/*"
    }
  ]
}
```

---

## 4. S3 Bucket Security & Lifecycle Recommendations

1. **Block Public Access**:
   - Enable `BlockPublicAcls`, `IgnorePublicAcls`, `BlockPublicPolicy`, and `RestrictPublicBuckets` on the bucket.
   - All objects must remain strictly private. Direct public URLs are rejected.
2. **Server-Side Encryption (SSE)**:
   - Enforce default encryption using SSE-S3 (`AES256`) or AWS KMS (`aws:kms`).
3. **CORS Configuration**:
   When client applications download directly using presigned URLs:
   ```json
   [
     {
       "AllowedHeaders": ["*"],
       "AllowedMethods": ["GET", "HEAD"],
       "AllowedOrigins": ["https://app.griffin.ai", "http://localhost:3000"],
       "ExposeHeaders": ["ETag", "Content-Length", "Content-Disposition"]
     }
   ]
   ```
4. **Lifecycle Rules**:
   - Abort incomplete multipart uploads after 7 days (`AbortIncompleteMultipartUpload`).
   - Move old artifacts to S3 Standard-Infrequent Access (Standard-IA) after 90 days.
