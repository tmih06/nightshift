package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/tmih06/nightshift/internal/services/auth/providers"
)

// Account is one signed-in login and how its renewals have gone.
type Account struct {
	Name       string               `json:"name"`
	Provider   string               `json:"provider"`
	Credential providers.Credential `json:"credential"`
	Disabled   bool                 `json:"disabled,omitempty"`
	AddedAt    time.Time            `json:"addedAt"`
	// RefreshedAt is the last successful renewal.
	RefreshedAt  time.Time `json:"refreshedAt,omitzero"`
	RefreshError string    `json:"refreshError,omitempty"`
	// TriedAt is the last renewal attempt, failed or not.
	TriedAt time.Time `json:"triedAt,omitzero"`
	// NeedsSignIn says the login can no longer be renewed.
	NeedsSignIn bool `json:"needsSignIn,omitempty"`
	Refreshes   int  `json:"refreshes,omitempty"`
	Failures    int  `json:"failures,omitempty"`
}

// Label is how the account is named on screen and in the proxy's usage.
func (a *Account) Label() string {
	if a.Credential.Email != "" {
		return a.Credential.Email
	}
	return a.Name
}

// Published is what the last publish put into the cluster.
type Published struct {
	At       time.Time                   `json:"at,omitzero"`
	Accounts map[string]PublishedAccount `json:"accounts,omitempty"`
}

// PublishedAccount is the account one agent's sandboxes got.
type PublishedAccount struct {
	Name    string    `json:"name"`
	Label   string    `json:"label"`
	Expires time.Time `json:"expires,omitzero"`
	Chain   []string  `json:"chain,omitempty"`
}

// State is everything the vault keeps.
type State struct {
	Accounts  []Account           `json:"accounts"`
	Chains    map[string][]string `json:"chains,omitempty"`
	Published Published           `json:"published"`
}

// Store keeps State in one file only its owner can read. Writes replace the
// file atomically, so a crash leaves the old state or the new one.
type Store struct{ path string }

// NewStore keeps the state in dir, made private when it is new. The file
// itself is always written 0600.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("make the vault directory: %w", err)
	}
	return &Store{path: filepath.Join(dir, "accounts.json")}, nil
}

// Load reads the state; a vault never written is empty.
func (s *Store) Load() (State, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{Chains: map[string][]string{}}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read the vault: %w", err)
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, fmt.Errorf("parse the vault: %w", err)
	}
	if st.Chains == nil {
		st.Chains = map[string][]string{}
	}
	return st, nil
}

// Save replaces the state.
func (s *Store) Save(st *State) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the vault: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".accounts-*.json")
	if err != nil {
		return fmt.Errorf("write the vault: %w", err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(raw)
	serr := tmp.Sync()
	cerr := tmp.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		return fmt.Errorf("write the vault: %w", errors.Join(err, os.Remove(name)))
	}
	if err := os.Rename(name, s.path); err != nil {
		return fmt.Errorf("replace the vault: %w", errors.Join(err, os.Remove(name)))
	}
	return nil
}
