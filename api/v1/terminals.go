package v1

import "time"

// Terminal is one live terminal inside a claimed sandbox. Terminals belong to
// the sandbox, not to whoever is looking: they keep running when a frontend
// goes away, and another frontend can pick them up.
type Terminal struct {
	// Address names the terminal within its sandbox: t1, t2, and so on.
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
	// Command is the foreground command.
	Command string `json:"command,omitempty"`
	Title   string `json:"title,omitempty"`
	Status  string `json:"status,omitempty"`
	// CWD is the working directory.
	CWD       string    `json:"cwd,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Terminals is the body of GET /v1/sandboxes/{name}/terminals.
type Terminals struct {
	Terminals []Terminal `json:"terminals"`
}

// TerminalRequest opens a terminal.
type TerminalRequest struct {
	// Name is the terminal's name; empty lets the server pick one.
	Name string `json:"name,omitempty"`
	// From is the address of a terminal whose working directory the new one
	// starts in.
	From string `json:"from,omitempty"`
	// CWD is the directory to start in; From wins when both are set.
	CWD string `json:"cwd,omitempty"`
}

// Key is one key press sent to a terminal: typed text, or a named key such as
// Enter, Tab, Escape, Up, or C-c.
type Key struct {
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
}

// Input is the body of POST .../terminals/{address}/input. The keys arrive in
// order, and a second request does not overtake the first.
type Input struct {
	Keys []Key `json:"keys"`
}

// Screen is what a terminal shows: GET .../terminals/{address}/screen.
type Screen struct {
	// Changed is false when nothing was drawn before the wait ran out; Lines
	// is then empty and the client keeps what it has.
	Changed bool `json:"changed"`
	Cols    int  `json:"cols"`
	Rows    int  `json:"rows"`
	// Lines are the screen's rows, top first, with ANSI SGR styling.
	Lines []string `json:"lines,omitempty"`
}
