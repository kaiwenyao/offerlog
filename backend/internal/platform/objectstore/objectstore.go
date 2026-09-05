package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// Store is a replaceable object-storage adapter. The Go application only ever
// talks to this interface; whether the bytes live on a local disk directory or
// inside SeaweedFS behind its S3 gateway is a deployment concern.
type Store interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (int64, error)
}

func New(cfg Config) (Store, error) {
	if cfg.Provider == "local" || cfg.Provider == "" {
		if err := os.MkdirAll(cfg.LocalDir, 0o755); err != nil {
			return nil, fmt.Errorf("create local object dir: %w", err)
		}
		return &localStore{dir: cfg.LocalDir}, nil
	}
	if cfg.Provider == "s3" {
		return newS3Store(cfg)
	}
	return nil, fmt.Errorf("unknown objectstore provider %q", cfg.Provider)
}

// Config mirrors the subset of deployment configuration the object store needs.
type Config struct {
	Provider     string
	LocalDir     string
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
}

// --- local disk adapter ---

type localStore struct {
	dir string
	mu  sync.Mutex
}

func (l *localStore) path(key string) string {
	// keys are internal, e.g. owners/{id}/files/{uuid}/content ; sanitize anyway
	clean := filepath.Clean(strings.ReplaceAll(key, "..", "_"))
	return filepath.Join(l.dir, clean)
}

func (l *localStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	p := l.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}

func (l *localStore) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	p := l.path(key)
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

func (l *localStore) Delete(_ context.Context, key string) error {
	p := l.path(key)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (l *localStore) Stat(_ context.Context, key string) (int64, error) {
	st, err := os.Stat(l.path(key))
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// --- S3 / SeaweedFS adapter ---

type s3Store struct {
	client *s3.Client
	up     *manager.Uploader
	bucket string
}

func newS3Store(cfg Config) (Store, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = cfg.UsePathStyle
	})
	return &s3Store{
		client: client,
		up:     manager.NewUploader(client),
		bucket: cfg.Bucket,
	}, nil
}

func (s *s3Store) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.up.Upload(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          r,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	})
	return err
}

func (s *s3Store) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, 0, err
	}
	var sz int64
	if out.ContentLength != nil {
		sz = *out.ContentLength
	}
	return out.Body, sz, nil
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		// Deleting a missing key is idempotent in S3 semantics; SeaweedFS
		// returns NoSuchKey for repeated deletes, treat that as success.
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "404") {
			return nil
		}
		return err
	}
	return nil
}

func (s *s3Store) Stat(ctx context.Context, key string) (int64, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return 0, err
	}
	if out.ContentLength == nil {
		return 0, nil
	}
	return *out.ContentLength, nil
}
