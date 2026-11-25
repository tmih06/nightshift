// Command nightshift is the software factory's terminal: a sandbox tab to
// watch and drive the agent-sandbox fleet, and a GitHub tab that is
// gh-dash.
//
// go run ./cmd/nightshift opens the TUI; -once prints one sandbox frame;
// -pr owner/repo#123 (or a pull request URL) opens on that pull request;
// -repos opens on the GitHub Repo view.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
	gitm "github.com/aymanbagabas/git-module"
	"github.com/cli/go-gh/v2/pkg/repository"

	"github.com/tmih06/nightshift/internal/ghdash/config"
	"github.com/tmih06/nightshift/internal/ghdash/git"
	ghtui "github.com/tmih06/nightshift/internal/ghdash/tui"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
	"github.com/tmih06/nightshift/internal/tui/app"
	zgh "github.com/tmih06/nightshift/internal/zenocto/gh"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-once" {
		fmt.Println(app.Once(120, 32))
		return
	}
	var (
		openPR     string
		startRepos bool
	)
	switch {
	case len(os.Args) > 2 && os.Args[1] == "-pr":
		openPR = os.Args[2]
	case len(os.Args) > 1 && os.Args[1] == "-repos":
		startRepos = true
	}
	if err := run(openPR, startRepos); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(openPR string, startRepos bool) error {
	var start *zgh.PRRef
	if openPR != "" {
		ref, err := zgh.ParsePRRef(openPR)
		if err != nil {
			return err
		}
		start = &ref
	}

	// gh-dash logs to stderr, which would draw over the TUI. NIGHTSHIFT_DEBUG
	// sends it to a file instead.
	log.SetOutput(io.Discard)
	if path := os.Getenv("NIGHTSHIFT_DEBUG"); path != "" {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		log.SetOutput(f)
		log.SetLevel(log.DebugLevel)
	}

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
		return fmt.Errorf("gh-dash config: %w", err)
	}
	theme := ghtheme.ParseTheme(&cfg)

	dash := ghtui.NewModel(location, ghtui.Repositories{GitRepo: gitRepo, GHRepo: &ghRepo})

	root := app.New(theme, app.NewGhDash(&dash), nil)
	if ghRepo.Owner != "" && ghRepo.Name != "" {
		root = root.WithCurrentRepo(ghRepo.Owner + "/" + ghRepo.Name)
	}
	if start != nil {
		root = root.StartOn(*start)
	} else if startRepos {
		root = root.StartOnRepos()
	}
	final, err := tea.NewProgram(root).Run()
	if m, ok := final.(app.Model); ok {
		m.Close()
	}
	return err
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
