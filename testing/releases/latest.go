package releases

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/circleci/ex/releases/download"
)

// DownloadConfig is the configuration for the DownloadLatest helper
type DownloadConfig struct {
	// BaseURL is the url of the file binary release file server
	BaseURL string
	// Which is the application to download
	Which string
	// Binary is the binary to download. If empty it will default to the value of Which
	Binary string
	// Pinned if set will use this version to download
	Pinned string
	// Dir is the directory to download into, if empty will default to ../bin
	Dir string
}

// DownloadLatest is a helper that will download the latest test binary
func DownloadLatest(ctx context.Context, conf DownloadConfig) (string, error) {
	d := New(conf.BaseURL + "/" + conf.Which)

	var ver string
	if conf.Pinned != "" {
		ver = conf.Pinned
	} else {
		var err error
		ver, err = d.Version(ctx)
		if err != nil {
			return "", fmt.Errorf("version failed: %w", err)
		}
	}

	testBinURLs, err := d.ResolveURLs(ctx, Requirements{
		Version: ver,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	})
	if err != nil {
		return "", fmt.Errorf("resolve failed: %w", err)
	}

	if conf.Binary == "" {
		conf.Binary = conf.Which
	}

	testBinURL, ok := testBinURLs[conf.Binary]
	if !ok {
		return "", fmt.Errorf("resolve binary failed: %s", conf.Binary)
	}

	// default the download directory to bin
	if conf.Dir == "" {
		conf.Dir = "../bin"
	}

	dl, err := download.NewDownloader(time.Minute, conf.Dir)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	path, err := dl.Download(ctx, testBinURL, 0700)
	if err != nil {
		return "", fmt.Errorf("download (%s) problem: %w", testBinURL, err)
	}
	const winExeExtension = ".exe"
	if runtime.GOOS == "windows" && !strings.HasSuffix(path, winExeExtension) {
		path += winExeExtension
	}
	return path, nil
}
