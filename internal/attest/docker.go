package attest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

func eventColor(event string) func(...any) string {
	switch {
	case event == "KILL" || event == "STOP":
		return red
	case event == "START":
		return green
	default:
		return yellow
	}
}

type logEntry struct {
	T     int64  `json:"t"`
	Node  string `json:"node"`
	Msg   string `json:"msg,omitempty"`
	Event string `json:"event,omitempty"`
}

const (
	dockerNetwork = "clstr-net"
	dockerSubnet  = "10.0.42.0/24"

	containerPort = 8080

	logDir = "/tmp/clstr"
)

// checkDockerDaemon verifies that docker is installed and the daemon is reachable.
func checkDockerDaemon(ctx context.Context) error {
	_, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found in $PATH")
	}

	out, err := exec.CommandContext(ctx, "docker", "info").CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker daemon not running: %w\n%s", err, out)
	}

	return nil
}

// buildDockerImage builds the challenge Docker image from the Dockerfile in the
// current directory.
func buildDockerImage(ctx context.Context, challengeKey string) error {
	var buf bytes.Buffer

	cmd := exec.CommandContext(ctx, "docker", "build", "-t", "clstr-"+challengeKey, "--label", "io.clstr=true", ".")
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("Image build failed:\n%s", buf.String())
	}

	return nil
}

// createDockerNetwork creates the clstr Docker network if it does not already exist.
func createDockerNetwork(ctx context.Context) error {
	out, err := exec.CommandContext(
		ctx, "docker", "network", "create", "--subnet", dockerSubnet, dockerNetwork,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Create network: %w\n%s", err, out)
	}

	return nil
}

// resetDockerEnv cleans up containers, volumes, and log files for the given challenge.
func resetDockerEnv(ctx context.Context, challengeKey string, containerNames []string) {
	for _, name := range containerNames {
		exec.CommandContext(ctx, "docker", "rm", "-f", "clstr-"+challengeKey+"-"+name).Run()
		exec.CommandContext(ctx, "docker", "volume", "rm", "clstr-"+challengeKey+"-"+name+"-data").Run()
	}

	matches, err := filepath.Glob(filepath.Join(logDir, "clstr-"+challengeKey+"-*.log"))
	if err == nil {
		for _, f := range matches {
			os.Remove(f)
		}
	}

	exec.CommandContext(ctx, "docker", "network", "rm", dockerNetwork).Run()
	exec.CommandContext(ctx, "docker", "image", "prune", "-f", "--filter", "label=io.clstr=true").Run()
}

// NodeLogPath returns the path to the log file for the given challenge and node.
func NodeLogPath(challengeKey, nodeName string) string {
	return filepath.Join(logDir, "clstr-"+challengeKey+"-"+nodeName+".log")
}

// containerNode is a cluster node running as a Docker container.
type containerNode struct {
	name        string
	logicalName string
	imageTag    string
	ip          string
	mappedPort  int
	peers       []string
	alive       atomic.Bool
}

func (n *containerNode) ContainerIP() string {
	return n.ip
}

func (n *containerNode) MappedPort() int {
	return n.mappedPort
}

func (n *containerNode) IsAlive() bool {
	return n.alive.Load()
}

func (n *containerNode) logPath() string {
	return filepath.Join(logDir, n.name+".log")
}

func (n *containerNode) followLogs(f *os.File, tail string) {
	go func() {
		defer f.Close()

		pr, pw := io.Pipe()
		args := []string{"logs", "--follow", "--timestamps"}
		if tail != "" {
			args = append(args, "--tail", tail)
		}
		args = append(args, n.name)
		cmd := exec.Command("docker", args...)
		cmd.Stdout = pw
		cmd.Stderr = pw

		go func() {
			cmd.Run()
			pw.Close()
		}()

		enc := json.NewEncoder(f)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := scanner.Text()
			t, msg := parseDockerTimestamp(line)
			if strings.TrimSpace(msg) == "" {
				continue
			}

			enc.Encode(logEntry{
				T:    t.UnixNano(),
				Node: n.logicalName,
				Msg:  msg,
			})
		}
	}()
}

func (n *containerNode) Logs() string {
	entries, _ := readLogEntries(n.logPath())
	if len(entries) == 0 {
		return ""
	}

	t0 := entries[0].T
	var sb strings.Builder
	for _, e := range entries {
		elapsed := fmt.Sprintf("+%.3fs", float64(e.T-t0)/1e9)
		if e.Event != "" {
			fmt.Fprintf(&sb, "%-10s  [%s]\n", elapsed, e.Event)
		} else {
			fmt.Fprintf(&sb, "%-10s  %s\n", elapsed, e.Msg)
		}
	}

	return sb.String()
}

func parseDockerTimestamp(line string) (time.Time, string) {
	i := strings.IndexByte(line, ' ')
	if i < 0 {
		return time.Now(), line
	}

	t, err := time.Parse(time.RFC3339Nano, line[:i])
	if err != nil {
		return time.Now(), line
	}

	return t, line[i+1:]
}

func readLogEntries(path string) ([]logEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var entries []logEntry
	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		var e logEntry
		if json.Unmarshal(scanner.Bytes(), &e) == nil {
			entries = append(entries, e)
		}
	}

	return entries, nil
}

// NodesWithLogs returns logical node names that have log files for the given challenge.
func NodesWithLogs(challengeKey string) ([]string, error) {
	pattern := filepath.Join(logDir, "clstr-"+challengeKey+"-*.log")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	prefix := "clstr-" + challengeKey + "-"
	var names []string
	for _, m := range matches {
		base := strings.TrimSuffix(filepath.Base(m), ".log")
		names = append(names, strings.TrimPrefix(base, prefix))
	}

	sort.Strings(names)
	return names, nil
}

// RenderLogs reads log files for the given nodes and prints them interleaved by timestamp.
func RenderLogs(challengeKey string, nodeNames []string) error {
	var entries []logEntry
	for _, name := range nodeNames {
		es, err := readLogEntries(NodeLogPath(challengeKey, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return fmt.Errorf("read logs for %s: %w", name, err)
		}

		entries = append(entries, es...)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].T < entries[j].T
	})

	if len(entries) == 0 {
		return nil
	}

	t0 := entries[0].T
	maxWidth := 0
	for _, e := range entries {
		if len(e.Node) > maxWidth {
			maxWidth = len(e.Node)
		}
	}

	for _, e := range entries {
		elapsed := fmt.Sprintf("%-10s", fmt.Sprintf("+%.3fs", float64(e.T-t0)/1e9))
		node := fmt.Sprintf("[%-*s]", maxWidth, e.Node)
		if e.Event != "" {
			colorFn := eventColor(e.Event)
			fmt.Printf("%s  %s  %s\n", colorFn(node), colorFn(elapsed), colorFn("["+e.Event+"]"))
		} else {
			fmt.Printf("%s  %s  %s\n", bold(node), elapsed, e.Msg)
		}
	}

	return nil
}

func (n *containerNode) Annotate(msg string) {
	os.MkdirAll(logDir, 0755)
	f, err := os.OpenFile(n.logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	json.NewEncoder(f).Encode(logEntry{
		T:     time.Now().UnixNano(),
		Node:  n.logicalName,
		Event: msg,
	})
}

func (n *containerNode) Start(ctx context.Context) error {
	exec.CommandContext(ctx, "docker", "rm", "-f", n.name).Run()

	port, err := freePort()
	if err != nil {
		return fmt.Errorf("assign port for %q: %w", n.name, err)
	}
	n.mappedPort = port

	args := []string{
		"run", "-d",
		"--name", n.name,
		"--network", dockerNetwork,
		"--ip", n.ip,
		"--cap-add", "NET_ADMIN",
		"-p", fmt.Sprintf("%d:%d", n.mappedPort, containerPort),
		"-v", fmt.Sprintf("%s:/app/data", n.name+"-data"),
		"-e", "DATA_DIR=/app/data",
		"-e", fmt.Sprintf("ADDR=%s:%d", n.ip, containerPort),
		"-e", fmt.Sprintf("PEERS=%s", strings.Join(n.peers, ",")),
		n.imageTag,
	}

	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run: %w\n%s", err, out)
	}

	n.alive.Store(true)

	err = os.MkdirAll(logDir, 0755)
	if err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	f, err := os.OpenFile(n.logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	n.followLogs(f, "")
	return nil
}

func (n *containerNode) Stop(ctx context.Context, timeout time.Duration) error {
	out, err := exec.CommandContext(
		ctx, "docker", "stop", "--timeout", fmt.Sprintf("%d", int(timeout.Seconds())), n.name,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop: %w\n%s", err, out)
	}

	n.alive.Store(false)

	return nil
}

func (n *containerNode) Kill(ctx context.Context) error {
	out, err := exec.CommandContext(
		ctx, "docker", "kill", "--signal", "SIGKILL", n.name,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker kill: %w\n%s", err, out)
	}

	n.alive.Store(false)

	return nil
}

func (n *containerNode) Restart(ctx context.Context, signal syscall.Signal, timeout time.Duration) error {
	sig := "SIGTERM"
	if signal == syscall.SIGKILL {
		sig = "SIGKILL"
	}

	out, err := exec.CommandContext(ctx, "docker", "restart",
		"--signal", sig,
		"--timeout", fmt.Sprintf("%d", int(timeout.Seconds())),
		n.name,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker restart: %w\n%s", err, out)
	}

	n.alive.Store(true)

	f, err := os.OpenFile(n.logPath(), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file after restart: %w", err)
	}
	n.followLogs(f, "0")

	return nil
}

func (n *containerNode) Exec(ctx context.Context, args ...string) error {
	cmdArgs := append([]string{"exec", n.name}, args...)
	out, err := exec.CommandContext(ctx, "docker", cmdArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker exec: %w\n%s", err, out)
	}

	return nil
}

func waitUntilNodeReady(ctx context.Context, logicalName, containerName string, node clusterNode, timeout, pollInterval time.Duration) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", node.MappedPort())

	succeeded := eventually(ctx, func() bool {
		resp, err := http.Get(url)
		if err != nil {
			return false
		}

		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, timeout, pollInterval)

	if !succeeded {
		select {
		case <-ctx.Done():
			return nil
		default:
			msg := fmt.Sprintf(
				"node %q did not become ready at %s within %s",
				logicalName, url, timeout,
			)
			logs, _ := exec.CommandContext(context.Background(), "docker", "logs", containerName).CombinedOutput()

			return fmt.Errorf("%s\n\ndocker logs %s:\n%s", msg, containerName, string(logs))
		}
	}

	return nil
}

func execOnNode(ctx context.Context, node clusterNode, args ...string) {
	err := node.Exec(ctx, args...)
	if err != nil {
		panic(fmt.Sprintf("exec failed: %v", err))
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}

	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port, nil
}
