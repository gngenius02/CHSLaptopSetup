package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func uiAlert(title, message string) error {
	script := fmt.Sprintf(
		`display alert %q message %q buttons {"OK"} default button "OK"`,
		title, message,
	)
	return osascript(script)
}

func uiConfirm(title, message string) (bool, error) {
	script := fmt.Sprintf(
		`display dialog %q with title %q buttons {"No", "Yes"} default button "Yes"`,
		message, title,
	)
	out, err := osascriptOutput(script)
	if err != nil {
		return false, nil // treat cancel as No
	}
	return strings.Contains(out, "Yes"), nil
}

func uiPrompt(title, message, defaultValue string) (string, error) {
	script := fmt.Sprintf(
		`display dialog %q with title %q default answer %q`,
		message, title, defaultValue,
	)
	out, err := osascriptOutput(script)
	if err != nil {
		return "", fmt.Errorf("dialog cancelled: %w", err)
	}
	for _, part := range strings.Split(out, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "text returned:") {
			return strings.TrimPrefix(part, "text returned:"), nil
		}
	}
	return "", fmt.Errorf("could not parse dialog output: %q", out)
}

func uiChooseFromList(title, message string, options, defaultOptions []string) ([]string, error) {
	if len(options) == 0 {
		return nil, nil
	}
	script := fmt.Sprintf(`
set itemList to {%s}
set defaultList to {%s}
set chosen to choose from list itemList with title %q with prompt %q default items defaultList with multiple selections allowed and empty selection allowed
if chosen is false then
	return ""
end if
return chosen as string`, appleScriptList(options), appleScriptList(defaultOptions), title, message)

	out, err := osascriptOutput(script)
	if err != nil {
		return nil, fmt.Errorf("tool selection dialog failed: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}

	parts := strings.Split(out, ",")
	selected := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			selected = append(selected, p)
		}
	}
	return selected, nil
}

func appleScriptList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, strconv.Quote(item))
	}
	return strings.Join(quoted, ", ")
}

// uiShowSSHKey shows the public key and blocks until user confirms it's been added to Bitbucket.
func uiShowSSHKey(pubKey string) error {
	script := fmt.Sprintf(
		`display dialog "Your SSH public key — add this to Bitbucket:\n\n%s" `+
			`with title "SSH Public Key" buttons {"Open Bitbucket", "Already added"} `+
			`default button "Already added"`,
		pubKey,
	)
	out, err := osascriptOutput(script)
	if err != nil {
		return fmt.Errorf("dialog cancelled: %w", err)
	}
	if strings.Contains(out, "Open Bitbucket") {
		_ = exec.Command("open", "https://bitbucket.oci.oraclecorp.com/plugins/servlet/ssh/account/keys").Run()
		ok, _ := uiConfirm("SSH Key", "Have you added the SSH key to Bitbucket?")
		if !ok {
			return fmt.Errorf("SSH key not added to Bitbucket — cannot continue")
		}
	}
	return nil
}

func uiVPNPrompt() {
	_ = uiAlert("Connect to VPN", "Phase 1 complete.\n\nPlease connect to Cisco VPN now, then click OK to continue.")
}

func osascript(script string) error {
	return exec.Command("osascript", "-e", script).Run()
}

func osascriptOutput(script string) (string, error) {
	out, err := exec.Command("osascript", "-e", script).Output()
	return strings.TrimSpace(string(out)), err
}
