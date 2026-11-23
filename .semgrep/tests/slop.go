package fixtures

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
)

func blankBad(f *os.File, err error, b []byte, v any, w io.Writer, r io.Reader) {
	// ruleid: slop-error-assigned-to-blank
	_ = err
	// ruleid: slop-error-assigned-to-blank
	_ = f.Close()
	// ruleid: slop-error-assigned-to-blank
	_ = json.Unmarshal(b, v)
	// ruleid: slop-error-assigned-to-blank
	_, _ = io.Copy(w, r)
}

func blankGood(f *os.File, m map[string]int) (int, error) {
	// ok: slop-error-assigned-to-blank
	_ = m["key"]
	// ok: slop-error-assigned-to-blank
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("close: %w", err)
	}
	return 1, nil
}

func emptyBad(err error) {
	// ruleid: slop-empty-error-block
	if err != nil {
	}
}

func emptyGood(err error, p *int) error {
	// ok: slop-empty-error-block
	if p != nil {
	}
	// ok: slop-empty-error-block
	if err != nil {
		return fmt.Errorf("step: %w", err)
	}
	return nil
}

func goroutineBad(work func()) {
	// ruleid: slop-floating-goroutine
	go func() {
		work()
	}()
}

func goroutineGood(ctx context.Context, work func()) {
	var wg sync.WaitGroup
	wg.Add(1)
	// ok: slop-floating-goroutine
	go func() {
		defer wg.Done()
		work()
	}()
	done := make(chan struct{})
	// ok: slop-floating-goroutine
	go func() {
		work()
		done <- struct{}{}
	}()
	// ok: slop-floating-goroutine
	go func() {
		<-ctx.Done()
		work()
	}()
	wg.Wait()
	<-done
}

func kubectlBad(ctx context.Context) {
	// ruleid: slop-kubectl-outside-cluster
	exec.Command("kubectl", "get", "pods")
	// ruleid: slop-kubectl-outside-cluster
	exec.CommandContext(ctx, "kubectl", "get", "pods")
}

func kubectlGood(ctx context.Context) {
	// ok: slop-kubectl-outside-cluster
	exec.CommandContext(ctx, "git", "status")
}

func contextBad(ctx context.Context) context.Context {
	// ruleid: slop-background-context-in-request-path
	return context.Background()
}

func handlerBad(w http.ResponseWriter, r *http.Request) {
	// ruleid: slop-background-context-in-request-path
	_ = context.Background()
}

func contextGood() context.Context {
	// ok: slop-background-context-in-request-path
	return context.Background()
}

func handlerGood(w http.ResponseWriter, r *http.Request) context.Context {
	// ok: slop-background-context-in-request-path
	return r.Context()
}
