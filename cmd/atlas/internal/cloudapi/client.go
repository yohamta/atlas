// Copyright 2021-present The Atlas Authors. All rights reserved.
// This source code is licensed under the Apache 2.0 license found
// in the LICENSE file in the root directory of this source tree.

package cloudapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"ariga.io/atlas/sql/migrate"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

// defaultURL for Atlas Cloud.
const defaultURL = "https://api.atlasgo.cloud/query"

// Client is a client for the Atlas Cloud API.
type Client struct {
	client   *http.Client
	endpoint string
}

// New creates a new Client for the Atlas Cloud API.
func New(endpoint, token string) *Client {
	if endpoint == "" {
		endpoint = defaultURL
	}
	return &Client{
		endpoint: endpoint,
		client: &http.Client{
			Transport: &roundTripper{
				token: token,
			},
			Timeout: time.Second * 30,
		},
	}
}

// DirInput is the input type for retrieving a single directory.
type DirInput struct {
	Name string `json:"name"`
	Tag  string `json:"tag,omitempty"`
}

// Dir retrieves a directory from the Atlas Cloud API.
func (c *Client) Dir(ctx context.Context, input DirInput) (migrate.Dir, error) {
	var (
		payload struct {
			Dir struct {
				Content []byte `json:"content"`
			} `json:"dir"`
		}
		query = `
		query getDir($input: DirInput!) {
		   dir(input: $input) {
		     content
		   }
		}`
		vars = struct {
			Input DirInput `json:"input"`
		}{
			Input: input,
		}
	)
	if err := c.post(ctx, query, vars, &payload); err != nil {
		return nil, err
	}
	return migrate.UnarchiveDir(payload.Dir.Content)
}

type (
	// ReportMigrationInput represents an input type for a reporting migration deployments.
	ReportMigrationInput struct {
		ProjectName    string              `json:"projectName"`
		EnvName        string              `json:"envName"`
		DirName        string              `json:"dirName"`
		AtlasVersion   string              `json:"atlasVersion"`
		Target         DeployedTargetInput `json:"target"`
		StartTime      time.Time           `json:"startTime"`
		EndTime        time.Time           `json:"endTime"`
		FromVersion    string              `json:"fromVersion"`
		ToVersion      string              `json:"toVersion"`
		CurrentVersion string              `json:"currentVersion"`
		Error          *string             `json:"error,omitempty"`
		Files          []DeployedFileInput `json:"files"`
	}

	// DeployedTargetInput represents the input type for a deployed target.
	DeployedTargetInput struct {
		ID     string `json:"id"`
		Schema string `json:"schema"`
		URL    string `json:"url"` // URL string without userinfo.
	}

	// DeployedFileInput represents the input type for a deployed file.
	DeployedFileInput struct {
		Name      string          `json:"name"`
		Content   string          `json:"content"`
		StartTime time.Time       `json:"startTime"`
		EndTime   time.Time       `json:"endTime"`
		Skipped   int             `json:"skipped"`
		Applied   int             `json:"applied"`
		Error     *StmtErrorInput `json:"error,omitempty"`
	}

	// StmtErrorInput represents the input type for a statement error.
	StmtErrorInput struct {
		Stmt string `json:"stmt"`
		Text string `json:"text"`
	}
)

// ReportMigration reports a migration deployment to the Atlas Cloud API.
func (c *Client) ReportMigration(ctx context.Context, input ReportMigrationInput) error {
	var (
		payload struct {
			ReportMigration struct {
				Success bool `json:"success"`
			} `json:"reportMigration"`
		}
		query = `
		mutation ReportMigration($input: ReportMigrationInput!) {
		   reportMigration(input: $input) {
		     success
		   }
		}`
		vars = struct {
			Input ReportMigrationInput `json:"input"`
		}{
			Input: input,
		}
	)
	return c.post(ctx, query, vars, &payload)
}

func (c *Client) post(ctx context.Context, query string, vars, data any) error {
	body, err := json.Marshal(struct {
		Query     string `json:"query"`
		Variables any    `json:"variables,omitempty"`
	}{
		Query:     query,
		Variables: vars,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer req.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}
	var scan = struct {
		Data   any           `json:"data"`
		Errors gqlerror.List `json:"errors,omitempty"`
	}{
		Data: data,
	}
	if err := json.NewDecoder(res.Body).Decode(&scan); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(scan.Errors) > 0 {
		return scan.Errors
	}
	return nil
}

// roundTripper is a http.RoundTripper that adds the Authorization header.
type roundTripper struct {
	token string
}

// RoundTrip implements http.RoundTripper.
func (r *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("User-Agent", "atlas-cli")
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultTransport.RoundTrip(req)
}
