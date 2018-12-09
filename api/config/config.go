package config

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

var config = &Config{}

// Config represents a configuration.
type Config struct {
	Project     string
	Instance    string
	Creds       string
	TokenSource oauth2.TokenSource

	ErrStream io.Writer
}

// RegisterFlags registers a set of standard flags for this config.
func (c *Config) registerFlags() {
	flag.StringVar(&c.Project, "project", c.Project, "project ID, if unset uses gcloud configured project")
	flag.StringVar(&c.Instance, "instance", c.Instance, "Cloud Bigtable instance")
	flag.StringVar(&c.Creds, "creds", c.Creds, "if set, use application credentials in this file")
}

// NewConfig returns initialized config.
func NewConfig(writer io.Writer) *Config {
	return &Config{
		ErrStream: writer,
	}
}

// Load returns initialized configuration
func (c *Config) Load() error {
	filename := filepath.Join(os.Getenv("HOME"), ".cbtrc")
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("Reading %s: %v", filename, err)
		}
	}
	s := bufio.NewScanner(bytes.NewReader(data))
	for s.Scan() {
		line := s.Text()
		i := strings.Index(line, "=")
		if i < 0 {
			return fmt.Errorf("Bad line in %s: %q", filename, line)
		}
		key, val := strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
		switch key {
		default:
			return fmt.Errorf("Unknown key in %s: %q", filename, key)
		case "project":
			c.Project = val
		case "instance":
			c.Instance = val
		case "creds":
			c.Creds = val
		}
	}

	c.registerFlags()
	flag.Parse()
	if err := c.setFromGcloud(); err != nil {
		return err
	}

	return s.Err()
}

type gcloudCredential struct {
	AccessToken string    `json:"access_token"`
	Expiry      time.Time `json:"token_expiry"`
}

func (cred *gcloudCredential) Token() *oauth2.Token {
	return &oauth2.Token{AccessToken: cred.AccessToken, TokenType: "Bearer", Expiry: cred.Expiry}
}

// GcloudConfig configuration fot the gcloud
type GcloudConfig struct {
	Configuration struct {
		Properties struct {
			Core struct {
				Project string `json:"project"`
			} `json:"core"`
		} `json:"properties"`
	} `json:"configuration"`
	Credential gcloudCredential `json:"credential"`
}

// GcloudCmdTokenSource represents gcloud command that returns a token source
type GcloudCmdTokenSource struct {
	Command string
	Args    []string
}

// Token implements the oauth2.TokenSource interface
func (g *GcloudCmdTokenSource) Token() (*oauth2.Token, error) {
	gcloudConfig, err := loadGcloudConfig(g.Command, g.Args)
	if err != nil {
		return nil, err
	}
	return gcloudConfig.Credential.Token(), nil
}

func loadGcloudConfig(gcloudCmd string, gcloudCmdArgs []string) (*GcloudConfig, error) {
	out, err := exec.Command(gcloudCmd, gcloudCmdArgs...).Output()
	if err != nil {
		return nil, fmt.Errorf("Could not retrieve gcloud configuration")
	}

	var gcloudConfig GcloudConfig
	if err := json.Unmarshal(out, &gcloudConfig); err != nil {
		return nil, fmt.Errorf("Could not parse gcloud configuration")
	}

	return &gcloudConfig, nil
}

func (c *Config) setFromGcloud() error {
	if c.Creds == "" {
		c.Creds = os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
		if c.Creds == "" {
			fmt.Fprintln(c.ErrStream, "-creds flag unset, will use gcloud credential")
		}
	} else {
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", c.Creds)
	}

	if c.Project == "" {
		fmt.Fprintln(c.ErrStream, "-project flag unset, will use gcloud active project")
	}

	if c.Creds != "" && c.Project != "" {
		return nil
	}

	gcloudCmd := "gcloud"
	if runtime.GOOS == "windows" {
		gcloudCmd = gcloudCmd + ".cmd"
	}

	gcloudCmdArgs := []string{"config", "config-helper",
		"--format=json(configuration.properties.core.project,credential)"}

	gcloudConfig, err := loadGcloudConfig(gcloudCmd, gcloudCmdArgs)
	if err != nil {
		return err
	}

	if c.Project == "" && gcloudConfig.Configuration.Properties.Core.Project != "" {
		fmt.Fprintf(c.ErrStream, "gcloud active project is \"%s\"\n", gcloudConfig.Configuration.Properties.Core.Project)
		c.Project = gcloudConfig.Configuration.Properties.Core.Project
	}

	if c.Creds == "" {
		c.TokenSource = oauth2.ReuseTokenSource(
			gcloudConfig.Credential.Token(),
			&GcloudCmdTokenSource{Command: gcloudCmd, Args: gcloudCmdArgs})
	}

	return nil
}
