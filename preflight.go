package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var cachedSSHPublicKey string

// preflightRun executes all preflight checks. Returns the Oracle GUID entered by the user.
func preflightRun() (string, error) {
	logSetPhase("preflight")

	if err := preflightNetCheck(); err != nil {
		return "", err
	}
	if err := preflightSSHKeyEnsure(); err != nil {
		return "", err
	}
	return preflightIdentity()
}

func preflightNetCheck() error {
	logInfo("net_check", "checking public internet reachability", nil)
	if err := checkPublicInternet(); err != nil {
		return fmt.Errorf("public internet unreachable — ensure VPN is OFF before running Phase 1")
	}
	logInfo("net_check", "public internet reachable", nil)
	return nil
}

func preflightSSHKeyEnsure() error {
	home := os.Getenv("HOME")
	candidates := []string{
		home + "/.ssh/id_ed25519.pub",
		home + "/.ssh/id_rsa.pub",
	}

	var pubKeyPath string
	for _, p := range candidates {
		if pathExists(p) {
			pubKeyPath = p
			break
		}
	}

	if pubKeyPath == "" {
		if dryRun {
			logInfo("ssh_key", "dry-run mode: no SSH key found; would generate ed25519 key", nil)
			cachedSSHPublicKey = "<dry-run: ssh public key would be generated here>"
			return nil
		}
		logInfo("ssh_key", "no SSH key found, generating ed25519 key", nil)
		email, err := uiPrompt("SSH Key Setup", "Enter your Oracle email for SSH key generation:", "firstname.lastname@oracle.com")
		if err != nil {
			return fmt.Errorf("SSH key generation cancelled: %w", err)
		}
		keyPath := home + "/.ssh/id_ed25519"
		if err := exec.Command("ssh-keygen", "-t", "ed25519", "-C", email, "-f", keyPath, "-N", "").Run(); err != nil {
			return fmt.Errorf("ssh-keygen failed: %w", err)
		}
		pubKeyPath = keyPath + ".pub"
		logInfo("ssh_key", "SSH key generated", map[string]string{"path": keyPath})
	}

	pubKey, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("could not read public key at %s: %w", pubKeyPath, err)
	}

	cachedSSHPublicKey = strings.TrimSpace(string(pubKey))
	logInfo("ssh_key", "SSH public key ready; Bitbucket add step deferred until VPN is connected", nil)
	return nil
}

func postVPNSSHKeyStep() error {
	if cachedSSHPublicKey == "" {
		logWarn("ssh_key", "no cached SSH key found from preflight; skipping Bitbucket prompt", nil)
		return nil
	}
	if dryRun {
		logInfo("ssh_key", "dry-run mode: would prompt user to add SSH key in Bitbucket now", nil)
		return nil
	}
	logInfo("ssh_key", "presenting SSH public key for Bitbucket add", nil)
	return uiShowSSHKey(cachedSSHPublicKey)
}

func preflightIdentity() (string, error) {
	guid, err := uiPrompt("Oracle Identity", "Enter your Oracle GUID (e.g. jsmith):", "")
	if err != nil || strings.TrimSpace(guid) == "" {
		return "", fmt.Errorf("oracle GUID is required")
	}
	guid = strings.TrimSpace(guid)
	logInfo("identity", "oracle GUID entered", map[string]string{"guid": guid})
	logInfo("identity", "hostname/realname mutation disabled; GUID will only be used for tool config", nil)
	return guid, nil
}

func checkPublicInternet() error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Head("https://github.com")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}
	return nil
}

func configureNoSleepAliases() error {
	sleep := firstPmsetValue("sleep", "10")
	hibernate := firstPmsetValue("hibernatemode", "3")
	disableSleep := firstPmsetValue("disablesleep", "0")

	fmt.Printf("\n── Sleep Settings ─────────────────────────────────────────────\n")
	fmt.Printf("  current pmset values: sleep=%s hibernatemode=%s disablesleep=%s\n", sleep, hibernate, disableSleep)

	block := fmt.Sprintf(`# BEGIN: Sleep Controls
alias ns='sudo pmset -a sleep 0; sudo pmset -a hibernatemode 0; sudo pmset -a disablesleep 1;'
alias ys='sudo pmset -a sleep %s; sudo pmset -a hibernatemode %s; sudo pmset -a disablesleep %s;'
# END: Sleep Controls`, sleep, hibernate, disableSleep)
	return appendToZshrc("# BEGIN: Sleep Controls", block)
}

func firstPmsetValue(key, fallback string) string {
	out := cmdOutput("pmset", "-g", "custom")
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s+([^\s]+)`)
	matches := re.FindStringSubmatch(out)
	if len(matches) < 2 || strings.TrimSpace(matches[1]) == "" {
		return fallback
	}
	return strings.TrimSpace(matches[1])
}
