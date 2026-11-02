package storage

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3ClientOptions defines configuration for connecting to AWS S3.
type S3ClientOptions struct {
	Region         string
	Bucket         string
	AWSProfile     string
	Endpoint       string
	ForcePathStyle bool
}

// S3ClientAPI abstracts the s3.Client operations needed by Griffin.
type S3ClientAPI interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// S3UploadAPI abstracts upload operations (satisfied by *manager.Uploader).
type S3UploadAPI interface {
	Upload(ctx context.Context, input *s3.PutObjectInput, options ...func(*manager.Uploader)) (*manager.UploadOutput, error)
}

// S3PresignAPI abstracts presigned URL generation (satisfied by *s3.PresignClient).
type S3PresignAPI interface {
	PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// LoadAWSConfig loads the AWS SDK v2 configuration following Griffin requirements:
// 1. Configured AWS region (defaults to ap-south-1).
// 2. Named AWS profile if provided (e.g. for local dev).
// 3. Instance profile / environment variables fallback (for EC2 worker role).
// 4. Verifies credentials resolve or fails fast (never falls back to anonymous).
func LoadAWSConfig(ctx context.Context, opts S3ClientOptions) (aws.Config, error) {
	var cfgOpts []func(*config.LoadOptions) error

	if opts.Region != "" {
		cfgOpts = append(cfgOpts, config.WithRegion(opts.Region))
	} else {
		cfgOpts = append(cfgOpts, config.WithRegion("ap-south-1"))
	}

	if opts.AWSProfile != "" {
		cfgOpts = append(cfgOpts, config.WithSharedConfigProfile(opts.AWSProfile))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, cfgOpts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("%w: failed to load default config: %v", ErrAuthenticationFailed, err)
	}

	// Verify credentials can be retrieved (fail fast if no credentials exist)
	creds, err := awsCfg.Credentials.Retrieve(ctx)
	if err != nil || !creds.HasKeys() {
		return aws.Config{}, fmt.Errorf("%w: no valid AWS credentials found in profile or environment", ErrAuthenticationFailed)
	}

	return awsCfg, nil
}

// NewS3RawClient creates an authenticated *s3.Client with optional custom endpoint and path style.
func NewS3RawClient(ctx context.Context, opts S3ClientOptions) (*s3.Client, error) {
	awsCfg, err := LoadAWSConfig(ctx, opts)
	if err != nil {
		return nil, err
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if opts.Endpoint != "" {
			o.BaseEndpoint = aws.String(opts.Endpoint)
		}
		o.UsePathStyle = opts.ForcePathStyle
	})

	return s3Client, nil
}
