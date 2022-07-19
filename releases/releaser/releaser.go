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
	"net/url"
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

type PublishParameters struct {
	Path    string
	Bucket  string
	App     string
	Version string

	// IncludeFilter optionally allows filtering the files to be uploaded
	IncludeFilter func(path string, info os.FileInfo) bool

	// Tags optionally allows bucket tags to be applied
	Tags map[string]string
}

func (r *Releaser) Publish(ctx context.Context, params PublishParameters) error {
	err := r.uploadBinaries(ctx, params)
	if err != nil {
		return err
	}

	err = r.uploadChecksums(ctx, params)
	if err != nil {
		return err
	}

	return nil
}

type ReleaseParameters struct {
	Bucket  string
	App     string
	Version string

	// Environment optionally allows specifying the environment, defaults to "release"
	Environment string

	// Tags optionally allows bucket tags to be applied
	Tags map[string]string
}

func (r *Releaser) Release(ctx context.Context, params ReleaseParameters) error {
	if params.Environment == "" {
		params.Environment = "release"
	}

	key := filepath.Join(params.App, params.Environment+".txt")
	fmt.Printf("Releasing: %q - %s\n", key, params.Version)
	_, err := r.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: &params.Bucket,
		Body:   strings.NewReader(params.Version),
		Key:    &key,

		Tagging: encodeTags(params.Tags),
	})
	return err
}

func (r *Releaser) uploadBinaries(ctx context.Context, params PublishParameters) error {
	return r.walkFiles(params.Path, params.IncludeFilter, func(path string, info os.FileInfo) (err error) {
		key := fileKey(params.App, params.Version, strings.TrimPrefix(path, params.Path))
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
			if ferr != nil {
				err = ferr
			}
		}()

		pr, pw := io.Pipe()
		defer closer(pw, &err)

		g.Go(func() error {
			_, err := r.uploader.Upload(ctx, &s3.PutObjectInput{
				Bucket:          &params.Bucket,
				Body:            pr,
				Key:             &key,
				ContentEncoding: &contentEncodingGZIP,
				ContentType:     &contentTypeOctetStream,

				Tagging: encodeTags(params.Tags),
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

func (r *Releaser) uploadChecksums(ctx context.Context, params PublishParameters) error {
	var checksums bytes.Buffer

	err := r.walkFiles(params.Path, params.IncludeFilter, func(path string, info os.FileInfo) (err error) {
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

		fileName := strings.TrimPrefix(path, params.Path)
		fileName = strings.TrimPrefix(fileName, string(os.PathSeparator))
		_, err = fmt.Fprintf(&checksums, "%x *%s\n", h.Sum(nil), fileName)
		return err
	})
	if err != nil {
		return err
	}

	checksumsFile := filepath.Join(params.Path, "checksums.txt")
	fmt.Printf("Writing: %q\n", checksumsFile)
	//#nosec:G306 // These permissions are intentional
	err = os.WriteFile(checksumsFile, checksums.Bytes(), 0644)
	if err != nil {
		return err
	}

	key := fileKey(params.App, params.Version, "checksums.txt")
	fmt.Printf("Uploading: %q\n", key)
	_, err = r.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: &params.Bucket,
		Key:    &key,
		Body:   &checksums,
	})
	return err
}

func (r *Releaser) walkFiles(basePath string, includeFn func(path string, info os.FileInfo) bool,
	observerFn func(path string, info os.FileInfo) error) error {
	return filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if includeFn != nil && !includeFn(path, info) {
			return nil
		}

		return observerFn(path, info)
	})
}

func fileKey(app, version, file string) string {
	return filepath.Join(app, version, file)
}

func encodeTags(tags map[string]string) *string {
	if len(tags) == 0 {
		return nil
	}
	params := url.Values{}
	for k, v := range tags {
		params.Add(k, v)
	}
	encoded := params.Encode()
	return &encoded
}

func closer(r io.Closer, in *error) {
	ferr := r.Close()
	if *in == nil {
		*in = ferr
	}
}
