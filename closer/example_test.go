package closer_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/circleci/ex/closer"
)

func ExampleErrorHandler() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "Hello world!")
	}))
	defer srv.Close()

	output, err := run(srv.URL)
	if err != nil {
		os.Exit(1)
	}
	fmt.Println(output)

	// output: Hello world!
}

func run(rawurl string) (_ string, err error) {
	//#nosec:G107 // this is a test
	//nolint:bodyclose // handled by closer
	resp, err := http.Get(rawurl)
	if err != nil {
		return "", err
	}
	defer closer.ErrorHandler(resp.Body, &err)

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
