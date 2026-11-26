// Command nightshift is the software factory's terminal: a Pods tab to watch
// and drive the agent-sandbox fleet, a GitHub tab that is gh-dash, and an
// Accounts tab. It is a frontend of the Nightshift services and talks to them
// only through their API (docs/api.md), at the server named by -api, or by
// NIGHTSHIFT_API and NIGHTSHIFT_TOKEN. The services run in the cluster as the
// nightshiftd pod; nothing runs on the machine but this terminal.
//
// go run ./cmd/nightshift opens the TUI; -once prints one sandbox frame;
// -pr owner/repo#123 (or a pull request URL) opens on that pull request;
// -repos opens on the GitHub Repo view.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
	gitm "github.com/aymanbagabas/git-module"
	"github.com/cli/go-gh/v2/pkg/repository"

	"github.com/tmih06/nightshift/api/client"
	"github.com/tmih06/nightshift/internal/ghdash/config"
	"github.com/tmih06/nightshift/internal/ghdash/git"
	ghtui "github.com/tmih06/nightshift/internal/ghdash/tui"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
	"github.com/tmih06/nightshift/internal/tui/app"
	zgh "github.com/tmih06/nightshift/internal/zenocto/gh"
)

// options are what the command line asks for.
type options struct {
	once       bool
	openPR     string
	startRepos bool
	api, token string
}

func parseOptions(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("nightshift", flag.ContinueOnError)
	fs.BoolVar(&o.once, "once", false, "print one frame of the Pods tab and exit")
	fs.StringVar(&o.openPR, "pr", "", "open on a pull request: owner/repo#123 or its URL")
	fs.BoolVar(&o.startRepos, "repos", false, "open on the GitHub Repo view")
	fs.StringVar(&o.api, "api", os.Getenv("NIGHTSHIFT_API"), "address of the Nightshift services server (nightshiftd), e.g. https://nightshift.example.com")
	fs.StringVar(&o.token, "token", os.Getenv("NIGHTSHIFT_TOKEN"), "bearer token for -api")
	if err := fs.Parse(args); err != nil {
		return o, fmt.Errorf("arguments: %w", err)
	}
	return o, nil
}

func main() {
	o, err := parseOptions(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// connect is the services client for the server o names. The services never
// run inside this program: they run in the cluster (k8s/nightshiftd.yaml).
func connect(o options) (*client.Client, error) {
	if o.api == "" {
		return nil, errors.New("no services server: pass -api, or set NIGHTSHIFT_API and NIGHTSHIFT_TOKEN " +
			"(deploy one with k8s/nightshiftd-setup.sh; docs/api.md says how to reach it)")
	}
	if o.token == "" {
		return nil, errors.New("-api needs a token: pass -token or set NIGHTSHIFT_TOKEN")
	}
	c, err := client.New(o.api, o.token)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return c, nil
}

// logTo points the program's log at the NIGHTSHIFT_DEBUG file, or nowhere:
// gh-dash logs to stderr, which would draw over the TUI. close ends it.
func logTo(path string) (closeLog func(), err error) {
	log.SetOutput(io.Discard)
	if path == "" {
		return func() {}, nil
	}
	dir, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("open the debug log's directory: %w", err)
	}
	f, err := dir.OpenFile(filepath.Base(path), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the debug log: %w", err)
	}
	log.SetOutput(f)
	log.SetLevel(log.DebugLevel)
	return func() {
		if err := f.Close(); err != nil {
			log.Debug("close the debug log", "err", err)
		}
	}, nil
}

func run(o options) error {
	var start *zgh.PRRef
	if o.openPR != "" {
		ref, err := zgh.ParsePRRef(o.openPR)
		if err != nil {
			return fmt.Errorf("-pr: %w", err)
		}
		start = &ref
	}
	closeLog, err := logTo(os.Getenv("NIGHTSHIFT_DEBUG"))
	if err != nil {
		return err
	}
	defer closeLog()

	svc, err := connect(o)
	if err != nil {
		return err
	}
	if o.once {
		if _, err := fmt.Fprintln(os.Stdout, app.Once(120, 32, svc)); err != nil {
			return fmt.Errorf("print the frame: %w", err)
		}
		return nil
	}
	root, err := newRoot(svc, start, o.startRepos)
	if err != nil {
		return err
	}
	if _, err := tea.NewProgram(root).Run(); err != nil {
		return fmt.Errorf("run: %w", err)
	}
	return nil
}

// newRoot builds the root model: gh-dash for the repository the program was
// started in, over the services.
func newRoot(svc app.Services, start *zgh.PRRef, startRepos bool) (app.Model, error) {
	gitRepo, ghRepo := currentRepos()
	var repoPath string
	if gitRepo != nil {
		repoPath = gitRepo.Path()
	}
	location := config.Location{RepoPath: repoPath}

	// gh-dash exits the process on a bad config once it is running, so the
	// config is checked first, while the terminal is still ours.
	cfg, err := config.ParseConfig(location)
	if err != nil {
		return app.Model{}, fmt.Errorf("gh-dash config: %w", err)
	}
	theme := ghtheme.ParseTheme(&cfg)
	dash := ghtui.NewModel(location, ghtui.Repositories{GitRepo: gitRepo, GHRepo: &ghRepo})

	root := app.New(theme, app.NewGhDash(&dash), nil, svc)
	if ghRepo.Owner != "" && ghRepo.Name != "" {
		root = root.WithCurrentRepo(ghRepo.Owner + "/" + ghRepo.Name)
	}
	if start != nil {
		root = root.StartOn(*start)
	} else if startRepos {
		root = root.StartOnRepos()
	}
	return root, nil
}

// currentRepos finds the git and GitHub repositories of the working
// directory, as gh-dash does, so its repo-scoped sections and branch view work.
func currentRepos() (*gitm.Repository, repository.Repository) {
	var (
		gitRepo *gitm.Repository
		ghRepo  repository.Repository
		wg      sync.WaitGroup
	)
	wg.Go(func() {
		r, err := git.GetRepoInPwd()
		if err == nil {
			gitRepo = r
		}
	})
	wg.Go(func() {
		r, err := repository.Current()
		if err == nil || !errors.Is(err, context.Canceled) {
			ghRepo = r
		}
	})
	wg.Wait()
	return gitRepo, ghRepo
}
