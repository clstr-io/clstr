package attest

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

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
		os.Remove(NodeLogPath(challengeKey, name))
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
	name       string
	imageTag   string
	ip         string
	mappedPort int
	peers      []string
	alive      atomic.Bool
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

	go func() {
		defer f.Close()

		cmd := exec.Command("docker", "logs", "--follow", n.name)
		cmd.Stdout = f
		cmd.Stderr = f
		cmd.Run()
	}()

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

func (n *containerNode) Logs() string {
	b, err := os.ReadFile(n.logPath())
	if err != nil {
		return ""
	}

	return string(b)
}

func (n *containerNode) Annotate(msg string) {
	f, err := os.OpenFile(n.logPath(), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	// Uppercase the label but preserve node names that follow ": ".
	i := strings.Index(msg, ": ")
	if i >= 0 {
		msg = strings.ToUpper(msg[:i]) + ": " + msg[i+2:]
	} else {
		msg = strings.ToUpper(msg)
	}

	fmt.Fprintf(f, "\n================ %s ================\n\n", msg)
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
