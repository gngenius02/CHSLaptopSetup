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
var (
	originalSleepValue       string
	originalHibernateValue   string
	originalDisableSleepValue string
)

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

	currentUser := strings.TrimSpace(cmdOutput("id", "-un"))
	if currentUser == "" {
		currentUser = os.Getenv("USER")
	}
	currentHome := strings.TrimSpace(os.Getenv("HOME"))
	expectedHome := "/Users/" + guid

	if strings.EqualFold(currentUser, guid) && strings.EqualFold(currentHome, expectedHome) {
		logInfo("identity", "local username/home already match GUID", nil)
		return guid, nil
	}

	logWarn("identity", "local user/home mismatch detected", map[string]string{
		"current_user": currentUser,
		"guid":         guid,
		"current_home": currentHome,
		"expected_home": expectedHome,
	})

	choice, err := uiChoose(
		"GUID Mismatch",
		fmt.Sprintf("Your GUID is %s, but local account is %s with home %s.\n\nChoose how to proceed:", guid, currentUser, currentHome),
		[]string{"Auto-rename", "Manual steps", "Cancel"},
		"Manual steps",
	)
	if err != nil {
		return "", fmt.Errorf("identity choice cancelled: %w", err)
	}
	if choice == "Cancel" {
		return "", fmt.Errorf("cancelled by user")
	}
	if choice == "Manual steps" {
		_ = uiAlert("Manual Rename Required",
			fmt.Sprintf("Please rename your local user and home folder to GUID before rerunning:\n\nCurrent user: %s\nGUID: %s\nExpected home: %s\n\nThen run chs-onboard again.", currentUser, guid, expectedHome))
		return "", fmt.Errorf("manual rename required before proceeding")
	}

	if err := autoRenameLocalUser(guid, currentUser, currentHome); err != nil {
		return "", err
	}
	_ = uiAlert("Relogin Required", "Account rename completed. Please log out and log back in, then rerun chs-onboard.")
	os.Exit(0)
	return guid, nil
}

func autoRenameLocalUser(guid, currentUser, currentHome string) error {
	if dryRun {
		logInfo("identity", "dry-run mode: would auto-rename local user/home to GUID", map[string]string{"guid": guid})
		return nil
	}
	if strings.EqualFold(currentUser, guid) {
		return nil
	}

	newHome := "/Users/" + guid
	cmds := [][]string{
		{"sysadminctl", "-renameUser", currentUser, "-newName", guid},
		{"dscl", ".", "-create", "/Users/" + guid, "NFSHomeDirectory", newHome},
		{"mv", currentHome, newHome},
	}
	for _, c := range cmds {
		if _, err := sudoCmd("identity_rename", nil, c[0], c[1:]...); err != nil {
			return fmt.Errorf("auto-rename failed (%s): %w", strings.Join(c, " "), err)
		}
	}
	logInfo("identity", "auto-rename completed", map[string]string{"new_user": guid, "new_home": newHome})
	return nil
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
	originalSleepValue = firstPmsetValue("sleep", "10")
	originalHibernateValue = firstPmsetValue("hibernatemode", "3")
	originalDisableSleepValue = firstPmsetValue("disablesleep", "0")

	fmt.Printf("\n── Sleep Settings ─────────────────────────────────────────────\n")
	fmt.Printf("  current pmset values: sleep=%s hibernatemode=%s disablesleep=%s\n", originalSleepValue, originalHibernateValue, originalDisableSleepValue)

	block := fmt.Sprintf(`# BEGIN: Sleep Controls
alias ns='sudo pmset -a sleep 0; sudo pmset -a hibernatemode 0; sudo pmset -a disablesleep 1;'
alias ys='sudo pmset -a sleep %s; sudo pmset -a hibernatemode %s; sudo pmset -a disablesleep %s;'
# END: Sleep Controls`, originalSleepValue, originalHibernateValue, originalDisableSleepValue)
	return appendToZshrc("# BEGIN: Sleep Controls", block)
}

func applyNoSleepNow() error {
	if dryRun {
		logInfo("sleep_ns", "dry-run mode: would apply no-sleep settings now", nil)
		return nil
	}
	for _, c := range [][]string{{"pmset", "-a", "sleep", "0"}, {"pmset", "-a", "hibernatemode", "0"}, {"pmset", "-a", "disablesleep", "1"}} {
		if _, err := sudoCmd("sleep_ns", nil, c[0], c[1:]...); err != nil {
			return err
		}
	}
	logInfo("sleep_ns", "applied no-sleep settings", nil)
	return nil
}

func restoreSleepNow() error {
	if originalSleepValue == "" {
		originalSleepValue = firstPmsetValue("sleep", "10")
	}
	if originalHibernateValue == "" {
		originalHibernateValue = firstPmsetValue("hibernatemode", "3")
	}
	if originalDisableSleepValue == "" {
		originalDisableSleepValue = firstPmsetValue("disablesleep", "0")
	}
	if dryRun {
		logInfo("sleep_ys", "dry-run mode: would restore original sleep settings now", map[string]string{"sleep": originalSleepValue, "hibernatemode": originalHibernateValue, "disablesleep": originalDisableSleepValue})
		return nil
	}
	cmds := [][]string{
		{"pmset", "-a", "sleep", originalSleepValue},
		{"pmset", "-a", "hibernatemode", originalHibernateValue},
		{"pmset", "-a", "disablesleep", originalDisableSleepValue},
	}
	for _, c := range cmds {
		if _, err := sudoCmd("sleep_ys", nil, c[0], c[1:]...); err != nil {
			return err
		}
	}
	logInfo("sleep_ys", "restored original sleep settings", nil)
	return nil
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
