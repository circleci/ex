package redisfixture

import (
	"context"
	"errors"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/go-redis/redis/v8"
	"golang.org/x/mod/modfile"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/o11y"
)

type Fixture struct {
	*redis.Client
	DB int
}

func Setup(ctx context.Context, t testing.TB) *Fixture {
	ctx, span := o11y.StartSpan(ctx, "redisfixture: setup")
	defer span.End()

	// Tests for different go packages are run in parallel by the go test runtime, so
	// we try and use a unique DB for each package.
	pkgName := relativePackageName(2)
	db := hash(pkgName)
	span.AddField("package_name", pkgName)
	span.AddField("db", db)

	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   db,
	})
	t.Cleanup(func() {
		assert.Check(t, redisClient.Close())
	})

	err := redisClient.Ping(ctx).Err()
	if err != nil {
		span.AddField("skipped", true)
		t.Skip("Redis not available")
	}

	err = redisClient.FlushDB(ctx).Err()
	assert.Assert(t, err)

	return &Fixture{
		Client: redisClient,
		DB:     db,
	}
}

func findModfile() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		modFile := filepath.Join(cwd, "go.mod")
		if fileExists(modFile) {
			return modFile, nil
		}

		cwd = filepath.Dir(cwd)
		if cwd == "" || os.IsPathSeparator(cwd[len(cwd)-1]) {
			return "", errors.New("go.mod could not be found")
		}
	}
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func relativePackageName(skip int) string {
	modFile, err := findModfile()
	if err != nil {
		panic(err)
	}

	modBytes, err := os.ReadFile(modFile)
	if err != nil {
		panic(err)
	}

	modulePath := modfile.ModulePath(modBytes)
	return strings.ReplaceAll(packageName(skip), modulePath, "")
}

func packageName(skip int) string {
	var pc [1]uintptr
	n := runtime.Callers(skip+2, pc[:]) // skip + runtime.Callers + callerName
	if n == 0 {
		panic("testing: zero callers found")
	}
	funcName := pcToName(pc[0])
	i := strings.LastIndex(funcName, ".")
	if i == -1 {
		return funcName
	}
	return funcName[:i]
}

func pcToName(pc uintptr) string {
	pcs := []uintptr{pc}
	frames := runtime.CallersFrames(pcs)
	frame, _ := frames.Next()
	return frame.Function
}

func hash(s string) int {
	h := fnv.New32()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32()) % 2048
}
