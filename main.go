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
		requested, bastionSelected, err := promptToolSelection(*gnocFlag)
		if err != nil {
			logFatal("tool_select", err.Error(), nil)
		}
		if len(requested) == 0 && !bastionSelected {
			fmt.Println("\nNo tools selected. Exiting.")
			return
		}
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
	optionNames := make([]string, 0, len(validToolIDs)+1)
	for name := range validToolIDs {
		optionNames = append(optionNames, name)
	}
	optionNames = append(optionNames, bastionConfigsOption)
	sort.Strings(optionNames)

	defaultSelection := toolIDsToNames(allTools())
	if defaultIncludeGNOC {
		defaultSelection = append(defaultSelection, string(toolGNOCHelper))
	}

	selectedNames, err := uiChooseFromList(
		"CHS Onboard Tool Selection",
		"Choose which tools to install. If gnoc_helper is selected, stencil/silencer/jit_pass are included automatically.",
		optionNames,
		defaultSelection,
	)
	if err != nil {
		return nil, false, err
	}

	selected := make([]toolID, 0, len(selectedNames))
	bastionSelected := false
	for _, name := range selectedNames {
		if name == bastionConfigsOption {
			bastionSelected = true
			continue
		}
		if t, ok := validToolIDs[name]; ok {
			selected = append(selected, t)
		}
	}

	if hasTool(selected, toolGNOCHelper) {
		selected = append(selected, toolStencil, toolSilencer, toolJITPass)
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
	for name := range validToolIDs {
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
