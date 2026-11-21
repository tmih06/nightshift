package repoview

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cli/go-gh/v2/pkg/api"
)

const maxPreviewBytes = 64 * 1024

// Client defines the GitHub operations used by repoview.
type Client interface {
	Viewer(ctx context.Context) (string, error)
	ListRepos(ctx context.Context, limit int) ([]Repo, error)
	SearchRepos(ctx context.Context, query string, limit int) ([]Repo, error)
	GetRepoOverview(ctx context.Context, repo string) (Repo, error)

	ListBranches(ctx context.Context, repo string) ([]Branch, error)
	ListBranchCommits(ctx context.Context, repo, branch string, limit int) ([]Commit, error)
	CreateBranch(ctx context.Context, repo, name, baseRef string) error
	DeleteBranch(ctx context.Context, repo, name string) error

	ListPRs(ctx context.Context, repo string, limit int) ([]PullRequest, error)

	ListIssues(ctx context.Context, repo string, limit int) ([]Issue, error)
	ListIssueComments(ctx context.Context, repo string, number int) ([]IssueComment, error)
	CreateIssue(ctx context.Context, repo, title, body string) (Issue, error)
	CommentIssue(ctx context.Context, repo string, number int, body string) (IssueComment, error)
	SetIssueState(ctx context.Context, repo string, number int, state string) error

	ListWorkflowRuns(ctx context.Context, repo string, limit int) ([]WorkflowRun, error)
	ListWorkflowJobs(ctx context.Context, repo string, runID int64) ([]WorkflowJob, error)
	RerunWorkflow(ctx context.Context, repo string, runID int64, failedOnly bool) error

	ListDir(ctx context.Context, repo, ref, path string) ([]TreeEntry, error)
	GetFile(ctx context.Context, repo, ref, path string) (FileContent, error)
}

type graphQLDoer interface {
	DoWithContext(ctx context.Context, query string, variables map[string]any, response any) error
}

type restDoer interface {
	DoWithContext(ctx context.Context, method, path string, body io.Reader, response any) error
	RequestWithContext(ctx context.Context, method, path string, body io.Reader) (*http.Response, error)
}

type githubClient struct {
	gql  graphQLDoer
	rest restDoer
}

// NewGitHubClient creates a live GitHub API client using ambient gh auth.
func NewGitHubClient() (Client, error) {
	gql, err := api.NewGraphQLClient(api.ClientOptions{})
	if err != nil {
		return nil, fmt.Errorf("building GitHub GraphQL client: %w", err)
	}
	rest, err := api.NewRESTClient(api.ClientOptions{})
	if err != nil {
		return nil, fmt.Errorf("building GitHub REST client: %w", err)
	}
	return &githubClient{gql: gql, rest: rest}, nil
}

func splitRepo(full string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(strings.TrimSpace(full), "/")
	if !ok || owner == "" || name == "" {
		return "", "", fmt.Errorf("invalid repository %q: expected owner/name", full)
	}
	return owner, name, nil
}

func (c *githubClient) Viewer(ctx context.Context) (string, error) {
	var resp struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := c.gql.DoWithContext(ctx, `query { viewer { login } }`, nil, &resp); err != nil {
		return "", err
	}
	return resp.Viewer.Login, nil
}

type gqlRepoNode struct {
	NameWithOwner string `json:"nameWithOwner"`
	Name          string `json:"name"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
	Description string `json:"description"`
	URL         string `json:"url"`
	IsPrivate   bool   `json:"isPrivate"`
	IsFork      bool   `json:"isFork"`
	IsArchived  bool   `json:"isArchived"`
	Visibility  string `json:"visibility"`
	Stargazers  struct {
		TotalCount int `json:"totalCount"`
	} `json:"stargazers"`
	ForkCount int `json:"forkCount"`
	Issues    struct {
		TotalCount int `json:"totalCount"`
	} `json:"issues"`
	PrimaryLanguage *struct {
		Name string `json:"name"`
	} `json:"primaryLanguage"`
	DefaultBranchRef *struct {
		Name string `json:"name"`
	} `json:"defaultBranchRef"`
	RepositoryTopics struct {
		Nodes []struct {
			Topic struct {
				Name string `json:"name"`
			} `json:"topic"`
		} `json:"nodes"`
	} `json:"repositoryTopics"`
	UpdatedAt time.Time `json:"updatedAt"`
	Readme    *struct {
		Text string `json:"text"`
	} `json:"readme"`
	ReadmeLower *struct {
		Text string `json:"text"`
	} `json:"readmeLower"`
}

func (n gqlRepoNode) toRepo() Repo {
	r := Repo{
		Owner:       n.Owner.Login,
		Name:        n.Name,
		FullName:    n.NameWithOwner,
		Description: n.Description,
		URL:         n.URL,
		Visibility:  strings.ToLower(n.Visibility),
		IsPrivate:   n.IsPrivate,
		IsFork:      n.IsFork,
		IsArchived:  n.IsArchived,
		Stars:       n.Stargazers.TotalCount,
		Forks:       n.ForkCount,
		OpenIssues:  n.Issues.TotalCount,
		UpdatedAt:   n.UpdatedAt,
	}
	if r.Visibility == "" {
		if r.IsPrivate {
			r.Visibility = "private"
		} else {
			r.Visibility = "public"
		}
	}
	if n.PrimaryLanguage != nil {
		r.Language = n.PrimaryLanguage.Name
	}
	if n.DefaultBranchRef != nil {
		r.DefaultBranch = n.DefaultBranchRef.Name
	}
	for _, t := range n.RepositoryTopics.Nodes {
		if t.Topic.Name != "" {
			r.Topics = append(r.Topics, t.Topic.Name)
		}
	}
	if n.Readme != nil && n.Readme.Text != "" {
		r.Readme = n.Readme.Text
	} else if n.ReadmeLower != nil && n.ReadmeLower.Text != "" {
		r.Readme = n.ReadmeLower.Text
	}
	return r
}

func (c *githubClient) ListRepos(ctx context.Context, limit int) ([]Repo, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `query($limit: Int!) {
		viewer {
			repositories(first: $limit, affiliations: [OWNER, COLLABORATOR, ORGANIZATION_MEMBER], orderBy: {field: PUSHED_AT, direction: DESC}) {
				nodes {
					nameWithOwner
					name
					owner { login }
					description
					url
					isPrivate
					isFork
					isArchived
					visibility
					stargazers { totalCount }
					forkCount
					issues(states: OPEN) { totalCount }
					primaryLanguage { name }
					defaultBranchRef { name }
					updatedAt
				}
			}
		}
	}`
	var resp struct {
		Viewer struct {
			Repositories struct {
				Nodes []gqlRepoNode `json:"nodes"`
			} `json:"repositories"`
		} `json:"viewer"`
	}
	if err := c.gql.DoWithContext(ctx, q, map[string]any{"limit": limit}, &resp); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(resp.Viewer.Repositories.Nodes))
	for _, n := range resp.Viewer.Repositories.Nodes {
		if n.NameWithOwner != "" {
			out = append(out, n.toRepo())
		}
	}
	return out, nil
}

func (c *githubClient) SearchRepos(ctx context.Context, query string, limit int) ([]Repo, error) {
	if limit <= 0 {
		limit = 20
	}
	const q = `query($query: String!, $limit: Int!) {
		search(query: $query, type: REPOSITORY, first: $limit) {
			nodes {
				... on Repository {
					nameWithOwner
					name
					owner { login }
					description
					url
					isPrivate
					isFork
					isArchived
					visibility
					stargazers { totalCount }
					forkCount
					issues(states: OPEN) { totalCount }
					primaryLanguage { name }
					defaultBranchRef { name }
					updatedAt
				}
			}
		}
	}`
	var resp struct {
		Search struct {
			Nodes []gqlRepoNode `json:"nodes"`
		} `json:"search"`
	}
	if err := c.gql.DoWithContext(ctx, q, map[string]any{"query": query, "limit": limit}, &resp); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(resp.Search.Nodes))
	for _, n := range resp.Search.Nodes {
		if n.NameWithOwner != "" {
			out = append(out, n.toRepo())
		}
	}
	return out, nil
}

func (c *githubClient) GetRepoOverview(ctx context.Context, repo string) (Repo, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return Repo{}, err
	}
	const q = `query($owner: String!, $name: String!) {
		repository(owner: $owner, name: $name) {
			nameWithOwner
			name
			owner { login }
			description
			url
			isPrivate
			isFork
			isArchived
			visibility
			stargazers { totalCount }
			forkCount
			issues(states: OPEN) { totalCount }
			primaryLanguage { name }
			defaultBranchRef { name }
			repositoryTopics(first: 10) {
				nodes { topic { name } }
			}
			updatedAt
			readme: object(expression: "HEAD:README.md") {
				... on Blob { text }
			}
			readmeLower: object(expression: "HEAD:readme.md") {
				... on Blob { text }
			}
		}
	}`
	var resp struct {
		Repository *gqlRepoNode `json:"repository"`
	}
	if err := c.gql.DoWithContext(ctx, q, map[string]any{"owner": owner, "name": name}, &resp); err != nil {
		return Repo{}, err
	}
	if resp.Repository == nil {
		return Repo{}, fmt.Errorf("repository %s not found", repo)
	}
	return resp.Repository.toRepo(), nil
}

func (c *githubClient) ListBranches(ctx context.Context, repo string) ([]Branch, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	const q = `query($owner: String!, $name: String!) {
		repository(owner: $owner, name: $name) {
			defaultBranchRef { name }
			refs(refPrefix: "refs/heads/", first: 100, orderBy: {field: TAG_COMMIT_DATE, direction: DESC}) {
				nodes {
					name
					branchProtectionRule { id }
					target {
						... on Commit {
							oid
							committedDate
						}
					}
					associatedPullRequests(first: 1, states: [OPEN]) {
						nodes {
							number
							title
							state
							isDraft
							url
							headRefName
							baseRefName
							author { login }
						}
					}
				}
			}
		}
	}`
	var resp struct {
		Repository *struct {
			DefaultBranchRef *struct {
				Name string `json:"name"`
			} `json:"defaultBranchRef"`
			Refs struct {
				Nodes []struct {
					Name                 string `json:"name"`
					BranchProtectionRule *struct {
						ID string `json:"id"`
					} `json:"branchProtectionRule"`
					Target struct {
						OID           string    `json:"oid"`
						CommittedDate time.Time `json:"committedDate"`
					} `json:"target"`
					AssociatedPullRequests struct {
						Nodes []struct {
							Number      int    `json:"number"`
							Title       string `json:"title"`
							State       string `json:"state"`
							IsDraft     bool   `json:"isDraft"`
							URL         string `json:"url"`
							HeadRefName string `json:"headRefName"`
							BaseRefName string `json:"baseRefName"`
							Author      *struct {
								Login string `json:"login"`
							} `json:"author"`
						} `json:"nodes"`
					} `json:"associatedPullRequests"`
				} `json:"nodes"`
			} `json:"refs"`
		} `json:"repository"`
	}
	if err := c.gql.DoWithContext(ctx, q, map[string]any{"owner": owner, "name": name}, &resp); err != nil {
		return nil, err
	}
	if resp.Repository == nil {
		return nil, fmt.Errorf("repository %s not found", repo)
	}
	defaultName := ""
	if resp.Repository.DefaultBranchRef != nil {
		defaultName = resp.Repository.DefaultBranchRef.Name
	}
	out := make([]Branch, 0, len(resp.Repository.Refs.Nodes))
	for _, n := range resp.Repository.Refs.Nodes {
		b := Branch{
			Name:      n.Name,
			SHA:       n.Target.OID,
			Protected: n.BranchProtectionRule != nil,
			IsDefault: n.Name == defaultName,
			UpdatedAt: n.Target.CommittedDate,
		}
		if len(n.AssociatedPullRequests.Nodes) > 0 {
			pr := n.AssociatedPullRequests.Nodes[0]
			author := ""
			if pr.Author != nil {
				author = pr.Author.Login
			}
			b.LinkedPR = &PullRequest{
				Number:  pr.Number,
				Title:   pr.Title,
				State:   pr.State,
				IsDraft: pr.IsDraft,
				URL:     pr.URL,
				HeadRef: pr.HeadRefName,
				BaseRef: pr.BaseRefName,
				Author:  author,
			}
		}
		out = append(out, b)
	}
	return out, nil
}

func (c *githubClient) ListBranchCommits(ctx context.Context, repo, branch string, limit int) ([]Commit, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	path := fmt.Sprintf("repos/%s/%s/commits?sha=%s&per_page=%d",
		url.PathEscape(owner), url.PathEscape(name), url.QueryEscape(branch), limit)
	var raw []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string    `json:"name"`
				Date time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
		Author *struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if err := c.rest.DoWithContext(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Commit, 0, len(raw))
	for _, r := range raw {
		short := r.SHA
		if len(short) > 7 {
			short = short[:7]
		}
		author := r.Commit.Author.Name
		if r.Author != nil && r.Author.Login != "" {
			author = r.Author.Login
		}
		msg, _, _ := strings.Cut(r.Commit.Message, "\n")
		out = append(out, Commit{
			SHA:      r.SHA,
			ShortSHA: short,
			Message:  strings.TrimSpace(msg),
			Author:   author,
			Date:     r.Commit.Author.Date,
		})
	}
	return out, nil
}

func (c *githubClient) CreateBranch(ctx context.Context, repo, name, baseRef string) error {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	baseRef = strings.TrimSpace(baseRef)
	if baseRef == "" {
		baseRef = "HEAD"
	}
	refPath := fmt.Sprintf("repos/%s/%s/git/ref/heads/%s",
		url.PathEscape(owner), url.PathEscape(repoName), escapeGitRef(baseRef))
	var refResp struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.rest.DoWithContext(ctx, http.MethodGet, refPath, nil, &refResp); err != nil {
		return fmt.Errorf("resolve base branch %s: %w", baseRef, err)
	}
	body, err := json.Marshal(map[string]string{
		"ref": "refs/heads/" + name,
		"sha": refResp.Object.SHA,
	})
	if err != nil {
		return err
	}
	createPath := fmt.Sprintf("repos/%s/%s/git/refs", url.PathEscape(owner), url.PathEscape(repoName))
	return c.rest.DoWithContext(ctx, http.MethodPost, createPath, bytes.NewReader(body), nil)
}

func (c *githubClient) DeleteBranch(ctx context.Context, repo, name string) error {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("repos/%s/%s/git/refs/heads/%s",
		url.PathEscape(owner), url.PathEscape(repoName), escapeGitRef(name))
	return c.rest.DoWithContext(ctx, http.MethodDelete, path, nil, nil)
}

func escapeGitRef(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func (c *githubClient) ListPRs(ctx context.Context, repo string, limit int) ([]PullRequest, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	const q = `query($owner: String!, $name: String!, $limit: Int!) {
		repository(owner: $owner, name: $name) {
			pullRequests(first: $limit, orderBy: {field: UPDATED_AT, direction: DESC}) {
				nodes {
					number
					title
					body
					state
					isDraft
					url
					headRefName
					baseRefName
					additions
					deletions
					changedFiles
					reviewDecision
					createdAt
					updatedAt
					mergedAt
					author { login }
					comments { totalCount }
					labels(first: 10) { nodes { name } }
					commits(last: 1) {
						nodes {
							commit {
								statusCheckRollup { state }
							}
						}
					}
				}
			}
		}
	}`
	var resp struct {
		Repository *struct {
			PullRequests struct {
				Nodes []struct {
					Number         int        `json:"number"`
					Title          string     `json:"title"`
					Body           string     `json:"body"`
					State          string     `json:"state"`
					IsDraft        bool       `json:"isDraft"`
					URL            string     `json:"url"`
					HeadRefName    string     `json:"headRefName"`
					BaseRefName    string     `json:"baseRefName"`
					Additions      int        `json:"additions"`
					Deletions      int        `json:"deletions"`
					ChangedFiles   int        `json:"changedFiles"`
					ReviewDecision string     `json:"reviewDecision"`
					CreatedAt      time.Time  `json:"createdAt"`
					UpdatedAt      time.Time  `json:"updatedAt"`
					MergedAt       *time.Time `json:"mergedAt"`
					Author         *struct {
						Login string `json:"login"`
					} `json:"author"`
					Comments struct {
						TotalCount int `json:"totalCount"`
					} `json:"comments"`
					Labels struct {
						Nodes []struct {
							Name string `json:"name"`
						} `json:"nodes"`
					} `json:"labels"`
					Commits struct {
						Nodes []struct {
							Commit struct {
								StatusCheckRollup *struct {
									State string `json:"state"`
								} `json:"statusCheckRollup"`
							} `json:"commit"`
						} `json:"nodes"`
					} `json:"commits"`
				} `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	if err := c.gql.DoWithContext(ctx, q, map[string]any{"owner": owner, "name": name, "limit": limit}, &resp); err != nil {
		return nil, err
	}
	if resp.Repository == nil {
		return nil, fmt.Errorf("repository %s not found", repo)
	}
	out := make([]PullRequest, 0, len(resp.Repository.PullRequests.Nodes))
	for _, n := range resp.Repository.PullRequests.Nodes {
		author := ""
		if n.Author != nil {
			author = n.Author.Login
		}
		var labels []string
		for _, l := range n.Labels.Nodes {
			labels = append(labels, l.Name)
		}
		checks := ""
		if len(n.Commits.Nodes) > 0 && n.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
			checks = n.Commits.Nodes[0].Commit.StatusCheckRollup.State
		}
		out = append(out, PullRequest{
			Number:         n.Number,
			Title:          n.Title,
			Body:           n.Body,
			State:          n.State,
			IsDraft:        n.IsDraft,
			Author:         author,
			HeadRef:        n.HeadRefName,
			BaseRef:        n.BaseRefName,
			URL:            n.URL,
			Additions:      n.Additions,
			Deletions:      n.Deletions,
			ChangedFiles:   n.ChangedFiles,
			CommentsCount:  n.Comments.TotalCount,
			ReviewDecision: n.ReviewDecision,
			ChecksState:    checks,
			Labels:         labels,
			CreatedAt:      n.CreatedAt,
			UpdatedAt:      n.UpdatedAt,
			MergedAt:       n.MergedAt,
		})
	}
	return out, nil
}

func (c *githubClient) ListIssues(ctx context.Context, repo string, limit int) ([]Issue, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	const q = `query($owner: String!, $name: String!, $limit: Int!) {
		repository(owner: $owner, name: $name) {
			issues(first: $limit, orderBy: {field: UPDATED_AT, direction: DESC}) {
				nodes {
					number
					title
					body
					state
					url
					createdAt
					updatedAt
					author { login }
					assignees(first: 5) { nodes { login } }
					labels(first: 10) { nodes { name } }
					comments { totalCount }
				}
			}
		}
	}`
	var resp struct {
		Repository *struct {
			Issues struct {
				Nodes []struct {
					Number    int       `json:"number"`
					Title     string    `json:"title"`
					Body      string    `json:"body"`
					State     string    `json:"state"`
					URL       string    `json:"url"`
					CreatedAt time.Time `json:"createdAt"`
					UpdatedAt time.Time `json:"updatedAt"`
					Author    *struct {
						Login string `json:"login"`
					} `json:"author"`
					Assignees struct {
						Nodes []struct {
							Login string `json:"login"`
						} `json:"nodes"`
					} `json:"assignees"`
					Labels struct {
						Nodes []struct {
							Name string `json:"name"`
						} `json:"nodes"`
					} `json:"labels"`
					Comments struct {
						TotalCount int `json:"totalCount"`
					} `json:"comments"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	}
	if err := c.gql.DoWithContext(ctx, q, map[string]any{"owner": owner, "name": name, "limit": limit}, &resp); err != nil {
		return nil, err
	}
	if resp.Repository == nil {
		return nil, fmt.Errorf("repository %s not found", repo)
	}
	out := make([]Issue, 0, len(resp.Repository.Issues.Nodes))
	for _, n := range resp.Repository.Issues.Nodes {
		author := ""
		if n.Author != nil {
			author = n.Author.Login
		}
		var assignees, labels []string
		for _, a := range n.Assignees.Nodes {
			assignees = append(assignees, a.Login)
		}
		for _, l := range n.Labels.Nodes {
			labels = append(labels, l.Name)
		}
		out = append(out, Issue{
			Number:        n.Number,
			Title:         n.Title,
			Body:          n.Body,
			State:         n.State,
			Author:        author,
			Assignees:     assignees,
			Labels:        labels,
			CommentsCount: n.Comments.TotalCount,
			URL:           n.URL,
			CreatedAt:     n.CreatedAt,
			UpdatedAt:     n.UpdatedAt,
		})
	}
	return out, nil
}

func (c *githubClient) ListIssueComments(ctx context.Context, repo string, number int) ([]IssueComment, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("repos/%s/%s/issues/%d/comments?per_page=30",
		url.PathEscape(owner), url.PathEscape(name), number)
	var raw []struct {
		ID        int64     `json:"id"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
		User      *struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := c.rest.DoWithContext(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	out := make([]IssueComment, 0, len(raw))
	for _, r := range raw {
		author := ""
		if r.User != nil {
			author = r.User.Login
		}
		out = append(out, IssueComment{
			ID:        fmt.Sprint(r.ID),
			Author:    author,
			Body:      r.Body,
			CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

func (c *githubClient) CreateIssue(ctx context.Context, repo, title, body string) (Issue, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return Issue{}, err
	}
	payload, err := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
	})
	if err != nil {
		return Issue{}, err
	}
	path := fmt.Sprintf("repos/%s/%s/issues", url.PathEscape(owner), url.PathEscape(name))
	var raw struct {
		Number    int       `json:"number"`
		Title     string    `json:"title"`
		Body      string    `json:"body"`
		State     string    `json:"state"`
		HTMLURL   string    `json:"html_url"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		User      *struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := c.rest.DoWithContext(ctx, http.MethodPost, path, bytes.NewReader(payload), &raw); err != nil {
		return Issue{}, err
	}
	author := ""
	if raw.User != nil {
		author = raw.User.Login
	}
	return Issue{
		Number:    raw.Number,
		Title:     raw.Title,
		Body:      raw.Body,
		State:     strings.ToUpper(raw.State),
		Author:    author,
		URL:       raw.HTMLURL,
		CreatedAt: raw.CreatedAt,
		UpdatedAt: raw.UpdatedAt,
	}, nil
}

func (c *githubClient) CommentIssue(ctx context.Context, repo string, number int, body string) (IssueComment, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return IssueComment{}, err
	}
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return IssueComment{}, err
	}
	path := fmt.Sprintf("repos/%s/%s/issues/%d/comments", url.PathEscape(owner), url.PathEscape(name), number)
	var raw struct {
		ID        int64     `json:"id"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
		User      *struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := c.rest.DoWithContext(ctx, http.MethodPost, path, bytes.NewReader(payload), &raw); err != nil {
		return IssueComment{}, err
	}
	author := ""
	if raw.User != nil {
		author = raw.User.Login
	}
	return IssueComment{
		ID:        fmt.Sprint(raw.ID),
		Author:    author,
		Body:      raw.Body,
		CreatedAt: raw.CreatedAt,
	}, nil
}

func (c *githubClient) SetIssueState(ctx context.Context, repo string, number int, state string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"state": strings.ToLower(state)})
	if err != nil {
		return err
	}
	path := fmt.Sprintf("repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(name), number)
	return c.rest.DoWithContext(ctx, http.MethodPatch, path, bytes.NewReader(payload), nil)
}

func (c *githubClient) ListWorkflowRuns(ctx context.Context, repo string, limit int) ([]WorkflowRun, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 30
	}
	path := fmt.Sprintf("repos/%s/%s/actions/runs?per_page=%d",
		url.PathEscape(owner), url.PathEscape(name), limit)
	var resp struct {
		WorkflowRuns []struct {
			ID           int64     `json:"id"`
			Name         string    `json:"name"`
			DisplayTitle string    `json:"display_title"`
			Status       string    `json:"status"`
			Conclusion   string    `json:"conclusion"`
			HeadBranch   string    `json:"head_branch"`
			Event        string    `json:"event"`
			HeadSHA      string    `json:"head_sha"`
			RunNumber    int       `json:"run_number"`
			HTMLURL      string    `json:"html_url"`
			CreatedAt    time.Time `json:"created_at"`
			UpdatedAt    time.Time `json:"updated_at"`
		} `json:"workflow_runs"`
	}
	if err := c.rest.DoWithContext(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]WorkflowRun, 0, len(resp.WorkflowRuns))
	for _, r := range resp.WorkflowRuns {
		out = append(out, WorkflowRun{
			ID:           r.ID,
			Name:         r.Name,
			WorkflowName: r.Name,
			DisplayTitle: r.DisplayTitle,
			Status:       r.Status,
			Conclusion:   r.Conclusion,
			Branch:       r.HeadBranch,
			Event:        r.Event,
			HeadSHA:      r.HeadSHA,
			RunNumber:    r.RunNumber,
			URL:          r.HTMLURL,
			CreatedAt:    r.CreatedAt,
			UpdatedAt:    r.UpdatedAt,
		})
	}
	return out, nil
}

func (c *githubClient) ListWorkflowJobs(ctx context.Context, repo string, runID int64) ([]WorkflowJob, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("repos/%s/%s/actions/runs/%d/jobs?per_page=50",
		url.PathEscape(owner), url.PathEscape(name), runID)
	var resp struct {
		Jobs []struct {
			ID          int64     `json:"id"`
			Name        string    `json:"name"`
			Status      string    `json:"status"`
			Conclusion  string    `json:"conclusion"`
			StartedAt   time.Time `json:"started_at"`
			CompletedAt time.Time `json:"completed_at"`
			Steps       []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				Number     int    `json:"number"`
			} `json:"steps"`
		} `json:"jobs"`
	}
	if err := c.rest.DoWithContext(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]WorkflowJob, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		steps := make([]WorkflowStep, 0, len(j.Steps))
		for _, st := range j.Steps {
			steps = append(steps, WorkflowStep{
				Name:       st.Name,
				Status:     st.Status,
				Conclusion: st.Conclusion,
				Number:     st.Number,
			})
		}
		out = append(out, WorkflowJob{
			ID:          j.ID,
			Name:        j.Name,
			Status:      j.Status,
			Conclusion:  j.Conclusion,
			StartedAt:   j.StartedAt,
			CompletedAt: j.CompletedAt,
			Steps:       steps,
		})
	}
	return out, nil
}

func (c *githubClient) RerunWorkflow(ctx context.Context, repo string, runID int64, failedOnly bool) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	action := "rerun"
	if failedOnly {
		action = "rerun-failed-jobs"
	}
	path := fmt.Sprintf("repos/%s/%s/actions/runs/%d/%s",
		url.PathEscape(owner), url.PathEscape(name), runID, action)
	return c.rest.DoWithContext(ctx, http.MethodPost, path, nil, nil)
}

func (c *githubClient) ListDir(ctx context.Context, repo, ref, dirPath string) ([]TreeEntry, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	clean := strings.Trim(dirPath, "/")
	apiPath := fmt.Sprintf("repos/%s/%s/contents", url.PathEscape(owner), url.PathEscape(name))
	if clean != "" {
		apiPath += "/" + escapeGitRef(clean)
	}
	if ref != "" {
		apiPath += "?ref=" + url.QueryEscape(ref)
	}
	var raw []struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Type    string `json:"type"`
		Size    int    `json:"size"`
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
	}
	if err := c.rest.DoWithContext(ctx, http.MethodGet, apiPath, nil, &raw); err != nil {
		return nil, err
	}
	out := make([]TreeEntry, 0, len(raw))
	for _, e := range raw {
		out = append(out, TreeEntry{
			Name: e.Name,
			Path: e.Path,
			Type: e.Type,
			Size: e.Size,
			SHA:  e.SHA,
			URL:  e.HTMLURL,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		di := out[i].Type == "dir"
		dj := out[j].Type == "dir"
		if di != dj {
			return di
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (c *githubClient) GetFile(ctx context.Context, repo, ref, filePath string) (FileContent, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return FileContent{}, err
	}
	clean := strings.Trim(filePath, "/")
	apiPath := fmt.Sprintf("repos/%s/%s/contents/%s",
		url.PathEscape(owner), url.PathEscape(name), escapeGitRef(clean))
	if ref != "" {
		apiPath += "?ref=" + url.QueryEscape(ref)
	}
	var raw struct {
		Path     string `json:"path"`
		Size     int    `json:"size"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := c.rest.DoWithContext(ctx, http.MethodGet, apiPath, nil, &raw); err != nil {
		return FileContent{}, err
	}
	fc := FileContent{
		Path:     raw.Path,
		Ref:      ref,
		Size:     raw.Size,
		Encoding: raw.Encoding,
	}
	if fc.Path == "" {
		fc.Path = clean
	}
	if raw.Encoding == "base64" {
		cleaned := strings.ReplaceAll(raw.Content, "\n", "")
		decoded, err := base64.StdEncoding.DecodeString(cleaned)
		if err != nil {
			return FileContent{}, fmt.Errorf("decode file %s: %w", clean, err)
		}
		if len(decoded) > maxPreviewBytes {
			decoded = decoded[:maxPreviewBytes]
			fc.Truncated = true
		}
		if isBinaryContent(decoded) {
			fc.Binary = true
			return fc, nil
		}
		fc.Content = string(decoded)
		return fc, nil
	}
	if raw.Size > maxPreviewBytes && raw.Content == "" {
		fc.Truncated = true
		return fc, nil
	}
	b := []byte(raw.Content)
	if len(b) > maxPreviewBytes {
		b = b[:maxPreviewBytes]
		fc.Truncated = true
	}
	if isBinaryContent(b) {
		fc.Binary = true
		return fc, nil
	}
	fc.Content = string(b)
	return fc, nil
}

func isBinaryContent(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	sample := b
	if len(sample) > 8000 {
		sample = sample[:8000]
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return true
	}
	return !utf8.Valid(sample)
}
