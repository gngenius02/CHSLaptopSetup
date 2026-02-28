package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

const bastionConfigsOption = "bastion_configs (coming soon)"

var dryRun bool

func main() {
	gnocFlag := flag.Bool("gnoc", false, "include GNOC-specific tools")
	onlyFlag := flag.String("only", "", "comma-separated tool IDs to install (use --list to see options)")
	listFlag := flag.Bool("list", false, "list available tool IDs and exit")
	dryRunFlag := flag.Bool("dry-run", false, "print intended actions without making system changes")
	flag.Parse()
	dryRun = *dryRunFlag

	if *listFlag {
		printToolList()
		return
	}

	if err := logInit(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer logClose()

	printBanner()
	if dryRun {
		fmt.Println("\n[DRY-RUN] No system changes will be made.")
	}

	// Cache sudo and start keepalive goroutine
	if !dryRun {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fmt.Println("\n[sudo] chs-onboard needs administrator privileges:")
		if err := startSudoKeepalive(ctx); err != nil {
			logFatal("sudo", fmt.Sprintf("sudo auth failed: %v", err), nil)
		}
	} else {
		logInfo("sudo", "dry-run mode: skipping sudo credential caching", nil)
	}

	// Preflight
	logSetPhase("preflight")
	fmt.Println("\n── Preflight ─────────────────────────────────────────────────")
	guid, err := preflightRun()
	if err != nil {
		logFatal("preflight", err.Error(), nil)
	}

	// Write .zshrc blocks
	if err := writeBaseZshrcBlocks(); err != nil {
		logFatal("zshrc", err.Error(), nil)
	}
	if err := configureNoSleepAliases(); err != nil {
		logFatal("sleep_alias", err.Error(), nil)
	}

	// Resolve tool list
	var tools []toolID
	bastionConfigsSelected := false
	if *onlyFlag != "" {
		tools = parseOnlyFlag(*onlyFlag)
		if len(tools) == 0 {
			fmt.Fprintln(os.Stderr, "No valid tool IDs provided. Use --list to see available tools.")
			os.Exit(1)
		}
	} else {
		requestedOptional, bastionSelected, err := promptToolSelection(*gnocFlag)
		if err != nil {
			logFatal("tool_select", err.Error(), nil)
		}
		if len(requestedOptional) == 0 && !bastionSelected {
			fmt.Println("\nNo optional tools selected. Continuing with required tool set.")
		}
		requested := append(requiredTools(), requestedOptional...)
		tools = resolveTools(requested)
		bastionConfigsSelected = bastionSelected
	}

	// Split into phase 1 / phase 3
	var p1, p3 []toolID
	for _, t := range tools {
		if phase1Tools[t] {
			p1 = append(p1, t)
		} else {
			p3 = append(p3, t)
		}
	}

	// Phase 1: public internet
	logSetPhase("phase1")
	fmt.Println("\n── Phase 1: Public Internet (VPN OFF) ────────────────────────")
	if err := runPhase(p1, guid); err != nil {
		logFatal("phase1", err.Error(), nil)
	}
	if dryRun {
		logInfo("pyenv_global", "dry-run mode: would set pyenv global 3.13.2 ncpcli", nil)
	} else if err := setPyenvGlobal("3.13.2", "ncpcli"); err != nil {
		logWarn("pyenv_global", fmt.Sprintf("pyenv global set failed: %v", err), nil)
	}

	if hasTool(tools, toolGNOCHelper) {
		fmt.Println("\n  [→] Ensuring sshpass is installed (required for gnoc-helper)...")
		if dryRun {
			logInfo("sshpass", "dry-run mode: would install/verify sshpass", nil)
		} else if err := ensureSSHPass(); err != nil {
			logFatal("sshpass", err.Error(), nil)
		}
	}

	if len(p3) == 0 {
		if bastionConfigsSelected {
			logWarn("bastion_configs", "Bastion configs selected, but setup is not implemented yet; skipping", nil)
		}
		fmt.Println("\n✓ Done. No Phase 3 tools selected.")
		return
	}

	// Phase 2: VPN handover
	logSetPhase("phase2")
	fmt.Println("\n── Phase 2: Connect to VPN ───────────────────────────────────")
	uiVPNPrompt()
	waitForVPN()
	fmt.Println("  [✓] VPN confirmed")
	if err := postVPNSSHKeyStep(); err != nil {
		logFatal("ssh_key", err.Error(), nil)
	}
	if err := checkPublicInternet(); err != nil {
		logWarn("net_check", "public internet currently unreachable while on VPN (this can be expected before OCNA/full VPN)", nil)
	} else {
		logInfo("net_check", "public internet reachable while on VPN", nil)
	}

	// Phase 3: internal tools
	logSetPhase("phase3")
	fmt.Println("\n── Phase 3: Internal Tools (VPN ON) ──────────────────────────")
	if err := runPhase(p3, guid); err != nil {
		logFatal("phase3", err.Error(), nil)
	}
	if bastionConfigsSelected {
		logWarn("bastion_configs", "Bastion configs selected, but setup is not implemented yet; skipping", nil)
	}

	fmt.Println("\n✓ chs-onboard complete. Open a new terminal or run: source ~/.zshrc")
	logInfo("done", "completed successfully", nil)
}

func runPhase(tools []toolID, guid string) error {
	for i, t := range tools {
		fmt.Printf("\n  [%d/%d] %s\n", i+1, len(tools), t)
		if err := runTool(t, guid); err != nil {
			return fmt.Errorf("%s failed: %w", t, err)
		}
		fmt.Printf("  [✓] %s done\n", t)
	}
	return nil
}

func parseOnlyFlag(raw string) []toolID {
	var requested []toolID
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if t, ok := validToolIDs[s]; ok {
			requested = append(requested, t)
		} else {
			fmt.Fprintf(os.Stderr, "  [!] unknown tool: %q (use --list)\n", s)
		}
	}
	return resolveTools(requested)
}

func promptToolSelection(defaultIncludeGNOC bool) ([]toolID, bool, error) {
	optionalChoices := []string{
		"bastion setup",
		"allproxy",
		"hops-cli",
		"gnoc-helper",
	}
	defaultChoices := []string{}
	if defaultIncludeGNOC {
		defaultChoices = append(defaultChoices, "gnoc-helper")
	}

	selectedNames, err := uiChooseOptionalCheckboxes(
		"CHS Onboard Optional Tools",
		"Required tools are always installed. Choose optional tools to add:",
		optionalChoices,
		defaultChoices,
	)
	if err != nil {
		return nil, false, err
	}

	selected := make([]toolID, 0, len(selectedNames))
	bastionSelected := false
	for _, name := range selectedNames {
		switch name {
		case "bastion setup":
			bastionSelected = true
		case "allproxy":
			selected = append(selected, toolAllProxy)
		case "hops-cli":
			selected = append(selected, toolHopsCLI)
		case "gnoc-helper":
			selected = append(selected, toolGNOCHelper)
		}
	}

	if hasTool(selected, toolGNOCHelper) {
		_ = uiAlert("GNOC Helper Selection", "gnoc-helper selected. Additional GNOC tools will be installed automatically: stencil, silencer, ncpcli, and jit-pass.")
		selected = append(selected, toolStencil, toolSilencer, toolNCPCLI, toolJITPass)
	}

	return selected, bastionSelected, nil
}

func toolIDsToNames(ids []toolID) []string {
	seen := map[string]bool{}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		name := string(id)
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

func hasTool(tools []toolID, target toolID) bool {
	for _, t := range tools {
		if t == target {
			return true
		}
	}
	return false
}

func printToolList() {
	fmt.Println("Available tool IDs (--only=id1,id2,...):")
	names := make([]string, 0, len(validToolIDs))
	for name := range validToolIDs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Println("  " + name)
	}
}

func printBanner() {
	fmt.Println(`
  ██████╗██╗  ██╗███████╗      ██████╗ ███╗   ██╗██████╗  ██████╗  █████╗ ██████╗ ██████╗
 ██╔════╝██║  ██║██╔════╝     ██╔═══██╗████╗  ██║██╔══██╗██╔═══██╗██╔══██╗██╔══██╗██╔══██╗
 ██║     ███████║███████╗     ██║   ██║██╔██╗ ██║██████╔╝██║   ██║███████║██████╔╝██║  ██║
 ██║     ██╔══██║╚════██║     ██║   ██║██║╚██╗██║██╔══██╗██║   ██║██╔══██║██╔══██╗██║  ██║
 ╚██████╗██║  ██║███████║     ╚██████╔╝██║ ╚████║██████╔╝╚██████╔╝██║  ██║██║  ██║██████╔╝
  ╚═════╝╚═╝  ╚═╝╚══════╝      ╚═════╝ ╚═╝  ╚═══╝╚═════╝  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝
                                                       CHS New Hire Onboarding Tool`)
}
