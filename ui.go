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
	fmt.Println("  [!] If you are fullscreened in Terminal, a prompt may appear behind it.")
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
	fmt.Println("  [!] If you are fullscreened in Terminal, a selection dialog may appear behind it.")
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

// uiChooseOptionalCheckboxes renders a checkbox-style selector using JXA/Cocoa.
// Falls back to choose-from-list if JXA dialog fails.
func uiChooseOptionalCheckboxes(title, message string, options, defaultOptions []string) ([]string, error) {
	if len(options) == 0 {
		return nil, nil
	}
	fmt.Println("  [!] If you are fullscreened in Terminal, a checkbox dialog may appear behind it.")

	defaultSet := map[string]bool{}
	for _, d := range defaultOptions {
		defaultSet[d] = true
	}

	optionLiterals := make([]string, 0, len(options))
	defaultLiterals := make([]string, 0, len(options))
	for _, opt := range options {
		optionLiterals = append(optionLiterals, strconv.Quote(opt))
		if defaultSet[opt] {
			defaultLiterals = append(defaultLiterals, "true")
		} else {
			defaultLiterals = append(defaultLiterals, "false")
		}
	}

	script := fmt.Sprintf(`
ObjC.import('Cocoa');

const title = %s;
const message = %s;
const options = [%s];
const defaults = [%s];

$.NSApplication.sharedApplication;
const alert = $.NSAlert.alloc.init;
alert.messageText = $(title);
alert.informativeText = $(message);
alert.addButtonWithTitle($('Continue'));
alert.addButtonWithTitle($('Cancel'));

const rowHeight = 26;
const width = 360;
const height = Math.max(40, options.length * rowHeight + 8);
const view = $.NSView.alloc.initWithFrame($.NSMakeRect(0, 0, width, height));
const checks = [];

for (let i = 0; i < options.length; i++) {
  const y = height - ((i + 1) * rowHeight);
  const cb = $.NSButton.alloc.initWithFrame($.NSMakeRect(0, y, width, 22));
  cb.setButtonType($.NSSwitchButton);
  cb.title = $(options[i]);
  cb.state = defaults[i] ? $.NSControlStateValueOn : $.NSControlStateValueOff;
  view.addSubview(cb);
  checks.push(cb);
}

alert.accessoryView = view;
const response = alert.runModal;
if (response !== $.NSAlertFirstButtonReturn) {
  '';
} else {
  const selected = [];
  for (let i = 0; i < checks.length; i++) {
    if (checks[i].state === $.NSControlStateValueOn) {
      selected.push(options[i]);
    }
  }
  selected.join('|||');
}
`, strconv.Quote(title), strconv.Quote(message), strings.Join(optionLiterals, ", "), strings.Join(defaultLiterals, ", "))

	out, err := osascriptOutputLang("JavaScript", script)
	if err != nil {
		logWarn("tool_select", "checkbox dialog failed, falling back to list dialog", map[string]string{"error": err.Error()})
		return uiChooseFromList(title, message, options, defaultOptions)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	parts := strings.Split(out, "|||")
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
	return exec.Command("osascript", "-e", `tell application "Terminal" to activate`, "-e", script).Run()
}

func osascriptOutput(script string) (string, error) {
	out, err := exec.Command("osascript", "-e", `tell application "Terminal" to activate`, "-e", script).Output()
	return strings.TrimSpace(string(out)), err
}

func osascriptOutputLang(lang, script string) (string, error) {
	if strings.EqualFold(lang, "JavaScript") {
		script = `Application("Terminal").activate();` + "\n" + script
	}
	out, err := exec.Command("osascript", "-l", lang, "-e", script).Output()
	return strings.TrimSpace(string(out)), err
}
