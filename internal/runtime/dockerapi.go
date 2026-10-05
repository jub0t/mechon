package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// dockerAPIVersion is the Engine API version the agent speaks. 1.44 is Docker 25, and it is the
// oldest version Docker 29+ still accepts.
const dockerAPIVersion = "v1.44"

// dockerClient is a deliberately small Docker Engine API client: plain net/http over the unix
// socket, covering only the endpoints the agent needs.
type dockerClient struct {
	http *http.Client
}

func newDockerClient(socket string) *dockerClient {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		MaxIdleConns:    16,
		IdleConnTimeout: 90 * time.Second,
	}
	return &dockerClient{http: &http.Client{Transport: tr}}
}

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return fmt.Sprintf("docker: %d: %s", e.Status, e.Message) }

func isNotFound(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

func isNotModified(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == http.StatusNotModified
}

// do performs a request and returns the response for 2xx; otherwise it returns an *apiError.
func (c *dockerClient) do(ctx context.Context, method, path string, q url.Values, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	u := "http://docker/" + dockerAPIVersion + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &m) != nil || m.Message == "" {
		m.Message = strings.TrimSpace(string(b))
	}
	return nil, &apiError{Status: resp.StatusCode, Message: m.Message}
}

// call performs a request, decodes a JSON response into out (if non-nil) and closes the body.
func (c *dockerClient) call(ctx context.Context, method, path string, q url.Values, body, out any) error {
	resp, err := c.do(ctx, method, path, q, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ---------- Types (only the fields we use) ----------

type dockerVersion struct {
	Version       string `json:"Version"`
	APIVersion    string `json:"ApiVersion"`
	KernelVersion string `json:"KernelVersion"`
}

type dockerInfo struct {
	NCPU          int                    `json:"NCPU"`
	MemTotal      int64                  `json:"MemTotal"`
	CgroupVersion string                 `json:"CgroupVersion"`
	CgroupDriver  string                 `json:"CgroupDriver"`
	KernelVersion string                 `json:"KernelVersion"`
	Runtimes      map[string]interface{} `json:"Runtimes"`
	Name          string                 `json:"Name"`
}

type containerConfig struct {
	Hostname     string            `json:"Hostname,omitempty"`
	User         string            `json:"User,omitempty"`
	Env          []string          `json:"Env,omitempty"`
	Cmd          []string          `json:"Cmd,omitempty"`
	Entrypoint   []string          `json:"Entrypoint"`
	Image        string            `json:"Image"`
	WorkingDir   string            `json:"WorkingDir,omitempty"`
	Labels       map[string]string `json:"Labels,omitempty"`
	AttachStdout bool              `json:"AttachStdout"`
	AttachStderr bool              `json:"AttachStderr"`
	Tty          bool              `json:"Tty"`
	OpenStdin    bool              `json:"OpenStdin"`
	StopTimeout  *int              `json:"StopTimeout,omitempty"`
	HostConfig   hostConfig        `json:"HostConfig"`
}

type ulimit struct {
	Name string `json:"Name"`
	Soft int64  `json:"Soft"`
	Hard int64  `json:"Hard"`
}

type logConfig struct {
	Type   string            `json:"Type"`
	Config map[string]string `json:"Config,omitempty"`
}

type restartPolicy struct {
	Name string `json:"Name"`
}

type hostConfig struct {
	Binds          []string          `json:"Binds,omitempty"`
	NetworkMode    string            `json:"NetworkMode,omitempty"`
	RestartPolicy  restartPolicy     `json:"RestartPolicy"`
	CapDrop        []string          `json:"CapDrop,omitempty"`
	SecurityOpt    []string          `json:"SecurityOpt,omitempty"`
	ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
	Tmpfs          map[string]string `json:"Tmpfs,omitempty"`
	Memory         int64             `json:"Memory,omitempty"`
	MemorySwap     int64             `json:"MemorySwap,omitempty"`
	NanoCpus       int64             `json:"NanoCpus,omitempty"`
	PidsLimit      *int64            `json:"PidsLimit,omitempty"`
	Ulimits        []ulimit          `json:"Ulimits,omitempty"`
	LogConfig      logConfig         `json:"LogConfig"`
	Runtime        string            `json:"Runtime,omitempty"`
	Init           *bool             `json:"Init,omitempty"`
	IpcMode        string            `json:"IpcMode,omitempty"`
	DNS            []string          `json:"Dns,omitempty"`
	Privileged     bool              `json:"Privileged"`
}

type containerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
}

type containerJSON struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		Pid        int    `json:"Pid"`
		ExitCode   int    `json:"ExitCode"`
		OOMKilled  bool   `json:"OOMKilled"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

type dockerEvent struct {
	Type   string `json:"Type"`
	Action string `json:"Action"`
	Actor  struct {
		ID         string            `json:"ID"`
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
	TimeNano int64 `json:"timeNano"`
}

// ---------- Endpoints ----------

func (c *dockerClient) version(ctx context.Context) (dockerVersion, error) {
	var v dockerVersion
	err := c.call(ctx, "GET", "/version", nil, nil, &v)
	return v, err
}

func (c *dockerClient) info(ctx context.Context) (dockerInfo, error) {
	var v dockerInfo
	err := c.call(ctx, "GET", "/info", nil, nil, &v)
	return v, err
}

func (c *dockerClient) imageExists(ctx context.Context, ref string) (bool, error) {
	err := c.call(ctx, "GET", "/images/"+ref+"/json", nil, nil, nil)
	if isNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// pullImage pulls ref, calling progress with each human-meaningful status line.
func (c *dockerClient) pullImage(ctx context.Context, ref string, progress func(string)) error {
	q := url.Values{"fromImage": {ref}}
	resp, err := c.do(ctx, "POST", "/images/create", q, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	for {
		var m struct {
			Status         string          `json:"status"`
			ID             string          `json:"id"`
			ProgressDetail json.RawMessage `json:"progressDetail"`
			Error          string          `json:"error"`
		}
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if m.Error != "" {
			return fmt.Errorf("pull %s: %s", ref, m.Error)
		}
		// Skip the per-chunk "Downloading"/"Extracting" spam; keep the milestones.
		if progress != nil && (len(m.ProgressDetail) == 0 || string(m.ProgressDetail) == "{}") &&
			m.Status != "Waiting" {
			line := m.Status
			if m.ID != "" {
				line = m.ID + ": " + m.Status
			}
			progress(line)
		}
	}
}

func (c *dockerClient) createContainer(ctx context.Context, name string, cfg containerConfig) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	err := c.call(ctx, "POST", "/containers/create", url.Values{"name": {name}}, cfg, &out)
	return out.ID, err
}

func (c *dockerClient) startContainer(ctx context.Context, id string) error {
	err := c.call(ctx, "POST", "/containers/"+id+"/start", nil, nil, nil)
	if isNotModified(err) {
		return nil
	}
	return err
}

func (c *dockerClient) stopContainer(ctx context.Context, id string, timeoutSec int) error {
	err := c.call(ctx, "POST", "/containers/"+id+"/stop", url.Values{"t": {fmt.Sprint(timeoutSec)}}, nil, nil)
	if isNotModified(err) {
		return nil
	}
	return err
}

func (c *dockerClient) killContainer(ctx context.Context, id string) error {
	return c.call(ctx, "POST", "/containers/"+id+"/kill", nil, nil, nil)
}

func (c *dockerClient) removeContainer(ctx context.Context, id string) error {
	err := c.call(ctx, "DELETE", "/containers/"+id, url.Values{"force": {"1"}, "v": {"1"}}, nil, nil)
	if isNotFound(err) {
		return nil
	}
	return err
}

func (c *dockerClient) inspectContainer(ctx context.Context, id string) (containerJSON, error) {
	var v containerJSON
	err := c.call(ctx, "GET", "/containers/"+id+"/json", nil, nil, &v)
	return v, err
}

// waitContainer blocks until the container exits and returns its exit code.
func (c *dockerClient) waitContainer(ctx context.Context, id string) (int, error) {
	var out struct {
		StatusCode int `json:"StatusCode"`
		Error      *struct {
			Message string `json:"Message"`
		} `json:"Error"`
	}
	err := c.call(ctx, "POST", "/containers/"+id+"/wait", url.Values{"condition": {"not-running"}}, nil, &out)
	if err != nil {
		return -1, err
	}
	if out.Error != nil && out.Error.Message != "" {
		return out.StatusCode, errors.New(out.Error.Message)
	}
	return out.StatusCode, nil
}

func (c *dockerClient) listContainers(ctx context.Context, label string) ([]containerSummary, error) {
	f, _ := json.Marshal(map[string][]string{"label": {label}})
	var out []containerSummary
	err := c.call(ctx, "GET", "/containers/json", url.Values{"all": {"1"}, "filters": {string(f)}}, nil, &out)
	return out, err
}

// containerLogs returns the raw (multiplexed) log stream.
func (c *dockerClient) containerLogs(ctx context.Context, id string, q url.Values) (io.ReadCloser, error) {
	resp, err := c.do(ctx, "GET", "/containers/"+id+"/logs", q, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *dockerClient) events(ctx context.Context, since time.Time, filters map[string][]string) (io.ReadCloser, error) {
	f, _ := json.Marshal(filters)
	q := url.Values{"filters": {string(f)}}
	if !since.IsZero() {
		q.Set("since", fmt.Sprintf("%d.%09d", since.Unix(), since.Nanosecond()))
	}
	resp, err := c.do(ctx, "GET", "/events", q, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

type networkJSON struct {
	ID      string            `json:"Id"`
	Name    string            `json:"Name"`
	Options map[string]string `json:"Options"`
	IPAM    struct {
		Config []struct {
			Subnet  string `json:"Subnet"`
			Gateway string `json:"Gateway"`
		} `json:"Config"`
	} `json:"IPAM"`
}

func (c *dockerClient) inspectNetwork(ctx context.Context, name string) (networkJSON, error) {
	var n networkJSON
	err := c.call(ctx, "GET", "/networks/"+name, nil, nil, &n)
	return n, err
}

func (c *dockerClient) createNetwork(ctx context.Context, name string, options, labels map[string]string) error {
	body := map[string]any{
		"Name":       name,
		"Driver":     "bridge",
		"Options":    options,
		"Labels":     labels,
		"EnableIPv6": false,
	}
	return c.call(ctx, "POST", "/networks/create", nil, body, nil)
}

// ---------- Log stream demultiplexing ----------

// demuxLines reads Docker's multiplexed stream (8-byte header: stream, 0,0,0, big-endian size)
// and calls fn once per line. Lines longer than maxLine are split.
func demuxLines(r io.Reader, fn func(stream string, line []byte)) error {
	const maxLine = 16 << 10
	var hdr [8]byte
	partial := map[byte]*bytes.Buffer{1: {}, 2: {}}
	name := map[byte]string{1: "stdout", 2: "stderr"}
	br := bufio.NewReaderSize(r, 32<<10)
	flush := func(s byte) {
		if b := partial[s]; b.Len() > 0 {
			fn(name[s], b.Bytes())
			b.Reset()
		}
	}
	for {
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			flush(1)
			flush(2)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}
		s := hdr[0]
		size := binary.BigEndian.Uint32(hdr[4:])
		if s != 1 && s != 2 {
			if _, err := io.CopyN(io.Discard, br, int64(size)); err != nil {
				return nil
			}
			continue
		}
		buf := partial[s]
		for size > 0 {
			chunk := min(size, 4096)
			var tmp [4096]byte
			if _, err := io.ReadFull(br, tmp[:chunk]); err != nil {
				flush(1)
				flush(2)
				return nil
			}
			size -= chunk
			data := tmp[:chunk]
			for len(data) > 0 {
				i := bytes.IndexByte(data, '\n')
				if i < 0 {
					buf.Write(data)
					if buf.Len() >= maxLine {
						flush(s)
					}
					break
				}
				buf.Write(data[:i])
				fn(name[s], buf.Bytes())
				buf.Reset()
				data = data[i+1:]
			}
		}
	}
}
