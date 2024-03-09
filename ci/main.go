package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"dagger.io/dagger"
)

func main() {
	ctx := context.Background()

	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		panic(err)
	}
	defer client.Close()

	directory := client.Host().
		Directory(".", dagger.HostDirectoryOpts{
			Exclude: []string{"build/", "ci/"},
		})

	// pipeline
	ci := CI{}
	err = ci.doFmt(ctx, directory, client)
	if err != nil {
		panic(err)
	}

	err = ci.doBuildSrc(ctx, directory, client)
	if err != nil {
		panic(err)
	}

	err = ci.doBuildOci(ctx, directory, client)
	if err != nil {
		panic(err)
	}
}

type CI struct{}

type CiProcess interface {
	doFmt(ctx context.Context, dir *dagger.Directory, client *dagger.Client) error
	doBuildSrc(ctx context.Context, dir *dagger.Directory, client *dagger.Client) error
	doBuildOci(ctx context.Context, dir *dagger.Directory, client *dagger.Client) error
}

var _ CiProcess = &CI{}

func (c *CI) doFmt(ctx context.Context, dir *dagger.Directory, client *dagger.Client) error {
	goscan := client.Container().
		From("golang:1.22").
		WithDirectory("/src", dir).
		WithWorkdir("/src").
		WithExec([]string{"go", "fmt", "./..."})
	out, err := goscan.Stdout(ctx)
	if err != nil {
		return err
	}

	log.Printf("Formated source code with output: %v\n", out)

	golint := client.Container().
		From("golangci/golangci-lint:v1.54.1").
		WithDirectory("/src", dir).
		WithWorkdir("/src").
		WithExec([]string{"golangci-lint", "run", "-v"})
	out, err = golint.Stdout(ctx)
	if err != nil {
		return err
	}

	log.Printf("Linted source code with output: %v\n", out)
	return nil
}

func (c *CI) doBuildSrc(ctx context.Context, dir *dagger.Directory, client *dagger.Client) error {
	gobuild := client.Container().
		From("golang:1.22").
		WithDirectory("/src", dir).
		WithWorkdir("/src").
		WithExec([]string{"go", "mod", "download"})
	arches := []string{"arm64", "amd64"}
	oses := []string{"darwin", "linux"}
	artifactsDir := "./artifacts"

	for _, arch := range arches {
		for _, os := range oses {
			fileBuild := fmt.Sprintf("main_%v_%v", arch, os)
			_, err := gobuild.
				WithEnvVariable("GOOS", os).
				WithEnvVariable("GOARCH", arch).
				WithExec([]string{"go", "build", "-o", fileBuild, "main.go"}).
				File(fileBuild).
				Export(ctx, fmt.Sprintf("%v/%v", artifactsDir, fileBuild))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *CI) doBuildOci(ctx context.Context, dir *dagger.Directory, client *dagger.Client) error {
	tag := fmt.Sprint("/tmp/dagger_http.tar")
	build := client.Container().
		From("cgr.dev/chainguard/go:latest").
		WithDirectory("/app", dir).
		WithWorkdir("/app").
		WithEnvVariable("CGO_ENABLED", "0").
		WithEnvVariable("GOOS", "linux").
		WithEnvVariable("GOARCH", "amd64").
		WithExec([]string{"mod", "download"}).
		WithExec([]string{"build", "-o", "main", "main.go"})

	val, err := client.Container().
		From("cgr.dev/chainguard/static:latest-glibc").
		WithFile("/bin/main", build.File("/app/main")).
		WithEntrypoint([]string{"/bin/main"}).
		Export(ctx, tag)

	log.Printf("Exported image: %v", val)

	return err
}
