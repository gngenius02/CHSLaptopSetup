package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

type toolID string

const (
	toolXcode        toolID = "xcode"
	toolHomebrew     toolID = "homebrew"
	toolPyenv        toolID = "pyenv"
	toolPython313    toolID = "python313"
	toolPython396    toolID = "python396"
	toolPyenvVenvNCP toolID = "pyenv_venv_ncpcli"
	toolITerm2       toolID = "iterm2"
	toolAllProxy     toolID = "allproxy"
	toolSpartaPKI    toolID = "sparta_pki"
	toolHopsCLI      toolID = "hops_cli"
	toolGNOCHelper   toolID = "gnoc_helper"
	toolStencil      toolID = "stencil"
	toolSilencer     toolID = "silencer"
	toolNCPCLI       toolID = "ncpcli"
	toolJITPass      toolID = "jit_pass"
)

// depMap maps each tool to its required prerequisites.
var depMap = map[toolID][]toolID{
	toolXcode:        {},
	toolHomebrew:     {toolXcode},
	toolPyenv:        {toolHomebrew},
	toolPython313:    {toolPyenv},
	toolPython396:    {toolPyenv},
	toolPyenvVenvNCP: {toolPython396},
	toolITerm2:       {},
	toolAllProxy:     {toolPython313, toolHomebrew},
	toolSpartaPKI:    {},
	toolHopsCLI:      {toolPython313, toolSpartaPKI},
	toolGNOCHelper:   {toolPyenvVenvNCP, toolAllProxy},
	toolStencil:      {toolPyenvVenvNCP},
	toolSilencer:     {},
	toolNCPCLI:       {toolPyenvVenvNCP},
	toolJITPass:      {},
}

// validToolIDs maps string names (used in --only flag) to toolID constants.
var validToolIDs = map[string]toolID{
	"xcode":             toolXcode,
	"homebrew":          toolHomebrew,
	"pyenv":             toolPyenv,
	"python313":         toolPython313,
	"python396":         toolPython396,
	"pyenv_venv_ncpcli": toolPyenvVenvNCP,
	"iterm2":            toolITerm2,
	"allproxy":          toolAllProxy,
	"sparta_pki":        toolSpartaPKI,
	"hops_cli":          toolHopsCLI,
	"gnoc_helper":       toolGNOCHelper,
	"stencil":           toolStencil,
	"silencer":          toolSilencer,
	"ncpcli":            toolNCPCLI,
	"jit_pass":          toolJITPass,
}

// phase1Tools are installed before VPN is required.
var phase1Tools = map[toolID]bool{
	toolITerm2:       true,
	toolXcode:        true,
	toolHomebrew:     true,
	toolPyenv:        true,
	toolPython313:    true,
	toolPython396:    true,
	toolPyenvVenvNCP: true,
}

// resolveTools returns a deduplicated, dependency-ordered list for the requested tools.
func resolveTools(requested []toolID) []toolID {
	visited := map[toolID]bool{}
	var order []toolID
	var visit func(t toolID)
	visit = func(t toolID) {
		if visited[t] {
			return
		}
		visited[t] = true
		for _, dep := range depMap[t] {
			visit(dep)
		}
		order = append(order, t)
	}
	for _, t := range requested {
		visit(t)
	}
	return order
}

func allTools() []toolID {
	return resolveTools([]toolID{
		toolITerm2, toolXcode, toolHomebrew,
		toolPyenv, toolPython313, toolPython396, toolPyenvVenvNCP,
		toolAllProxy, toolSpartaPKI, toolHopsCLI,
	})
}

func gnocTools() []toolID {
	return resolveTools([]toolID{
		toolGNOCHelper, toolStencil, toolSilencer, toolNCPCLI, toolJITPass,
	})
}

// runTool installs a single tool. All installers are idempotent.
func runTool(t toolID, guid string) error {
	logInfo(string(t), fmt.Sprintf("installing: %s", t), nil)
	if dryRun {
		logInfo(string(t), fmt.Sprintf("dry-run mode: would install %s", t), nil)
		return nil
	}
	var err error
	switch t {
	case toolITerm2:
		err = installITerm2()
	case toolXcode:
		err = installXcode()
	case toolHomebrew:
		err = installHomebrew()
	case toolPyenv:
		err = installPyenv()
	case toolPython313:
		err = installPythonVersion("3.13.2")
	case toolPython396:
		err = installPythonVersion("3.9.6")
	case toolPyenvVenvNCP:
		err = installPyenvVenv("ncpcli", "3.9.6")
	case toolAllProxy:
		err = installAllProxy()
	case toolSpartaPKI:
		err = installSpartaPKI()
	case toolHopsCLI:
		err = installHopsCLI()
	case toolGNOCHelper:
		err = installGNOCHelper(guid)
	case toolStencil:
		err = installStencil()
	case toolSilencer:
		err = installSilencer()
	case toolNCPCLI:
		err = installNCPCLI()
	case toolJITPass:
		err = installJITPass()
	default:
		err = fmt.Errorf("unknown tool: %s", t)
	}
	if err != nil {
		logError(string(t), fmt.Sprintf("failed: %v", err), nil)
	} else {
		logInfo(string(t), "done", nil)
	}
	return err
}

// waitForVPN polls internal Oracle hosts until TCP connects. Blocks until VPN is up.
func waitForVPN() {
	if dryRun {
		logInfo("vpn_wait", "dry-run mode: would poll internal hosts for VPN connectivity", nil)
		return
	}
	hosts := []string{
		"artifactory.oci.oraclecorp.com:443",
		"bitbucket.oci.oraclecorp.com:7999",
	}
	logInfo("vpn_wait", "polling for VPN connectivity", nil)
	for {
		allUp := true
		for _, h := range hosts {
			conn, err := net.DialTimeout("tcp", h, 3*time.Second)
			if err != nil {
				allUp = false
				break
			}
			conn.Close()
		}
		if allUp {
			logInfo("vpn_wait", "VPN connectivity confirmed", nil)
			return
		}
		fmt.Printf("  [~] Waiting for VPN (%s)...\n", hosts[0])
		time.Sleep(5 * time.Second)
	}
}

// writeBaseZshrcBlocks appends Homebrew and pyenv init blocks if not present.
func writeBaseZshrcBlocks() error {
	blocks := []struct{ guard, block string }{
		{
			"# BEGIN: Homebrew",
			`# BEGIN: Homebrew
eval "$(/opt/homebrew/bin/brew shellenv)"
# END: Homebrew`,
		},
		{
			"# BEGIN: pyenv",
			`# BEGIN: pyenv
export PYENV_ROOT="$HOME/.pyenv"
export PATH="$PYENV_ROOT/bin:$PATH"
eval "$(pyenv init -)"
eval "$(pyenv virtualenv-init -)"
# END: pyenv`,
		},
	}
	for _, b := range blocks {
		if err := appendToZshrc(b.guard, b.block); err != nil {
			return err
		}
	}
	return nil
}

// ── Phase 1 installers ────────────────────────────────────────────────────────

func installITerm2() error {
	if pathExists("/Applications/iTerm.app") {
		logInfo("iterm2", "already installed, skipping", nil)
		return nil
	}
	tmpZip := "/tmp/iterm2.zip"
	if _, err := runCmd("iterm2", nil, "curl", "-L", "-o", tmpZip, "https://iterm2.com/downloads/stable/latest"); err != nil {
		return err
	}
	if _, err := runCmd("iterm2", nil, "unzip", "-o", tmpZip, "-d", "/Applications"); err != nil {
		return err
	}
	_ = os.Remove(tmpZip)

	home := os.Getenv("HOME")
	plistDir := home + "/Library/Preferences"
	_ = os.MkdirAll(plistDir, 0755)
	if err := os.WriteFile(plistDir+"/com.googlecode.iterm2.plist", iterm2Plist, 0644); err != nil {
		return fmt.Errorf("writing iterm2 plist: %w", err)
	}
	_, _ = runCmd("iterm2", nil, "defaults", "read", "com.googlecode.iterm2")
	return nil
}

func installXcode() error {
	if pathExists("/Library/Developer/CommandLineTools") {
		logInfo("xcode", "Xcode CLI tools already installed", nil)
		return nil
	}
	fmt.Println("  [→] Installing Xcode Command Line Tools (a system dialog will appear)...")
	_ = runInteractive("xcode", "xcode-select", "--install")
	ok, _ := uiConfirm("Xcode CLI Tools", "Click OK once the Xcode Command Line Tools installation is complete.")
	if !ok {
		return fmt.Errorf("xcode CLI tools installation not confirmed")
	}
	return nil
}

func ensureSSHPass() error {
	if pathExists("/opt/homebrew/bin/sshpass") || pathExists("/usr/local/bin/sshpass") {
		logInfo("sshpass", "sshpass already installed", nil)
		return nil
	}

	brew := "/opt/homebrew/bin/brew"
	if !pathExists(brew) {
		return fmt.Errorf("homebrew not found at %s; required for sshpass install", brew)
	}

	if _, err := runCmd("sshpass", nil, brew, "install", "hudochenkov/sshpass/sshpass"); err != nil {
		if _, fallbackErr := runCmd("sshpass", nil, brew, "install", "sshpass"); fallbackErr != nil {
			return fmt.Errorf("sshpass install failed: tap formula error: %v; fallback error: %v", err, fallbackErr)
		}
	}

	if !pathExists("/opt/homebrew/bin/sshpass") && !pathExists("/usr/local/bin/sshpass") {
		return fmt.Errorf("sshpass install completed but binary not found")
	}
	return nil
}

func installHomebrew() error {
	if pathExists("/opt/homebrew/bin/brew") {
		logInfo("homebrew", "Homebrew already installed", nil)
		return ensureBrewPackages()
	}
	fmt.Println("  [→] Installing Homebrew...")
	_, err := runCmd("homebrew", nil, "/bin/bash", "-c",
		`NONINTERACTIVE=1 curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh | bash`)
	if err != nil {
		return err
	}
	return ensureBrewPackages()
}

func ensureBrewPackages() error {
	brew := "/opt/homebrew/bin/brew"
	for _, pkg := range []string{"openssl", "yubico-piv-tool", "jq", "pyenv", "pyenv-virtualenv"} {
		if _, err := runCmd("homebrew", nil, brew, "install", pkg); err != nil {
			return fmt.Errorf("brew install %s: %w", pkg, err)
		}
	}
	if _, err := runCmd("homebrew", nil, brew, "install", "--cask", "opensc"); err != nil {
		logWarn("homebrew", "opensc cask install failed (may already be installed)", nil)
	}
	return ensureOpenSCSymlinks()
}

func ensureOpenSCSymlinks() error {
	src := "/Library/OpenSC/lib/opensc-pkcs11.so"
	dst := "/usr/local/lib/opensc-pkcs11.so"
	if !pathExists(src) {
		logWarn("opensc", "source opensc-pkcs11.so not found, skipping symlink", nil)
		return nil
	}
	if pathExists(dst) {
		logInfo("opensc", "symlink already exists", nil)
		return nil
	}
	if _, err := sudoCmd("opensc", nil, "mkdir", "-pv", "/usr/local/lib"); err != nil {
		return err
	}
	_, err := sudoCmd("opensc", nil, "ln", "-v", src, dst)
	return err
}

func installPyenv() error {
	// pyenv binary is installed via homebrew; this step just ensures .zshrc is configured.
	return appendToZshrc("# BEGIN: pyenv",
		`# BEGIN: pyenv
export PYENV_ROOT="$HOME/.pyenv"
export PATH="$PYENV_ROOT/bin:$PATH"
eval "$(pyenv init -)"
eval "$(pyenv virtualenv-init -)"
# END: pyenv`)
}

func installPythonVersion(version string) error {
	versionDir := os.Getenv("HOME") + "/.pyenv/versions/" + version
	if pathExists(versionDir) {
		logInfo("pyenv", fmt.Sprintf("Python %s already installed", version), nil)
		return nil
	}
	_, err := runCmd("pyenv", nil, os.Getenv("HOME")+"/.pyenv/bin/pyenv", "install", version)
	return err
}

func installPyenvVenv(venvName, pythonVersion string) error {
	venvDir := os.Getenv("HOME") + "/.pyenv/versions/" + venvName
	if pathExists(venvDir) {
		logInfo("pyenv", fmt.Sprintf("virtualenv %s already exists", venvName), nil)
		return nil
	}
	_, err := runCmd("pyenv_venv", nil, os.Getenv("HOME")+"/.pyenv/bin/pyenv", "virtualenv", pythonVersion, venvName)
	return err
}

func setPyenvGlobal(versions ...string) error {
	args := append([]string{"global"}, versions...)
	_, err := runCmd("pyenv_global", nil, os.Getenv("HOME")+"/.pyenv/bin/pyenv", args...)
	return err
}

// ── Phase 3 installers ────────────────────────────────────────────────────────

func installAllProxy() error {
	home := os.Getenv("HOME")
	dir := home + "/misc-tools"
	if err := gitCloneOrPull("allproxy", "ssh://git@bitbucket.oci.oraclecorp.com:7999/~rralliso/misc-tools.git", dir); err != nil {
		return err
	}
	_, err := runCmd("allproxy", nil, home+"/.pyenv/versions/3.13.2/bin/pip", "install", "-e", dir+"/allproxy")
	return err
}

func installSpartaPKI() error {
	home := os.Getenv("HOME")
	rootsDir := home + "/sparta_roots"
	if err := os.MkdirAll(rootsDir, 0750); err != nil {
		return err
	}
	tmpClone := home + "/sparta-pki"
	if err := gitCloneOrPull("sparta_pki", "ssh://git@bitbucket.oci.oraclecorp.com:7999/secinf/sparta-pki.git", tmpClone); err != nil {
		return err
	}
	entries, err := os.ReadDir(tmpClone + "/trustroots")
	if err != nil {
		return fmt.Errorf("sparta-pki trustroots dir not found: %w", err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(tmpClone + "/trustroots/" + e.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(rootsDir+"/"+e.Name(), data, 0644); err != nil {
			return err
		}
	}
	return os.RemoveAll(tmpClone)
}

func installHopsCLI() error {
	pip := os.Getenv("HOME") + "/.pyenv/versions/3.13.2/bin/pip"
	_, _ = runCmd("hops_cli", nil, pip, "cache", "purge")
	if _, err := runCmd("hops_cli", nil, pip, "install", "--upgrade", "pip"); err != nil {
		return err
	}
	if _, err := runCmd("hops_cli", nil, pip,
		"install", "--default-timeout=100", "-U",
		"--index-url", "https://artifactory.oci.oraclecorp.com/api/pypi/global-release-pypi/simple",
		"hops-cli"); err != nil {
		return err
	}
	// Known bug fix as of Feb 2026: downgrade setuptools
	_, err := runCmd("hops_cli", nil, pip, "install", "--no-cache-dir", "--force-reinstall", "setuptools==81.0.0")
	return err
}

func installGNOCHelper(guid string) error {
	home := os.Getenv("HOME")
	dir := home + "/gnoc-helper"
	if err := gitCloneOrPull("gnoc_helper", "ssh://git@bitbucket.oci.oraclecorp.com:7999/gnoc/gnoc-helper.git", dir); err != nil {
		return err
	}

	block := fmt.Sprintf(`# BEGIN: GNOC Temp Help
export OCI_USER="%s"
export AUTONET_PLANS_PATH="/path/to/plans"       # Ask your trainer
export GNOC_TEMPLATES_PATH="/path/to/templates"  # Ask your trainer

alias jit-pass="$HOME/gnoc-jit-pass/wrapper.sh"

rekey() {
    ssh-add -D
    for key in ~/.ssh/id_*; do
        [[ "$key" == *.pub ]] && continue
        if grep -q "PRIVATE KEY" "$key"; then
            ssh-add "$key" >/dev/null 2>&1 && echo "Loaded $key"
        fi
    done
    ssh-add -s /usr/local/lib/opensc-pkcs11.so 2>/dev/null
}
# END: GNOC Temp Help`, guid)
	if err := appendToZshrc("# BEGIN: GNOC Temp Help", block); err != nil {
		return err
	}

	symlinks := [][2]string{
		{dir + "/gnoc-helper.sh", "/usr/local/bin/gnoc-helper"},
		{dir + "/scripts/rack-finder.sh", "/usr/local/bin/rack-finder"},
		{dir + "/scripts/console-finder.sh", "/usr/local/bin/console-finder"},
	}
	for _, sl := range symlinks {
		if !pathExists(sl[1]) {
			if _, err := sudoCmd("gnoc_helper", nil, "ln", "-s", sl[0], sl[1]); err != nil {
				return err
			}
		}
	}
	if _, err := runCmd("gnoc_helper", nil, "/usr/local/bin/gnoc-helper", "--setup"); err != nil {
		return err
	}
	pip := home + "/.pyenv/versions/3.13.2/bin/pip"
	_, err := runCmd("gnoc_helper", nil, pip,
		"install", "rust", "cffi==1.16.0", "cryptography", "asyncssh", "pproxy", "pyyaml")
	return err
}

func installStencil() error {
	home := os.Getenv("HOME")
	repos := map[string]string{
		home + "/stencil":           "ssh://git@bitbucket.oci.oraclecorp.com:7999/nse/stencil.git",
		home + "/stencil-temp-gnoc": "ssh://git@bitbucket.oci.oraclecorp.com:7999/gnoc/stencil-temp-gnoc.git",
	}
	for dir, remote := range repos {
		if err := gitCloneOrPull("stencil", remote, dir); err != nil {
			return err
		}
	}
	pip := home + "/.pyenv/versions/ncpcli/bin/pip"
	if _, err := runCmd("stencil", pyenvEnv("ncpcli"), pip, "install", home+"/stencil/."); err != nil {
		return err
	}
	stencilBin := home + "/.pyenv/versions/ncpcli/bin/stencil"
	if !pathExists("/usr/local/bin/stencil") {
		if _, err := sudoCmd("stencil", nil, "ln", "-s", stencilBin, "/usr/local/bin/stencil"); err != nil {
			return err
		}
	}
	_, err := runCmd("stencil", pyenvEnv("ncpcli"), "/usr/local/bin/stencil", "init")
	return err
}

func installSilencer() error {
	home := os.Getenv("HOME")
	dir := home + "/silencer"
	if err := gitCloneOrPull("silencer", "ssh://git@bitbucket.oci.oraclecorp.com:7999/nse/silencer.git", dir); err != nil {
		return err
	}
	if _, err := runCmd("silencer", nil, "make", "-C", dir, "install"); err != nil {
		return err
	}
	_, err := runCmd("silencer", nil, "make", "-C", dir, "link")
	return err
}

func installNCPCLI() error {
	home := os.Getenv("HOME")
	pip := home + "/.pyenv/versions/ncpcli/bin/pip"
	env := pyenvEnv("ncpcli")

	_, _ = runCmd("ncpcli", env, pip, "cache", "purge")
	if _, err := runCmd("ncpcli", env, pip, "install", "--upgrade", "pip"); err != nil {
		return err
	}
	opensslPrefix := cmdOutput("/opt/homebrew/bin/brew", "--prefix", "openssl@1.1")
	env = append(env,
		"LDFLAGS=-L"+opensslPrefix+"/lib",
		"CFLAGS=-I"+opensslPrefix+"/include",
	)
	if _, err := runCmd("ncpcli", env, pip,
		"install",
		"--index-url", "https://artifactory.oci.oraclecorp.com/api/pypi/global-release-pypi/simple",
		"--trusted-host", "artifactory.oci.oraclecorp.com",
		"ncpcli"); err != nil {
		return err
	}
	_, err := runCmd("ncpcli", env, home+"/.pyenv/versions/ncpcli/bin/ncpcli", "--rebuild-config")
	return err
}

func installJITPass() error {
	home := os.Getenv("HOME")
	dir := home + "/gnoc-jit-pass"
	if err := gitCloneOrPull("jit_pass", "ssh://git@bitbucket.oci.oraclecorp.com:7999/gnoc/gnoc-jit-pass.git", dir); err != nil {
		return err
	}
	_, err := runCmd("jit_pass", nil, dir+"/wrapper.sh")
	return err
}
