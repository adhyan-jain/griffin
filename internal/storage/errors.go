package storage

import "errors"

var (
	// ErrObjectNotFound indicates that the requested object does not exist in storage.
	ErrObjectNotFound = errors.New("storage: object not found")

	// ErrBucketNotFound indicates that the specified bucket does not exist.
	ErrBucketNotFound = errors.New("storage: bucket not found")

	// ErrPermissionDenied indicates that storage access was denied due to missing permissions.
	ErrPermissionDenied = errors.New("storage: permission denied")

	// ErrInvalidKey indicates that an object key is empty or invalid.
	ErrInvalidKey = errors.New("storage: invalid object key")

	// ErrUploadFailed indicates that an upload operation failed.
	ErrUploadFailed = errors.New("storage: upload failed")

	// ErrDownloadFailed indicates that a download operation failed.
	ErrDownloadFailed = errors.New("storage: download failed")

	// ErrDeleteFailed indicates that a delete operation failed.
	ErrDeleteFailed = errors.New("storage: delete failed")

	// ErrAuthenticationFailed indicates that AWS credentials could not be loaded or verified.
	ErrAuthenticationFailed = errors.New("storage: authentication failed or credentials missing")
)
