package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

const upstreamHeader = "X-Lab-003-Upstream"

type config struct {
	targetID  string
	targetURL string
	readyPath string
	wafURL    string
	wafHost   string
	client    *http.Client
	direct    *http.Client
}

type fixture struct {
	ID      string
	Method  string
	Path    string
	Headers map[string]string
	Body    string
}

type fixtureResult struct {
	FixtureID         string `json:"fixture_id"`
	StatusCode        int    `json:"status_code"`
	Blocked           bool   `json:"blocked,omitempty"`
	UpstreamConfirmed bool   `json:"upstream_confirmed,omitempty"`
}

type validationResult struct {
	TargetID         string          `json:"target_id"`
	MaliciousResults []fixtureResult `json:"malicious_results"`
	BenignResults    []fixtureResult `json:"benign_results"`
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func loadConfig() (config, error) {
	cfg := config{
		targetID:  os.Getenv("LAB_003_TARGET_ID"),
		targetURL: os.Getenv("LAB_003_TARGET_URL"),
		readyPath: env("LAB_003_READY_PATH", "/"),
		wafURL:    env("LAB_003_WAF_URL", "http://bunkerweb:8080"),
		wafHost:   env("LAB_003_WAF_HOST", "target.lab.test"),
		client: &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
		direct: &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	if cfg.targetID == "" || cfg.targetURL == "" {
		return config{}, errors.New("LAB_003_TARGET_ID and LAB_003_TARGET_URL are required")
	}
	if _, err := url.ParseRequestURI(cfg.targetURL); err != nil {
		return config{}, fmt.Errorf("invalid target URL: %w", err)
	}
	return cfg, nil
}

func (c config) targetReady() error {
	req, err := http.NewRequest(http.MethodGet, c.targetURL+c.readyPath, nil)
	if err != nil {
		return err
	}
	resp, err := c.direct.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 500 {
		return fmt.Errorf("target returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c config) proxyHandler() (http.Handler, error) {
	target, err := url.Parse(c.targetURL)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DisableCompression:    true,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set(upstreamHeader, c.targetID)
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Printf("target proxy error: %v", err)
		http.Error(w, "target unavailable", http.StatusBadGateway)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__lab003_upstream_ready" {
			if err := c.targetReady(); err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			w.Header().Set(upstreamHeader, c.targetID)
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "ready\n")
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}

func (c config) replay(f fixture) (fixtureResult, error) {
	req, err := http.NewRequest(f.Method, c.wafURL+f.Path, bytes.NewBufferString(f.Body))
	if err != nil {
		return fixtureResult{}, err
	}
	req.Host = c.wafHost
	for name, value := range f.Headers {
		req.Header.Set(name, value)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fixtureResult{}, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	upstream := resp.Header.Get(upstreamHeader) == c.targetID
	return fixtureResult{
		FixtureID:         f.ID,
		StatusCode:        resp.StatusCode,
		Blocked:           resp.StatusCode == http.StatusTeapot && !upstream,
		UpstreamConfirmed: upstream && resp.StatusCode < 500,
	}, nil
}

func (c config) setupToken() (string, error) {
	resp, err := c.direct.Get(c.targetURL + "/api/session/properties")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var properties map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&properties); err != nil {
		return "", err
	}
	token, _ := properties["setup-token"].(string)
	if token == "" {
		return "", errors.New("Metabase setup-token is unavailable; restart the target to reset it")
	}
	return token, nil
}

func (c config) validate() (validationResult, error) {
	malicious, benign, err := fixtures(c.targetID)
	if err != nil {
		return validationResult{}, err
	}
	if c.targetID == "metabase/CVE-2023-38646" {
		token, err := c.setupToken()
		if err != nil {
			return validationResult{}, err
		}
		for i := range malicious {
			malicious[i].Body = strings.ReplaceAll(malicious[i].Body, "{{TOKEN}}", token)
		}
	}
	result := validationResult{TargetID: c.targetID}
	for _, f := range malicious {
		item, err := c.replay(f)
		if err != nil {
			return validationResult{}, fmt.Errorf("malicious fixture %s: %w", f.ID, err)
		}
		result.MaliciousResults = append(result.MaliciousResults, item)
	}
	for _, f := range benign {
		item, err := c.replay(f)
		if err != nil {
			return validationResult{}, fmt.Errorf("benign fixture %s: %w", f.ID, err)
		}
		result.BenignResults = append(result.BenignResults, item)
	}
	return result, nil
}

func (c config) controlHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if requested := r.URL.Query().Get("target"); requested != "" && requested != c.targetID {
			http.Error(w, "requested target is not active", http.StatusConflict)
			return
		}
		if err := c.targetReady(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"target_id": c.targetID, "status": "ready"})
	})
	mux.HandleFunc("POST /validate", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Target string `json:"target"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
			http.Error(w, "invalid JSON request", http.StatusBadRequest)
			return
		}
		if input.Target != c.targetID {
			http.Error(w, "requested target is not active", http.StatusConflict)
			return
		}
		result, err := c.validate()
		if err != nil {
			log.Printf("validation failed for %s: %v", c.targetID, err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	return mux
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		if err := cfg.targetReady(); err != nil {
			log.Fatal(err)
		}
		return
	}
	proxy, err := cfg.proxyHandler()
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		log.Printf("proxying %s on :8081", cfg.targetID)
		if err := http.ListenAndServe(":8081", proxy); err != nil {
			log.Fatal(err)
		}
	}()
	log.Printf("serving validation control API on :8080")
	log.Fatal(http.ListenAndServe(":8080", cfg.controlHandler()))
}
