// Package api implements Airplane HTTP API client.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/airplanedev/cli/pkg/logger"
	"github.com/airplanedev/cli/pkg/version"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/pkg/errors"
)

var (
	// Client tolerates minor outages and retries.
	client *http.Client
)

func init() {
	rc := retryablehttp.NewClient()
	rc.RetryMax = 5
	rc.RetryWaitMin = 50 * time.Millisecond
	rc.RetryWaitMax = 1 * time.Second
	rc.Logger = logger.HTTPLogger{} // Logs messages as debug output
	client = rc.StandardClient()
}

// Error represents an API error.
type Error struct {
	Code    int
	Message string `json:"error"`
}

// Error implementation.
func (err Error) Error() string {
	return fmt.Sprintf("api: %d - %s", err.Code, err.Message)
}

const (
	// Host is the default API host.
	Host = "api.airplane.dev"
)

// Client implements Airplane client.
//
// The token must be configured, otherwise all methods will
// return an error.
//
// TODO(amir): probably need to configure the host and token somewhere
// globally, token might be read once in the beginning and passed down
// through the context?
type Client struct {
	// Host is the API host to use.
	//
	// If empty, it uses the global `api.Host`.
	Host string

	// Token is the token to use for authentication.
	//
	// When empty the client will return an error.
	Token string

	// Alternative to token-based authn.
	APIKey string
	TeamID string
}

// AppURL returns the app URL.
func (c Client) appURL() *url.URL {
	apphost := c.host()
	apphost = strings.ReplaceAll(apphost, "api.airstage.app", "web.airstage.app")
	apphost = strings.ReplaceAll(apphost, "api", "app")
	u, _ := url.Parse("https://" + apphost)
	return u
}

// LoginURL returns a login URL that redirects to `uri`.
func (c Client) LoginURL(uri string) string {
	u := c.appURL()
	u.Path = "/cli/login"
	u.RawQuery = url.Values{"redirect": []string{uri}}.Encode()
	return u.String()
}

// LoginSuccessURL returns a URL showing a message that logging in was successful.
func (c Client) LoginSuccessURL() string {
	u := c.appURL()
	u.Path = "/cli/success"
	return u.String()
}

// RunURL returns a run URL for a run ID.
func (c Client) RunURL(id string) string {
	u := c.appURL()
	u.Path = "/runs/" + id
	return u.String()
}

// TaskURL returns a task URL for a task slug.
func (c Client) TaskURL(slug string) string {
	u := c.appURL()
	u.Path = "/t/" + slug
	return u.String()
}

// AuthInfo responds with the currently authenticated details.
func (c Client) AuthInfo(ctx context.Context) (res AuthInfoResponse, err error) {
	err = c.do(ctx, "GET", "/auth/info", nil, &res)
	return
}

// GetRegistryToken responds with the registry token.
func (c Client) GetRegistryToken(ctx context.Context) (res RegistryTokenResponse, err error) {
	err = c.do(ctx, "POST", "/registry/getToken", nil, &res)
	return
}

// CreateTask creates a task with the given request.
func (c Client) CreateTask(ctx context.Context, req CreateTaskRequest) (res CreateTaskResponse, err error) {
	err = c.do(ctx, "POST", "/tasks/create", req, &res)
	return
}

// UpdateTask updates a task with the given req.
func (c Client) UpdateTask(ctx context.Context, req UpdateTaskRequest) (res UpdateTaskResponse, err error) {
	err = c.do(ctx, "POST", "/tasks/update", req, &res)
	return
}

// ListTasks lists all tasks.
func (c Client) ListTasks(ctx context.Context) (res ListTasksResponse, err error) {
	err = c.do(ctx, "GET", "/tasks/list", nil, &res)
	if err != nil {
		return
	}
	for j, t := range res.Tasks {
		res.Tasks[j].URL = c.TaskURL(t.Slug)
	}
	return
}

// GetUniqueSlug gets a unique slug based on the given name.
func (c Client) GetUniqueSlug(ctx context.Context, name, preferredSlug string) (res GetUniqueSlugResponse, err error) {
	q := url.Values{
		"name": []string{name},
		"slug": []string{preferredSlug},
	}
	err = c.do(ctx, "GET", "/tasks/getUniqueSlug?"+q.Encode(), nil, &res)
	return
}

// ListRuns lists most recent runs.
func (c Client) ListRuns(ctx context.Context, req ListRunsRequest) (ListRunsResponse, error) {
	q := url.Values{}
	q.Set("page", strconv.FormatInt(int64(req.Page), 10))
	if req.TaskID != "" {
		q.Set("taskID", req.TaskID)
	}
	pageLimit := 100
	if req.Limit > 0 && req.Limit < 100 {
		// If a user provides a smaller limit, fetch exactly that many items.
		pageLimit = req.Limit
	}
	q.Set("limit", strconv.FormatInt(int64(pageLimit), 10))
	if !req.Since.IsZero() {
		q.Set("since", req.Since.Format(time.RFC3339))
	}
	if !req.Until.IsZero() {
		q.Set("until", req.Until.Format(time.RFC3339))
	}

	var resp ListRunsResponse
	var page ListRunsResponse
	var i int
	for {
		q["page"] = []string{strconv.FormatInt(int64(i), 10)}
		i++
		if err := c.do(ctx, "GET", "/runs/list?"+q.Encode(), nil, &page); err != nil {
			return ListRunsResponse{}, err
		}
		runs := page.Runs
		if req.Limit > 0 && len(resp.Runs)+len(runs) > req.Limit {
			// Truncate the response if we over-fetched items:
			runs = runs[:req.Limit-len(resp.Runs)]
		}
		resp.Runs = append(resp.Runs, runs...)

		// There are no more items to fetch:
		if len(page.Runs) != pageLimit {
			break
		}
		// We have reached the requested limit of items to fetch:
		if req.Limit > 0 && len(resp.Runs) == req.Limit {
			break
		}
	}

	return resp, nil
}

// RunTask runs a task.
func (c Client) RunTask(ctx context.Context, req RunTaskRequest) (res RunTaskResponse, err error) {
	err = c.do(ctx, "POST", "/tasks/execute", req, &res)
	return
}

// Watcher runs a task with the given arguments and returns a run watcher.
func (c Client) Watcher(ctx context.Context, req RunTaskRequest) (*Watcher, error) {
	resp, err := c.RunTask(ctx, req)
	if err != nil {
		return nil, err
	}
	return newWatcher(ctx, c, resp.RunID), nil
}

// GetRun returns a run by id.
func (c Client) GetRun(ctx context.Context, id string) (res GetRunResponse, err error) {
	q := url.Values{"runID": []string{id}}
	err = c.do(ctx, "GET", "/runs/get?"+q.Encode(), nil, &res)
	return
}

// GetLogs returns the logs by runID and since timestamp.
func (c Client) GetLogs(ctx context.Context, runID, prevToken string) (res GetLogsResponse, err error) {
	q := url.Values{"runID": []string{runID}}
	if prevToken != "" {
		q.Set("prev_token", prevToken)
	}
	if logger.EnableDebug {
		q.Set("level", "debug")
	}
	err = c.do(ctx, "GET", "/runs/getLogs?"+q.Encode(), nil, &res)
	return
}

// GetOutputs returns the outputs by runID.
func (c Client) GetOutputs(ctx context.Context, runID string) (res GetOutputsResponse, err error) {
	q := url.Values{"runID": []string{runID}}
	err = c.do(ctx, "GET", "/runs/getOutputs?"+q.Encode(), nil, &res)
	return
}

// GetTask returns a task by its slug.
func (c Client) GetTask(ctx context.Context, slug string) (res Task, err error) {
	q := url.Values{"slug": []string{slug}}
	err = c.do(ctx, "GET", "/tasks/get?"+q.Encode(), nil, &res)

	if err, ok := err.(Error); ok && err.Code == 404 {
		return res, &TaskMissingError{
			appURL: c.appURL().String(),
			slug:   slug,
		}
	}

	if err != nil {
		return
	}
	res.URL = c.TaskURL(res.Slug)
	return
}

// GetConfig returns a config by name and tag.
func (c Client) GetConfig(ctx context.Context, req GetConfigRequest) (res GetConfigResponse, err error) {
	err = c.do(ctx, "POST", "/configs/get", req, &res)
	return
}

// SetConfig sets a config, creating it if new and updating it if already exists.
func (c Client) SetConfig(ctx context.Context, req SetConfigRequest) (err error) {
	err = c.do(ctx, "POST", "/configs/set", req, nil)
	return
}

// GetBuild returns metadata about a hosted build.
func (c Client) GetBuild(ctx context.Context, id string) (res GetBuildResponse, err error) {
	q := url.Values{"id": []string{id}}
	err = c.do(ctx, "GET", "/builds/get?"+q.Encode(), nil, &res)
	return
}

// CreateBuild creates an Airplane build and returns metadata about it.
func (c Client) CreateBuild(ctx context.Context, req CreateBuildRequest) (res CreateBuildResponse, err error) {
	err = c.do(ctx, "POST", "/builds/create", req, &res)
	return
}

// CreateBuildUpload creates an Airplane upload and returns metadata about it.
func (c Client) CreateBuildUpload(ctx context.Context, req CreateBuildUploadRequest) (res CreateBuildUploadResponse, err error) {
	err = c.do(ctx, "POST", "/builds/createUpload", req, &res)
	return
}

// CreateAPIKey creates a new API key and returns data about it.
func (c Client) CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (res CreateAPIKeyResponse, err error) {
	err = c.do(ctx, "POST", "/apiKeys/create", req, &res)
	return
}

// ListAPIKeys lists API keys.
func (c Client) ListAPIKeys(ctx context.Context) (res ListAPIKeysResponse, err error) {
	err = c.do(ctx, "GET", "/apiKeys/list", nil, &res)
	return
}

// DeleteAPIKey deletes an API key.
func (c Client) DeleteAPIKey(ctx context.Context, req DeleteAPIKeyRequest) (err error) {
	err = c.do(ctx, "POST", "/apiKeys/delete", req, nil)
	return
}

func (c Client) GetBuildLogs(ctx context.Context, buildID string, prevToken string) (res GetBuildLogsResponse, err error) {
	q := url.Values{
		"buildID": []string{buildID},
	}
	if logger.EnableDebug {
		q.Set("level", "debug")
	}
	if prevToken != "" {
		q.Set("prev_token", prevToken)
	}
	err = c.do(ctx, "GET", "/builds/getLogs?"+q.Encode(), nil, &res)
	return
}

func (c Client) ListResources(ctx context.Context) (res ListResourcesResponse, err error) {
	err = c.do(ctx, "GET", "/resources/list", nil, &res)
	return
}

// Do sends a request with `method`, `path`, `payload` and `reply`.
func (c Client) do(ctx context.Context, method, path string, payload, reply interface{}) error {
	var url = "https://" + c.host() + "/v0" + path
	var body io.Reader

	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return errors.Wrap(err, "api: marshal payload")
		}
		body = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return errors.Wrap(err, "api: new request")
	}

	// Authn
	if c.Token == "" && c.APIKey == "" {
		return errors.New("api: authentication is missing")
	}
	if c.Token != "" {
		req.Header.Set("X-Airplane-Token", c.Token)
	} else {
		req.Header.Set("X-Airplane-API-Key", c.APIKey)
		if c.TeamID == "" {
			return errors.New("api: team ID is missing")
		}
		req.Header.Set("X-Team-ID", c.TeamID)
	}

	req.Header.Set("X-Airplane-Client", "cli")
	req.Header.Set("X-Airplane-Version", version.Get())

	resp, err := client.Do(req)

	if resp != nil {
		defer func() {
			io.Copy(ioutil.Discard, resp.Body)
			resp.Body.Close()
		}()
	}

	if err != nil {
		return errors.Wrapf(err, "api: %s %s", method, url)
	}

	if resp.StatusCode >= 400 && resp.StatusCode < 600 {
		var errt Error

		if err := json.NewDecoder(resp.Body).Decode(&errt); err == nil {
			errt.Code = resp.StatusCode
			return errt
		}

		return errors.Errorf("api: %s %s - %s", method, url, resp.Status)
	}

	if reply != nil {
		if err := json.NewDecoder(resp.Body).Decode(reply); err != nil {
			return errors.Wrapf(err, "api: %s %s - decoding json", method, url)
		}
	}

	return nil
}

// Host returns the configured endpoint.
func (c Client) host() string {
	if c.Host != "" {
		return c.Host
	}
	return Host
}
