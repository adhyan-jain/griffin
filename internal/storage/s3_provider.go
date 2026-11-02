package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// S3StorageProvider implements Provider for AWS S3.
type S3StorageProvider struct {
	client        S3ClientAPI
	uploader      S3UploadAPI
	presigner     S3PresignAPI
	defaultBucket string
	region        string
}

// NewS3StorageProvider creates a production S3StorageProvider connected to AWS.
func NewS3StorageProvider(ctx context.Context, opts S3ClientOptions) (*S3StorageProvider, error) {
	rawClient, err := NewS3RawClient(ctx, opts)
	if err != nil {
		return nil, err
	}

	uploader := manager.NewUploader(rawClient, func(u *manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024 // 5 MB part size
	})
	presigner := s3.NewPresignClient(rawClient)

	return NewS3StorageProviderWithAPIs(rawClient, uploader, presigner, opts.Bucket, opts.Region), nil
}

// NewS3StorageProviderWithAPIs constructs an S3StorageProvider using custom or mocked APIs.
func NewS3StorageProviderWithAPIs(
	client S3ClientAPI,
	uploader S3UploadAPI,
	presigner S3PresignAPI,
	defaultBucket string,
	region string,
) *S3StorageProvider {
	return &S3StorageProvider{
		client:        client,
		uploader:      uploader,
		presigner:     presigner,
		defaultBucket: defaultBucket,
		region:        region,
	}
}

func (p *S3StorageProvider) Name() string {
	return "s3"
}

func (p *S3StorageProvider) resolveBucket(bucket string) (string, error) {
	if bucket != "" {
		return bucket, nil
	}
	if p.defaultBucket != "" {
		return p.defaultBucket, nil
	}
	return "", ErrBucketNotFound
}

func (p *S3StorageProvider) Upload(ctx context.Context, input UploadInput) (*UploadOutput, error) {
	bucket, err := p.resolveBucket(input.Bucket)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(input.Key)
	if key == "" {
		return nil, ErrInvalidKey
	}
	if input.Body == nil {
		return nil, fmt.Errorf("%w: upload body is nil", ErrUploadFailed)
	}

	putInput := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   input.Body,
	}
	if input.ContentType != "" {
		putInput.ContentType = aws.String(input.ContentType)
	}
	if len(input.Metadata) > 0 {
		putInput.Metadata = input.Metadata
	}

	out, err := p.uploader.Upload(ctx, putInput)
	if err != nil {
		return nil, mapS3Error(err)
	}

	etag := ""
	if out.ETag != nil {
		etag = *out.ETag
	}
	versionID := ""
	if out.VersionID != nil {
		versionID = *out.VersionID
	}
	uri := fmt.Sprintf("s3://%s/%s", bucket, key)

	return &UploadOutput{
		Bucket:    bucket,
		Key:       key,
		URI:       uri,
		ETag:      etag,
		Size:      input.Size,
		VersionID: versionID,
	}, nil
}

func (p *S3StorageProvider) Download(ctx context.Context, input DownloadInput) (*DownloadOutput, error) {
	bucket, err := p.resolveBucket(input.Bucket)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(input.Key)
	if key == "" {
		return nil, ErrInvalidKey
	}

	out, err := p.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, mapS3Error(err)
	}

	ct := ""
	if out.ContentType != nil {
		ct = *out.ContentType
	}
	etag := ""
	if out.ETag != nil {
		etag = *out.ETag
	}
	var length int64
	if out.ContentLength != nil {
		length = *out.ContentLength
	}
	var lastModified time.Time
	if out.LastModified != nil {
		lastModified = *out.LastModified
	}

	return &DownloadOutput{
		Body:          out.Body,
		ContentType:   ct,
		ContentLength: length,
		ETag:          etag,
		LastModified:  lastModified,
		Metadata:      out.Metadata,
	}, nil
}

func (p *S3StorageProvider) Delete(ctx context.Context, input DeleteInput) error {
	bucket, err := p.resolveBucket(input.Bucket)
	if err != nil {
		return err
	}
	key := strings.TrimSpace(input.Key)
	if key == "" {
		return ErrInvalidKey
	}

	_, err = p.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return mapS3Error(err)
	}
	return nil
}

func (p *S3StorageProvider) Head(ctx context.Context, input HeadInput) (*ObjectInfo, error) {
	bucket, err := p.resolveBucket(input.Bucket)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(input.Key)
	if key == "" {
		return nil, ErrInvalidKey
	}

	out, err := p.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, mapS3Error(err)
	}

	ct := ""
	if out.ContentType != nil {
		ct = *out.ContentType
	}
	etag := ""
	if out.ETag != nil {
		etag = *out.ETag
	}
	var length int64
	if out.ContentLength != nil {
		length = *out.ContentLength
	}
	var lastModified time.Time
	if out.LastModified != nil {
		lastModified = *out.LastModified
	}

	return &ObjectInfo{
		Bucket:       bucket,
		Key:          key,
		Size:         length,
		ETag:         etag,
		ContentType:  ct,
		LastModified: lastModified,
		Metadata:     out.Metadata,
	}, nil
}

func (p *S3StorageProvider) List(ctx context.Context, input ListInput) (*ListOutput, error) {
	bucket, err := p.resolveBucket(input.Bucket)
	if err != nil {
		return nil, err
	}

	params := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	}
	if input.Prefix != "" {
		params.Prefix = aws.String(input.Prefix)
	}
	if input.Delimiter != "" {
		params.Delimiter = aws.String(input.Delimiter)
	}
	if input.MaxKeys > 0 {
		params.MaxKeys = aws.Int32(input.MaxKeys)
	}

	out, err := p.client.ListObjectsV2(ctx, params)
	if err != nil {
		return nil, mapS3Error(err)
	}

	var objects []ObjectInfo
	for _, obj := range out.Contents {
		var k string
		if obj.Key != nil {
			k = *obj.Key
		}
		var sz int64
		if obj.Size != nil {
			sz = *obj.Size
		}
		var et string
		if obj.ETag != nil {
			et = *obj.ETag
		}
		var lm time.Time
		if obj.LastModified != nil {
			lm = *obj.LastModified
		}

		objects = append(objects, ObjectInfo{
			Bucket:       bucket,
			Key:          k,
			Size:         sz,
			ETag:         et,
			LastModified: lm,
		})
	}

	isTruncated := false
	if out.IsTruncated != nil {
		isTruncated = *out.IsTruncated
	}
	nextToken := ""
	if out.NextContinuationToken != nil {
		nextToken = *out.NextContinuationToken
	}

	return &ListOutput{
		Objects:     objects,
		IsTruncated: isTruncated,
		NextToken:   nextToken,
	}, nil
}

func (p *S3StorageProvider) PresignDownloadURL(ctx context.Context, input PresignInput) (*PresignOutput, error) {
	bucket, err := p.resolveBucket(input.Bucket)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(input.Key)
	if key == "" {
		return nil, ErrInvalidKey
	}

	expiry := input.ExpiresIn
	if expiry <= 0 {
		expiry = 15 * time.Minute
	}

	presigned, err := p.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return nil, mapS3Error(err)
	}

	return &PresignOutput{
		URL:       presigned.URL,
		ExpiresAt: time.Now().UTC().Add(expiry),
	}, nil
}

// mapS3Error normalizes AWS SDK errors to Griffin storage sentinel errors.
func mapS3Error(err error) error {
	if err == nil {
		return nil
	}

	var noKey *types.NoSuchKey
	if errors.As(err, &noKey) {
		return ErrObjectNotFound
	}

	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return ErrObjectNotFound
	}

	var noBucket *types.NoSuchBucket
	if errors.As(err, &noBucket) {
		return ErrBucketNotFound
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return ErrObjectNotFound
		case "NoSuchBucket":
			return ErrBucketNotFound
		case "AccessDenied", "Forbidden":
			return ErrPermissionDenied
		case "InvalidBucketName", "InvalidArgument":
			return ErrInvalidKey
		}
	}

	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		if respErr.HTTPStatusCode() == 404 {
			return ErrObjectNotFound
		}
		if respErr.HTTPStatusCode() == 403 {
			return ErrPermissionDenied
		}
	}

	var smithyRespErr *smithyhttp.ResponseError
	if errors.As(err, &smithyRespErr) {
		if smithyRespErr.HTTPStatusCode() == 404 {
			return ErrObjectNotFound
		}
		if smithyRespErr.HTTPStatusCode() == 403 {
			return ErrPermissionDenied
		}
	}

	return err
}
