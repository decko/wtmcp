package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/LeGambiArt/wtmcp/pkg/handler"
	gogitlab "gitlab.com/gitlab-org/api/client-go"
)

// instance holds a named GitLab client.
type instance struct {
	Name   string
	URL    string
	Client *gogitlab.Client
}

// instances maps instance names to their clients.
var instances map[string]*instance

// defaultInstance is used when only one instance is configured
// or when no instance param is provided.
var defaultInstance string

// discoverInstances sets up a single GitLab client using the core's
// HTTP proxy for auth and transport. The core injects PRIVATE-TOKEN
// headers via the BearerProvider configured in plugin.yaml.
//
// Multi-instance is not supported under proxy mode because the proxy
// has a single auth provider — it cannot inject different tokens for
// different domains. Multi-instance users must wait for per-domain
// auth binding.
func discoverInstances(p *handler.Plugin) error {
	instances = make(map[string]*instance)
	defaultInstance = ""

	// Detect multi-instance configuration and reject it.
	if names := detectMultiInstance(); len(names) > 0 {
		return fmt.Errorf(
			"multi-instance (%s) is not supported under proxy mode; "+
				"use single GITLAB_TOKEN + GITLAB_URL instead",
			strings.Join(names, ", "),
		)
	}

	gitlabURL := os.Getenv("GITLAB_URL")
	if gitlabURL == "" {
		gitlabURL = "https://gitlab.com"
	}

	httpClient := handler.NewProxyTransport(p).Client()

	// Empty token: the go-gitlab SDK sends "Private-Token: " (empty)
	// on every request. The core proxy strips it (Private-Token is in
	// dangerousHeaders), then the BearerProvider re-injects the real
	// token from services.auth config.
	client, err := gogitlab.NewClient("",
		gogitlab.WithBaseURL(gitlabURL),
		gogitlab.WithHTTPClient(httpClient),
	)
	if err != nil {
		return fmt.Errorf("gitlab client: %w", err)
	}

	instances = map[string]*instance{
		"default": {Name: "default", URL: gitlabURL, Client: client},
	}
	defaultInstance = "default"

	// Register domain for proxy allowlist.
	if host := extractHost(gitlabURL); host != "" {
		p.SetInitDomains([]string{host})
	}

	return nil
}

// resolveInstance returns the client for the given instance name.
// If name is empty, returns the default (only works with single instance).
func resolveInstance(name string) (*gogitlab.Client, error) {
	if name == "" {
		if defaultInstance == "" {
			names := make([]string, 0, len(instances))
			for n := range instances {
				names = append(names, n)
			}
			return nil, fmt.Errorf("instance is required (available: %s)", strings.Join(names, ", "))
		}
		name = defaultInstance
	}

	inst, ok := instances[name]
	if !ok {
		names := make([]string, 0, len(instances))
		for n := range instances {
			names = append(names, n)
		}
		return nil, fmt.Errorf("unknown instance %q (available: %s)", name, strings.Join(names, ", "))
	}
	return inst.Client, nil
}

// detectMultiInstance scans the environment for GITLAB_{NAME}_TOKEN
// patterns that indicate multi-instance configuration.
func detectMultiInstance() []string {
	var names []string
	for _, env := range os.Environ() {
		key, value, ok := strings.Cut(env, "=")
		if !ok || value == "" {
			continue
		}
		if !strings.HasPrefix(key, "GITLAB_") || !strings.HasSuffix(key, "_TOKEN") {
			continue
		}
		if key == "GITLAB_TOKEN" || key == "GITLAB_SSL_VERIFY" {
			continue
		}

		name := strings.ToLower(key[7 : len(key)-6])
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// extractHost returns the hostname from a URL string.
func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
