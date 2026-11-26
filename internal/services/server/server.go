// Package server is the HTTP face of the Nightshift services: it implements
// the contract in api/openapi.yaml over whatever backends it is given, and
// knows nothing of Kubernetes.
package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"charm.land/log/v2"

	"github.com/tmih06/nightshift/api"
	v1 "github.com/tmih06/nightshift/api/v1"
)

// maxBody bounds a request body. Nothing in the API sends more than a few
// kilobytes.
const maxBody = 1 << 20

// Config is everything a Server needs.
type Config struct {
	// Token is the bearer token every request but /v1/healthz must carry.
	Token string
	// Hosts are the Host header names, besides loopback, that requests may
	// use. It defeats DNS rebinding from a web page.
	Hosts []string
	// AnyHost accepts every Host header, for a server behind a gateway that
	// has its own names. The token is still required.
	AnyHost bool
	// Version is the server's build, for bug reports.
	Version string
	// Cluster says which cluster the services act on.
	Cluster v1.ClusterInfo
	// WatchInterval is how often a fleet watch looks; default 2s.
	WatchInterval time.Duration

	Fleet     Fleet
	Claims    Claims
	Checkouts Checkouts
	Volumes   Volumes
	Terminals Terminals
	Accounts  Accounts
}

// Server serves the API.
type Server struct {
	cfg       Config
	tokenHash [32]byte
	hosts     map[string]bool
}

type handler func(http.ResponseWriter, *http.Request) error

type route struct {
	method  string
	pattern string
	handle  handler
	public  bool
}

// New returns a Server, or says what is wrong with cfg. A server without a
// token would let anyone who can reach it open a shell in a sandbox, so it
// does not exist.
func New(cfg Config) (*Server, error) {
	if cfg.Token == "" {
		return nil, errors.New("a token is required: the API can open a shell in a sandbox")
	}
	if cfg.WatchInterval <= 0 {
		cfg.WatchInterval = 2 * time.Second
	}
	s := &Server{cfg: cfg, tokenHash: sha256.Sum256([]byte(cfg.Token)), hosts: map[string]bool{
		"localhost": true, "127.0.0.1": true, "::1": true,
	}}
	for _, h := range cfg.Hosts {
		s.hosts[strings.ToLower(h)] = true
	}
	return s, nil
}

// Routes lists every "METHOD /path" the server answers, which a test compares
// with the contract.
func (s *Server) Routes() []string {
	rs := s.routes()
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.method + " " + r.pattern
	}
	return out
}

func (s *Server) routes() []route {
	var rs []route
	for _, group := range [][]route{s.metaRoutes(), s.fleetRoutes(), s.claimRoutes(), s.checkoutRoutes(), s.terminalRoutes(), s.accountRoutes()} {
		rs = append(rs, group...)
	}
	return rs
}

// Handler is the whole API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.routes() {
		mux.Handle(r.method+" "+r.pattern, s.wrap(r))
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, notFound("no such endpoint"))
	})
	return mux
}

// wrap puts a route behind the checks every request gets, and turns what the
// handler returns into a response.
func (s *Server) wrap(rt route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Error("handler panicked", "route", rt.method+" "+rt.pattern, "panic", v)
				writeProblem(w, errors.New("the server failed"))
			}
		}()
		if !s.hostOK(r.Host) {
			writeProblem(w, &apiError{status: http.StatusForbidden, code: v1.CodeUnauthorized, detail: "this Host is not allowed"})
			return
		}
		if !rt.public && !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeProblem(w, &apiError{status: http.StatusUnauthorized, code: v1.CodeUnauthorized, detail: "send Authorization: Bearer <token>"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := rt.handle(w, r); err != nil {
			writeProblem(w, err)
		}
	})
}

func (s *Server) hostOK(host string) bool {
	if s.cfg.AnyHost {
		return true
	}
	name, _, err := net.SplitHostPort(host)
	if err != nil {
		name = strings.Trim(host, "[]")
	}
	return s.hosts[strings.ToLower(name)]
}

// authorized compares the bearer token in constant time, over hashes so that
// its length does not leak either.
func (s *Server) authorized(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	sum := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(sum[:], s.tokenHash[:]) == 1
}

// decode reads a JSON request body into v. Fields it does not know are
// ignored, as the contract promises.
func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return invalid("the body is not the JSON this endpoint takes: " + err.Error())
	}
	return nil
}

// need is x, or the error that says the server does not offer what it stands
// for.
func need[T any](x T, what string) (T, error) {
	if any(x) == nil {
		return x, unavailable("this server does not offer " + what)
	}
	return x, nil
}

func (s *Server) metaRoutes() []route {
	return []route{
		{method: http.MethodGet, pattern: "/v1/healthz", public: true, handle: func(w http.ResponseWriter, _ *http.Request) error {
			w.Header().Set("Content-Type", "text/plain")
			_, err := w.Write([]byte("ok\n"))
			return wrap("write the answer", err)
		}},
		{method: http.MethodGet, pattern: "/v1", handle: s.info},
		{method: http.MethodGet, pattern: "/v1/openapi.yaml", handle: func(w http.ResponseWriter, _ *http.Request) error {
			w.Header().Set("Content-Type", "application/yaml")
			_, err := w.Write(api.OpenAPI)
			return wrap("write the answer", err)
		}},
	}
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) error {
	caps := []string{}
	for _, c := range []struct {
		name string
		has  bool
	}{
		{v1.CapFleetWatch, s.cfg.Fleet != nil},
		{v1.CapAccounts, s.cfg.Accounts != nil},
		{v1.CapCheckouts, s.cfg.Checkouts != nil},
		{v1.CapTerminals, s.cfg.Terminals != nil},
	} {
		if c.has {
			caps = append(caps, c.name)
		}
	}
	writeJSON(w, http.StatusOK, v1.Info{
		APIVersion:   v1.Version,
		Version:      s.cfg.Version,
		Cluster:      s.cfg.Cluster,
		Capabilities: caps,
	})
	return nil
}
