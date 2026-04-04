package attest

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

const (
	dockerNetwork = "clstr-net"
	dockerSubnet  = "10.0.42.0/24"
	containerPort = 8080
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
func buildDockerImage(ctx context.Context) error {
	var buf bytes.Buffer

	cmd := exec.CommandContext(ctx, "docker", "build", "-t", "clstr-challenge", "--label", "io.clstr=true", ".")
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("Image build failed:\n%s", buf.String())
	}

	return nil
}

// createDockerNetwork creates the clstr Docker network.
func createDockerNetwork(ctx context.Context) error {
	out, err := exec.CommandContext(
		ctx, "docker", "network", "create", "--subnet", dockerSubnet, dockerNetwork,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Create network: %w\n%s", err, out)
	}

	return nil
}

// resetDockerEnv cleans the docker environment.
func resetDockerEnv(ctx context.Context, containerNames []string) {
	for _, name := range containerNames {
		exec.CommandContext(ctx, "docker", "rm", "-f", "clstr-"+name).Run()
		exec.CommandContext(ctx, "docker", "volume", "rm", "clstr-"+name+"-data").Run()
	}

	exec.CommandContext(ctx, "docker", "network", "rm", dockerNetwork).Run()
	exec.CommandContext(ctx, "docker", "image", "prune", "-f", "--filter", "label=io.clstr=true").Run()
}

// containerNode is a cluster node running as a Docker container.
type containerNode struct {
	name       string
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

func (n *containerNode) Start(ctx context.Context) error {
	exec.CommandContext(ctx, "docker", "rm", "-f", n.name).Run()

	args := []string{
		"run", "-d",
		"--name", n.name,
		"--network", dockerNetwork,
		"--ip", n.ip,
		"--cap-add", "NET_ADMIN",
		"-p", fmt.Sprintf("%d:%d", n.mappedPort, containerPort),
		"-v", fmt.Sprintf("%s:/app/data", n.name+"-data"),
		"clstr-challenge",
		"--data-dir=/app/data",
		fmt.Sprintf("--peers=%s", strings.Join(n.peers, ",")),
	}

	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run: %w\n%s", err, out)
	}

	n.alive.Store(true)

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

func (n *containerNode) Exec(ctx context.Context, args ...string) error {
	cmdArgs := append([]string{"exec", n.name}, args...)
	out, err := exec.CommandContext(ctx, "docker", cmdArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker exec: %w\n%s", err, out)
	}

	return nil
}

func waitUntilNodeReady(ctx context.Context, name string, node clusterNode, timeout, pollInterval time.Duration) error {
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
				name, url, timeout,
			)
			logs, _ := exec.CommandContext(context.Background(), "docker", "logs", "clstr-"+name).CombinedOutput()

			return fmt.Errorf("%s\n\ndocker logs clstr-%s:\n%s", msg, name, string(logs))
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
