package acctest

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

// startTimeout bounds how long a started fake may take to listen and answer its health
// check. A full `go test ./...` starts a fake per package while the machine is busy
// building and running everything else, so the fake needs far longer than it does alone.
const startTimeout = 3 * time.Minute

var listeningAddress = regexp.MustCompile(`msg=listening address=(\S+)`)

// fake is a fake Stripe API process started by the harness.
type fake struct {
	baseURL string
	dir     string
	cmd     *exec.Cmd
}

// startFake builds the fake from a temporary copy of the source tree and starts it on a
// free loopback port. The source is only read.
func startFake(source string) (*fake, error) {
	dir, err := os.MkdirTemp("", "stripe-fake-")
	if err != nil {
		return nil, err
	}
	f := &fake{dir: dir}
	ok := false
	defer func() {
		if !ok {
			f.stop()
		}
	}()

	src := filepath.Join(dir, "src")
	if err := copyTree(source, src); err != nil {
		return nil, fmt.Errorf("copying the fake source: %w", err)
	}
	binary := filepath.Join(dir, "fake-stripe-api")
	build := exec.Command("go", "-C", src, "build", "-o", binary, "./cmd/fake-stripe-api")
	if out, err := build.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("building the fake: %w\n%s", err, out)
	}

	f.cmd = exec.Command(binary, "-listen", "127.0.0.1:0")
	stderr, err := f.cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := f.cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting the fake: %w", err)
	}

	addr := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		sent := false
		for scanner.Scan() {
			if m := listeningAddress.FindStringSubmatch(scanner.Text()); m != nil && !sent {
				addr <- m[1]
				sent = true
			}
		}
		if !sent {
			close(addr)
		}
		_, _ = io.Copy(io.Discard, stderr)
	}()
	select {
	case a, open := <-addr:
		if !open {
			return nil, fmt.Errorf("the fake exited before it listened")
		}
		f.baseURL = "http://" + a
	case <-time.After(startTimeout):
		return nil, fmt.Errorf("the fake did not report a listen address within %s", startTimeout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	if err := waitHealthy(ctx, f.baseURL); err != nil {
		return nil, err
	}
	ok = true
	return f, nil
}

func waitHealthy(ctx context.Context, baseURL string) error {
	admin := &adminClient{baseURL: baseURL}
	for {
		if _, err := admin.Health(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the fake did not become healthy: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// stop kills the process this harness started, by its own handle, and removes its files.
func (f *fake) stop() {
	if f.cmd != nil && f.cmd.Process != nil {
		_ = f.cmd.Process.Kill()
		_ = f.cmd.Wait()
	}
	_ = os.RemoveAll(f.dir)
}

// copyTree copies a directory tree, leaving out version control metadata.
func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o644)
	})
}
