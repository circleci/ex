/*
Package releaser aids with publishing your Go binaries efficiently and in a consistent way.
*/
package releaser

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/sync/errgroup"
)

var (
	contentEncodingGZIP    = "gzip"
	contentTypeOctetStream = "application/octet-stream"
)

type Releaser struct {
	s3       *s3.Client
	uploader *manager.Uploader
}

func New(ctx context.Context) (*Releaser, error) {
	aws, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	r := NewWithClient(s3.NewFromConfig(aws))
	return r, nil
}

func NewWithClient(client *s3.Client) *Releaser {
	return &Releaser{
		s3:       client,
		uploader: manager.NewUploader(client),
	}
}

func (r *Releaser) Publish(ctx context.Context, path, bucket, app, version string) error {
	err := r.uploadBinaries(ctx, path, bucket, app, version)
	if err != nil {
		return err
	}

	err = r.uploadChecksums(ctx, path, bucket, app, version)
	if err != nil {
		return err
	}

	return nil
}

func (r *Releaser) Release(ctx context.Context, bucket, app, version string) error {
	key := filepath.Join(app, "release.txt")
	fmt.Printf("Releasing: %q - %s\n", key, version)
	_, err := r.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: &bucket,
		Body:   strings.NewReader(version),
		Key:    &key,
	})
	return err
}

func (r *Releaser) uploadBinaries(ctx context.Context, basePath, bucket, app, version string) error {
	return r.walkFiles(basePath, func(path string, info os.FileInfo) (err error) {
		key := r.fileKey(app, version, strings.TrimPrefix(path, basePath))
		fmt.Printf("Uploading: %q\n", key)

		//#nosec:G304 // Intentionally uploading file from disk
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer closer(in, &err)

		g, _ := errgroup.WithContext(ctx)
		defer func() {
			ferr := g.Wait()
			if err == nil {
				err = ferr
			}
		}()

		pr, pw := io.Pipe()
		defer closer(pw, &err)

		g.Go(func() error {
			_, err := r.uploader.Upload(ctx, &s3.PutObjectInput{
				Bucket:          &bucket,
				Body:            pr,
				Key:             &key,
				ContentEncoding: &contentEncodingGZIP,
				ContentType:     &contentTypeOctetStream,
			})
			if err != nil {
				_ = pw.CloseWithError(err)
				return err
			}
			return nil
		})

		gz := gzip.NewWriter(pw)
		defer closer(gz, &err)

		_, err = io.Copy(gz, in)
		return err
	})
}

func (r *Releaser) uploadChecksums(ctx context.Context, basePath, bucket, app, version string) error {
	var checksums bytes.Buffer

	err := r.walkFiles(basePath, func(path string, info os.FileInfo) (err error) {
		//#nosec:G304 // Intentionally reading file from disk
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() {
			_ = f.Close()
		}()

		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}

		fileName := strings.TrimPrefix(path, basePath)
		fileName = strings.TrimPrefix(fileName, string(os.PathSeparator))
		_, err = fmt.Fprintf(&checksums, "%x *%s\n", h.Sum(nil), fileName)
		return err
	})
	if err != nil {
		return err
	}

	key := r.fileKey(app, version, "checksums.txt")
	fmt.Printf("Uploading: %q\n", key)
	_, err = r.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: &bucket,
		Key:    &key,
		Body:   &checksums,
	})
	return err
}

func (r *Releaser) walkFiles(basePath string, fn func(path string, info os.FileInfo) error) error {
	return filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		return fn(path, info)
	})
}

func (r *Releaser) fileKey(app, version, file string) string {
	return filepath.Join(app, version, file)
}

func closer(r io.Closer, in *error) {
	ferr := r.Close()
	if *in == nil {
		*in = ferr
	}
}
