package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// baseEnv provides absolute paths for all tools without requiring .zshrc to be sourced.
var baseEnv = func() []string {
	home := os.Getenv("HOME")
	return []string{
		"HOME=" + home,
		"USER=" + os.Getenv("USER"),
		"LOGNAME=" + os.Getenv("LOGNAME"),
		"PATH=/opt/homebrew/bin:/opt/homebrew/sbin:/opt/local/bin:/opt/local/sbin:" +
			home + "/.pyenv/bin:" +
			home + "/.pyenv/shims:" +
			"/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
		"PYENV_ROOT=" + home + "/.pyenv",
		"HOMEBREW_PREFIX=/opt/homebrew",
		"HOMEBREW_CELLAR=/opt/homebrew/Cellar",
		"HOMEBREW_REPOSITORY=/opt/homebrew",
		"TERM=xterm-256color",
		"LANG=en_US.UTF-8",
	}
}()

// pyenvEnv returns an env slice with a specific pyenv virtualenv activated.
func pyenvEnv(venv string) []string {
	home := os.Getenv("HOME")
	pyenvRoot := home + "/.pyenv"
	env := make([]string, 0, len(baseEnv)+3)
	for _, e := range baseEnv {
		if !strings.HasPrefix(e, "PATH=") && !strings.HasPrefix(e, "VIRTUAL_ENV=") && !strings.HasPrefix(e, "PYENV_VERSION=") {
			env = append(env, e)
		}
	}
	return append(env,
		"PYENV_VERSION="+venv,
		"VIRTUAL_ENV="+pyenvRoot+"/versions/"+venv,
		"PATH="+pyenvRoot+"/versions/"+venv+"/bin:"+
			"/opt/homebrew/bin:/opt/homebrew/sbin:/opt/local/bin:/opt/local/sbin:"+
			pyenvRoot+"/bin:"+pyenvRoot+"/shims:"+
			"/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
	)
}

// runCmd executes a command, returns combined output and error.
func runCmd(step string, env []string, name string, args ...string) (string, error) {
	if dryRun {
		logInfo(step, fmt.Sprintf("DRY-RUN: would exec: %s %s", name, strings.Join(args, " ")), nil)
		return "", nil
	}
	if env == nil {
		env = baseEnv
	}
	cmd := exec.Command(name, args...)
	cmd.Env = env

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	logInfo(step, fmt.Sprintf("exec: %s %s", name, strings.Join(args, " ")), nil)

	err := cmd.Run()
	out := strings.TrimSpace(buf.String())

	fields := map[string]string{"cmd": name + " " + strings.Join(args, " ")}
	if out != "" {
		fields["output"] = truncate(out, 500)
	}
	if err != nil {
		fields["error"] = err.Error()
		logError(step, "command failed", fields)
		return out, fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, out)
	}
	logInfo(step, "command ok", fields)
	return out, nil
}

// sudoCmd runs a command under sudo. Assumes sudo is already cached.
func sudoCmd(step string, env []string, name string, args ...string) (string, error) {
	allArgs := append([]string{name}, args...)
	return runCmd(step, env, "sudo", allArgs...)
}

// runInteractive attaches stdio directly (for xcode-select --install etc.)
func runInteractive(step, name string, args ...string) error {
	if dryRun {
		logInfo(step, fmt.Sprintf("DRY-RUN: would exec interactive: %s %s", name, strings.Join(args, " ")), nil)
		return nil
	}
	cmd := exec.Command(name, args...)
	cmd.Env = baseEnv
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	logInfo(step, fmt.Sprintf("exec interactive: %s %s", name, strings.Join(args, " ")), nil)
	return cmd.Run()
}

// cmdOutput runs a command and returns stdout only, ignoring errors.
func cmdOutput(name string, args ...string) string {
	out, _ := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out))
}

// pathExists returns true if the path exists on disk.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// fileContains returns true if the file contains substr.
func fileContains(path, substr string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), substr)
}

// appendToZshrc appends block to ~/.zshrc only if guardString is not already present.
func appendToZshrc(guardString, block string) error {
	zshrc := os.Getenv("HOME") + "/.zshrc"
	if fileContains(zshrc, guardString) {
		logInfo("zshrc", "block already present, skipping: "+guardString, nil)
		return nil
	}
	if dryRun {
		logInfo("zshrc", "DRY-RUN: would append block: "+guardString, nil)
		return nil
	}
	f, err := os.OpenFile(zshrc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n" + block + "\n")
	logInfo("zshrc", "appended block: "+guardString, nil)
	return err
}

// startSudoKeepalive caches sudo credentials then refreshes every 60s via a goroutine.
func startSudoKeepalive(ctx context.Context) error {
	cmd := exec.Command("sudo", "-v")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo auth failed: %w", err)
	}
	logInfo("sudo", "sudo credentials cached", nil)
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := exec.Command("sudo", "-n", "-v").Run(); err != nil {
					logWarn("sudo", "sudo keepalive tick failed", map[string]string{"error": err.Error()})
				}
			}
		}
	}()
	return nil
}

// gitCloneOrPull clones remote into dir, or pulls if the repo already exists.
func gitCloneOrPull(step, remote, dir string) error {
	if pathExists(dir + "/.git") {
		logInfo(step, "repo exists, pulling: "+dir, nil)
		_, err := runCmd(step, nil, "git", "-C", dir, "pull")
		return err
	}
	logInfo(step, fmt.Sprintf("cloning %s → %s", remote, dir), nil)
	_, err := runCmd(step, nil, "git", "clone", remote, dir)
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
