package light

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nv-x/internal/config"
)

const defaultPort = "9123"

type Controller struct {
	cfg    config.LightConfig
	client *http.Client
	logf   func(string, ...any)

	opMu        sync.Mutex
	mu          sync.Mutex
	address     string
	lastDesired *bool
}

type lightsResponse struct {
	NumberOfLights int     `json:"numberOfLights"`
	Lights         []light `json:"lights"`
}

type light struct {
	On          int `json:"on"`
	Brightness  int `json:"brightness"`
	Temperature int `json:"temperature"`
}

type externalConfig struct {
	ActiveIP string `json:"activeIp"`
}

func NewController(cfg config.LightConfig, logf func(string, ...any)) *Controller {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	return &Controller{
		cfg:    cfg,
		client: &http.Client{Timeout: timeout},
		logf:   logf,
	}
}

func (c *Controller) Enabled() bool {
	return c != nil && c.cfg.Enabled
}

func (c *Controller) SetDesired(ctx context.Context, on bool) {
	if !c.Enabled() {
		return
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()

	c.mu.Lock()
	if c.lastDesired != nil && *c.lastDesired == on {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	if err := c.set(ctx, on); err != nil {
		c.logf("light auto-control skipped: %v", err)
		return
	}
	c.mu.Lock()
	desired := on
	c.lastDesired = &desired
	c.mu.Unlock()
	if on {
		c.logf("light auto-control: on")
	} else {
		c.logf("light auto-control: off")
	}
}

func (c *Controller) set(ctx context.Context, on bool) error {
	address, err := c.resolveAddress()
	if err != nil {
		return err
	}
	state, err := c.get(ctx, address)
	if err != nil {
		return err
	}
	if len(state.Lights) == 0 {
		return errors.New("device returned no lights")
	}
	state.Lights[0].On = 0
	if on {
		state.Lights[0].On = 1
		state.Lights[0].Brightness = c.cfg.Brightness
		state.Lights[0].Temperature = c.cfg.Temperature
	}
	if state.NumberOfLights <= 0 {
		state.NumberOfLights = len(state.Lights)
	}
	return c.put(ctx, address, state)
}

func (c *Controller) resolveAddress() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.address != "" {
		return c.address, nil
	}
	address := strings.TrimSpace(c.cfg.Address)
	if address == "" {
		address = strings.TrimSpace(loadElgatoToggleAddress())
	}
	if address == "" {
		return "", errors.New("no light address configured")
	}
	c.address = normalizeAddress(address)
	return c.address, nil
}

func (c *Controller) get(ctx context.Context, address string) (lightsResponse, error) {
	var out lightsResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(address), nil)
	if err != nil {
		return out, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return out, fmt.Errorf("get lights failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Controller) put(ctx context.Context, address string, state lightsResponse) error {
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint(address), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("update lights failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func endpoint(address string) string {
	return "http://" + normalizeAddress(address) + "/elgato/lights"
}

func normalizeAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return net.JoinHostPort("127.0.0.1", defaultPort)
	}
	if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
		address = parsed.Host
	}
	if host, port, err := net.SplitHostPort(address); err == nil && host != "" && port != "" {
		return net.JoinHostPort(host, port)
	}
	if strings.Contains(address, ":") && strings.Count(address, ":") > 1 {
		return net.JoinHostPort(address, defaultPort)
	}
	return net.JoinHostPort(address, defaultPort)
}

func loadElgatoToggleAddress() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".config", "elgato-light-toggle", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg externalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return cfg.ActiveIP
}
