package app_test

import (
	"strings"
	"testing"

	"github.com/tmih06/nightshift/internal/zenocto/tui/app"
	"github.com/tmih06/nightshift/internal/zenocto/tui/prview"
)

const prURL = "https://github.com/acme/rocket/pull/412"

func links(t *testing.T, copyErr, browseErr error) (copied, opened *string) {
	t.Helper()

	var gotCopy, gotBrowse string
	app.StubLinks(t,
		func(s string) error { gotCopy = s; return copyErr },
		func(s string) error { gotBrowse = s; return browseErr },
	)
	return &gotCopy, &gotBrowse
}

func TestTheDetailScreensLinkKeysReachTheSameHandlers(t *testing.T) {
	copied, opened := links(t, nil, nil)
	pr := samplePRs()[0]

	m := loaded(t, &fakeSearcher{prs: samplePRs()}, 120, 40)
	m = settle(m, prview.CopyLinkMsg{PR: pr}, prview.BrowseMsg{PR: pr})

	if *copied != prURL {
		t.Errorf("copied %q, want %q", *copied, prURL)
	}
	if *opened != prURL {
		t.Errorf("opened %q, want %q", *opened, prURL)
	}
	if bar := lastLine(render(t, m)); !strings.Contains(bar, "Copied the link to #412") {
		t.Errorf("status bar = %q, want the copy reported", strings.TrimSpace(bar))
	}
}
