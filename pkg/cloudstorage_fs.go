package storage

import (
	"context"
	"io"
	"time"

	"google.golang.org/api/option"

	credentials "cloud.google.com/go/iam/credentials/apiv1"
	"cloud.google.com/go/storage"
	"github.com/pkg/errors"
	credentialspb "google.golang.org/genproto/googleapis/iam/credentials/v1"
)

var ErrCredentialsMissing = errors.New("credentials missing")

// NewCloudStorageFS creates a Google Cloud Storage FS
// credentials can be nil to use the default GOOGLE_APPLICATION_CREDENTIALS
func NewCloudStorageFS(bucket string, opts ...option.ClientOption) FS {
	return &cloudStorageFS{
		bucket: bucket,
		opts:   opts,
	}
}

// cloudStorageFS implements FS and uses Google Cloud Storage as the underlying
// file storage.
type cloudStorageFS struct {
	// bucket is the name of the bucket to use as the underlying storage.
	bucket string
	opts   []option.ClientOption
}

func (c *cloudStorageFS) URL(ctx context.Context, path string, options *SignedURLOptions) (string, error) {
	creds, err := credentials.NewIamCredentialsClient(ctx, c.opts...)
	if err != nil {
		return "", errors.Wrap(err, "cloud stoarge: error while creating credentials")
	}

	var storageOptions *storage.SignedURLOptions
	if options != nil {
		o := storage.SignedURLOptions(*options)
		storageOptions = &o
	}

	if storageOptions.Expires.IsZero() {
		storageOptions.Expires = time.Now().Add(15 * time.Minute)
	}

	storageOptions.Method = "GET"
	storageOptions.SignBytes = func(b []byte) ([]byte, error) {
		req := &credentialspb.SignBlobRequest{
			Payload: b,
			Name:    storageOptions.GoogleAccessID,
		}
		resp, err := creds.SignBlob(ctx, req)
		if err != nil {
			return nil, errors.Wrap(err, "cloud storage: error signing blob for SignURL")
		}
		return resp.SignedBlob, err
	}

	return storage.SignedURL(c.bucket, path, storageOptions)
}

// Open implements FS.
func (c *cloudStorageFS) Open(ctx context.Context, path string, options *ReaderOptions) (*File, error) {
	b, err := c.bucketHandle(ctx)
	if err != nil {
		return nil, err
	}

	f, err := b.Object(path).NewReader(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return nil, &notExistError{
				Path: path,
			}
		}
		return nil, errors.Wrap(err, "cloud storage: error fetching object attributes")
	}

	return &File{
		ReadCloser: f,
		Attributes: Attributes{
			ContentType: f.ContentType(),
			Size:        f.Size(),
			ModTime:     f.Attrs.LastModified,
		},
	}, nil
}

// Attributes implements FS.
func (c *cloudStorageFS) Attributes(ctx context.Context, path string, options *ReaderOptions) (*Attributes, error) {
	b, err := c.bucketHandle(ctx)
	if err != nil {
		return nil, err
	}

	a, err := b.Object(path).Attrs(ctx)
	if err != nil {
		return nil, err
	}

	return &Attributes{
		ContentType: a.ContentType,
		Metadata:    a.Metadata,
		ModTime:     a.Updated,
		Size:        a.Size,
	}, nil
}

// Create implements FS.
func (c *cloudStorageFS) Create(ctx context.Context, path string, options *WriterOptions) (io.WriteCloser, error) {
	b, err := c.bucketHandle(ctx)
	if err != nil {
		return nil, err
	}

	obj := b.Object(path)

	writer := obj.NewWriter(ctx)
	writer.Metadata = options.Attributes.Metadata
	writer.ContentType = options.Attributes.ContentType
	writer.Size = options.Attributes.Size
	writer.ChunkSize = options.BufferSize

	if options.ACL != nil {
		writer.ACL = options.ACL
	} else {
		writer.ACL = []storage.ACLRule{{Entity: storage.AllAuthenticatedUsers, Role: storage.RoleReader}}
	}

	return writer, nil
}

// Delete implements FS.
func (c *cloudStorageFS) Delete(ctx context.Context, path string) error {
	b, err := c.bucketHandle(ctx)
	if err != nil {
		return err
	}
	return b.Object(path).Delete(ctx)
}

// Walk implements FS.
func (c *cloudStorageFS) Walk(ctx context.Context, path string, fn WalkFn) error {
	b, err := c.bucketHandle(ctx)
	if err != nil {
		return err
	}

	it := b.Objects(ctx, &storage.Query{
		Prefix: path,
	})

	for {
		r, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			// TODO(dhowden): Properly handle this error.
			return err
		}

		if err = fn(r.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c *cloudStorageFS) client(ctx context.Context) (*storage.Client, error) {
	client, err := storage.NewClient(ctx, c.opts...)
	if err != nil {
		return nil, errors.Wrap(err, "cloud storage: unable to create client")
	}

	return client, nil
}

func (c *cloudStorageFS) bucketHandle(ctx context.Context) (*storage.BucketHandle, error) {
	client, err := c.client(ctx)
	if err != nil {
		return nil, err
	}

	return client.Bucket(c.bucket), nil
}
