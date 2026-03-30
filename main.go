package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/jee4nc/packwatch/internal/lockfile"
	"github.com/jee4nc/packwatch/internal/node"
	"github.com/jee4nc/packwatch/internal/npmrc"
	"github.com/jee4nc/packwatch/internal/registry"
	"github.com/jee4nc/packwatch/internal/runner"
	"github.com/jee4nc/packwatch/internal/security"
	"github.com/jee4nc/packwatch/internal/semver"
	"github.com/jee4nc/packwatch/internal/styles"
	"github.com/jee4nc/packwatch/internal/tui"
	"github.com/jee4nc/packwatch/internal/unused"
)

// Set at build time via ldflags.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	prodOnly := flag.Bool("prod-only", false, "Show only production dependencies")
	devOnly := flag.Bool("dev-only", false, "Show only dev dependencies")
	noColor := flag.Bool("no-color", false, "Disable color output")
	jsonOut := flag.Bool("json", false, "Output JSON (no interactive TUI)")
	showVersion := flag.Bool("version", false, "Show version and exit")
	unusedFlag := flag.Bool("unused", false, "Detect unused dependencies")
	flag.Parse()

	if *showVersion {
		fmt.Printf("packwatch %s (%s) built %s\n", version, commit, buildDate)
		os.Exit(0)
	}

	if *prodOnly && *devOnly {
		fmt.Fprintf(os.Stderr, "Error: --prod-only and --dev-only are mutually exclusive\n")
		os.Exit(1)
	}

	styles.Init(*noColor)

	// Banner
	fmt.Println()
	fmt.Println(styles.Banner.Render(styles.Emoji("📦 ") + "packwatch " + version))
	fmt.Println()

	// --unused mode: detect unused dependencies and exit
	if *unusedFlag {
		runUnusedMode(*jsonOut)
		return
	}

	// 1. Detect Node version
	nodeDetection, err := node.Detect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s\n", styles.Emoji("❌ "), styles.Red.Render(err.Error()))
		os.Exit(1)
	}
	fmt.Printf("  %sNode %s  %s\n",
		styles.Emoji("⬢  "),
		styles.BoldGreen.Render(nodeDetection.Version.String()),
		styles.Gray.Render("from "+nodeDetection.Source))

	// 2. Parse .npmrc for registry config
	npmrcCfg := npmrc.Parse()
	if summary := npmrcCfg.Summary(); summary != "" {
		fmt.Printf("  %sRegistries: %s\n",
			styles.Emoji("🔗 "),
			styles.Gray.Render(summary))
	}

	// 3. Parse lockfile
	parsed, err := lockfile.Parse()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s\n", styles.Emoji("❌ "), styles.Red.Render(err.Error()))
		os.Exit(1)
	}
	fmt.Printf("  %sLockfile v%d — %d direct dependencies\n",
		styles.Emoji("🔒 "),
		parsed.LockVersion,
		len(parsed.Packages))

	// Check project engines constraint against active node
	if parsed.ProjectEngines.Node != "" {
		if !semver.SatisfiesConstraints(nodeDetection.Version, parsed.ProjectEngines.Node) {
			fmt.Printf("  %s%s\n",
				styles.Emoji("⚠️  "),
				styles.BoldYellow.Render(fmt.Sprintf("engines.node requires %q but active Node is %s",
					parsed.ProjectEngines.Node, nodeDetection.Version.String())))
		}
	}

	// Filter by flags
	var packages []lockfile.PackageInfo
	for _, p := range parsed.Packages {
		if *prodOnly && p.IsDev {
			continue
		}
		if *devOnly && !p.IsDev {
			continue
		}
		packages = append(packages, p)
	}

	if len(packages) == 0 {
		fmt.Printf("\n  %s No dependencies to check.\n", styles.Emoji("✅ "))
		os.Exit(0)
	}

	// 4. Fetch registry info
	names := make([]string, len(packages))
	for i, p := range packages {
		names[i] = p.Name
	}

	fmt.Printf("\n  %sChecking npm registry for %d packages...\n",
		styles.Emoji("🔍 "), len(names))

	var mu sync.Mutex
	registryResults := registry.Fetch(names, npmrcCfg, func(completed, total int) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Printf("\r%s", styles.ProgressBar(completed, total, 30))
	})
	fmt.Println() // newline after progress bar

	// 5. Build reverse peer dependency map:
	//    For each installed package, check what peer constraints it imposes on other packages.
	//    This lets us warn when updating package A would break package B's peerDependencies.
	type peerConstraint struct {
		constraint string
		source     string // package that imposes this constraint
	}
	peerConstraintMap := map[string][]peerConstraint{}

	for i, pkg := range packages {
		reg := registryResults[i]
		if reg.Error != nil {
			continue
		}
		peers, ok := reg.PeerDeps[pkg.Version.String()]
		if !ok {
			continue
		}
		for depName, constraint := range peers {
			peerConstraintMap[depName] = append(peerConstraintMap[depName], peerConstraint{
				constraint: constraint,
				source:     pkg.Name,
			})
		}
	}

	// 6. Build items list
	var items []tui.Item
	var errors []string
	upToDateCount := 0

	for i, pkg := range packages {
		reg := registryResults[i]
		if reg.Error != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", pkg.Name, reg.Error))
			continue
		}

		updateType := semver.ClassifyUpdate(pkg.Version, reg.Latest)
		available := reg.Latest.String()
		nodeWarning := ""
		peerWarning := ""
		compatVersion := ""

		// Check if latest requires newer Node
		if updateType != semver.UpToDate {
			latestEngines, hasEngines := reg.Engines[reg.Latest.String()]
			if hasEngines && !semver.SatisfiesConstraints(nodeDetection.Version, latestEngines) {
				// Find compatible version
				compat, found := registry.FindCompatibleLatest(reg, nodeDetection.Version)
				if found && compat.Compare(pkg.Version) > 0 {
					minNode, hasMin := semver.ExtractMinNodeVersion(latestEngines)
					nodeWarning = fmt.Sprintf("latest (%s) requires Node >=%s; suggesting %s instead",
						reg.Latest.String(), minNode.String(), compat.String())
					if !hasMin {
						nodeWarning = fmt.Sprintf("latest (%s) requires %s; suggesting %s instead",
							reg.Latest.String(), latestEngines, compat.String())
					}
					available = compat.String()
					compatVersion = compat.String()
					updateType = semver.ClassifyUpdate(pkg.Version, compat)
				} else if !found {
					nodeWarning = fmt.Sprintf("no compatible version found for Node %s", nodeDetection.Version.String())
					updateType = semver.UpToDate // can't update
				}
			}
		}

		// Check peer dependency constraints from other installed packages
		if updateType != semver.UpToDate {
			if pcs, hasPeerConstraints := peerConstraintMap[pkg.Name]; hasPeerConstraints {
				candidateVersion, _ := semver.Parse(available)
				var conflictSources []string
				for _, pc := range pcs {
					if !semver.SatisfiesConstraints(candidateVersion, pc.constraint) {
						conflictSources = append(conflictSources, pc.source)
					}
				}
				if len(conflictSources) > 0 {
					var constraints []string
					for _, pc := range pcs {
						constraints = append(constraints, pc.constraint)
					}
					compat, found := registry.FindPeerCompatibleLatest(reg, nodeDetection.Version, constraints)
					if found && compat.Compare(pkg.Version) > 0 {
						peerWarning = fmt.Sprintf("latest (%s) breaks peer deps of %s; suggesting %s instead",
							available, strings.Join(conflictSources, ", "), compat.String())
						available = compat.String()
						compatVersion = compat.String()
						updateType = semver.ClassifyUpdate(pkg.Version, compat)
					} else if !found {
						peerWarning = fmt.Sprintf("no compatible version found (peer deps: %s)",
							strings.Join(conflictSources, ", "))
						updateType = semver.UpToDate
					}
				}
			}
		}

		if updateType == semver.UpToDate {
			upToDateCount++
			continue
		}

		items = append(items, tui.Item{
			Name:          pkg.Name,
			Installed:     pkg.Version.String(),
			Available:     available,
			UpdateType:    updateType.String(),
			IsDev:         pkg.IsDev,
			Selectable:    true,
			NodeWarning:   nodeWarning,
			PeerWarning:   peerWarning,
			CompatVersion: compatVersion,
		})
	}

	// Sort: prod before dev, alphabetical within group
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDev != items[j].IsDev {
			return !items[i].IsDev // prod first
		}
		return items[i].Name < items[j].Name
	})

	// Report errors
	if len(errors) > 0 {
		fmt.Printf("\n  %s%s\n",
			styles.Emoji("⚠️  "),
			styles.Yellow.Render(fmt.Sprintf("%d packages failed to fetch:", len(errors))))
		for _, e := range errors {
			fmt.Printf("    %s %s\n", styles.Gray.Render("•"), styles.Gray.Render(e))
		}
	}

	// Security check (--health)
	vulnCount := 0
	sevCounts := map[string]int{}
	{
		var queries []security.Query
		for _, it := range items {
			queries = append(queries, security.Query{
				Name:             it.Name,
				InstalledVersion: it.Installed,
				AvailableVersion: it.Available,
			})
		}

		fmt.Printf("\n  %sChecking security advisories for %d packages...\n",
			styles.Emoji("🛡️  "), len(queries))

		secResults := security.Check(queries, func(completed, total int) {
			mu.Lock()
			defer mu.Unlock()
			fmt.Printf("\r%s", styles.ProgressBar(completed, total, 30))
		})
		fmt.Println()

		for i, sr := range secResults {
			if sr.Error != nil {
				errors = append(errors, fmt.Sprintf("%s: security check: %v", sr.Name, sr.Error))
				continue
			}
			if len(sr.Vulnerabilities) > 0 {
				items[i].VulnCount = len(sr.Vulnerabilities)
				items[i].VulnSeverity = sr.HighestSeverity
				items[i].VulnFixed = sr.FixedByUpdate
				for _, v := range sr.Vulnerabilities {
					items[i].Vulns = append(items[i].Vulns, tui.VulnInfo{
						ID:       v.ID,
						Summary:  v.Summary,
						Severity: v.Severity,
						Fixed:    v.Fixed,
					})
				}
				vulnCount++
				sevCounts[sr.HighestSeverity]++
			}
		}
	}

	// Count updates
	updateCount := 0
	for _, it := range items {
		if it.Selectable {
			updateCount++
		}
	}

	if updateCount == 0 && vulnCount == 0 {
		fmt.Printf("\n  %s All %d packages are up-to-date!\n",
			styles.Emoji("✅ "), upToDateCount)
		os.Exit(0)
	}

	var summaryParts []string
	if updateCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d updates available", updateCount))
	}
	if vulnCount > 0 {
		var sevParts []string
		for _, sev := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
			if n, ok := sevCounts[sev]; ok && n > 0 {
				sevParts = append(sevParts, fmt.Sprintf("%d %s", n, sev))
			}
		}
		vulnSummary := fmt.Sprintf("%d vulnerable", vulnCount)
		if len(sevParts) > 0 {
			vulnSummary += " (" + strings.Join(sevParts, ", ") + ")"
		}
		summaryParts = append(summaryParts, vulnSummary)
	}
	fmt.Printf("\n  %s%s\n",
		styles.Emoji("📊 "),
		styles.Bold.Render(strings.Join(summaryParts, " · ")))

	// 6. JSON mode
	if *jsonOut {
		outputJSON(items, nodeDetection.Version.String())
		return
	}

	// 7. Interactive TUI
	result := tui.Run(items)
	if result.Aborted {
		fmt.Printf("\n  %s Cancelled.\n", styles.Emoji("👋 "))
		os.Exit(0)
	}

	if len(result.Selected) == 0 {
		fmt.Printf("\n  %s Nothing selected.\n", styles.Emoji("🤷 "))
		os.Exit(0)
	}

	// 8. Generate and show commands
	cmds := runner.GenerateCommands(result.Selected)
	runner.PrintCommands(cmds)

	// 9. Ask to execute
	if tui.Confirm("Execute these commands?") {
		if err := runner.Execute(cmds); err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s %s\n", styles.Emoji("❌ "), styles.Red.Render(err.Error()))
			os.Exit(1)
		}
		fmt.Printf("\n  %s All done!\n", styles.Emoji("🎉 "))
	} else {
		fmt.Printf("\n  %s Commands not executed. Copy and run them manually.\n", styles.Emoji("📋 "))
	}
}

type jsonVuln struct {
	ID       string `json:"id"`
	Summary  string `json:"summary"`
	Severity string `json:"severity"`
	URL      string `json:"url,omitempty"`
	Fixed    bool   `json:"fixedByUpdate,omitempty"`
}

type jsonItem struct {
	Name            string     `json:"name"`
	Installed       string     `json:"installed"`
	Available       string     `json:"available"`
	UpdateType      string     `json:"updateType"`
	IsDev           bool       `json:"isDev"`
	NodeWarning     string     `json:"nodeWarning,omitempty"`
	PeerWarning     string     `json:"peerWarning,omitempty"`
	CompatVersion   string     `json:"compatVersion,omitempty"`
	VulnCount       int        `json:"vulnCount,omitempty"`
	VulnSeverity    string     `json:"vulnSeverity,omitempty"`
	VulnFixed       int        `json:"vulnFixedByUpdate,omitempty"`
	Vulnerabilities []jsonVuln `json:"vulnerabilities,omitempty"`
}

type jsonOutput struct {
	Version  string     `json:"packwatchVersion"`
	Node     string     `json:"nodeVersion"`
	Packages []jsonItem `json:"packages"`
}

func outputJSON(items []tui.Item, nodeVersion string) {
	var pkgs []jsonItem
	for _, it := range items {
		if !it.Selectable && it.VulnCount == 0 {
			continue
		}
		ji := jsonItem{
			Name:          it.Name,
			Installed:     it.Installed,
			Available:     it.Available,
			UpdateType:    it.UpdateType,
			IsDev:         it.IsDev,
			NodeWarning:   it.NodeWarning,
			PeerWarning:   it.PeerWarning,
			CompatVersion: it.CompatVersion,
			VulnCount:     it.VulnCount,
			VulnSeverity:  it.VulnSeverity,
			VulnFixed:     it.VulnFixed,
		}
		for _, v := range it.Vulns {
			ji.Vulnerabilities = append(ji.Vulnerabilities, jsonVuln{
				ID:       v.ID,
				Summary:  v.Summary,
				Severity: v.Severity,
				Fixed:    v.Fixed,
			})
		}
		pkgs = append(pkgs, ji)
	}

	out := jsonOutput{
		Version:  version,
		Node:     nodeVersion,
		Packages: pkgs,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}

func runUnusedMode(jsonOut bool) {
	fmt.Printf("  %sScanning project for unused dependencies...\n",
		styles.Emoji("🔍 "))

	result, err := unused.Scan()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s\n", styles.Emoji("❌ "), styles.Red.Render(err.Error()))
		os.Exit(1)
	}

	fmt.Printf("  %sScanned %d files — %d direct dependencies\n",
		styles.Emoji("📂 "),
		result.ScannedFiles,
		result.Total)

	if len(result.Unused) == 0 {
		fmt.Printf("\n  %s All %d dependencies are in use!\n",
			styles.Emoji("✅ "), result.Total)
		os.Exit(0)
	}

	fmt.Printf("\n  %s%s\n",
		styles.Emoji("📊 "),
		styles.Bold.Render(fmt.Sprintf("%d unused dependencies found", len(result.Unused))))

	if jsonOut {
		outputUnusedJSON(result)
		return
	}

	// Build TUI items
	var items []tui.UnusedItem
	for _, pkg := range result.Unused {
		items = append(items, tui.UnusedItem{
			Name:    pkg.Name,
			Version: pkg.Version,
		})
	}

	// Interactive selection
	tuiResult := tui.RunUnused(items, result.Total, result.ScannedFiles)
	if tuiResult.Aborted {
		fmt.Printf("\n  %s Cancelled.\n", styles.Emoji("👋 "))
		os.Exit(0)
	}

	if len(tuiResult.Selected) == 0 {
		fmt.Printf("\n  %s Nothing selected.\n", styles.Emoji("🤷 "))
		os.Exit(0)
	}

	// Generate and show commands
	cmds := runner.GenerateUninstallCommands(tuiResult.Selected)
	runner.PrintCommands(cmds)

	// Ask to execute
	if tui.Confirm("Execute these commands?") {
		if err := runner.Execute(cmds); err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s %s\n", styles.Emoji("❌ "), styles.Red.Render(err.Error()))
			os.Exit(1)
		}
		fmt.Printf("\n  %s %d unused dependencies removed!\n",
			styles.Emoji("🎉 "), len(tuiResult.Selected))
	} else {
		fmt.Printf("\n  %s Commands not executed. Copy and run them manually.\n", styles.Emoji("📋 "))
	}
}

type jsonUnusedPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type jsonUnusedOutput struct {
	Version           string              `json:"packwatchVersion"`
	UnusedPackages    []jsonUnusedPackage `json:"unusedPackages"`
	TotalDependencies int                 `json:"totalDependencies"`
	ScannedFiles      int                 `json:"scannedFiles"`
}

func outputUnusedJSON(result unused.ScanResult) {
	var pkgs []jsonUnusedPackage
	for _, p := range result.Unused {
		pkgs = append(pkgs, jsonUnusedPackage{
			Name:    p.Name,
			Version: p.Version,
		})
	}

	out := jsonUnusedOutput{
		Version:           version,
		UnusedPackages:    pkgs,
		TotalDependencies: result.Total,
		ScannedFiles:      result.ScannedFiles,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	flag.Usage = func() {
		w := os.Stderr
		fmt.Fprintf(w, "Usage: packwatch [flags]\n\n")
		fmt.Fprintf(w, "packwatch reads package-lock.json, checks npm for updates,\n")
		fmt.Fprintf(w, "and lets you interactively select which packages to update.\n")
		fmt.Fprintf(w, "It is Node-version-aware: if the latest version of a package\n")
		fmt.Fprintf(w, "requires a newer Node, it suggests the best compatible version.\n\n")
		fmt.Fprintf(w, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(w, "\nExamples:\n")
		fmt.Fprintf(w, "  packwatch                  Interactive update selector + security check\n")
		fmt.Fprintf(w, "  packwatch --prod-only      Only show production dependencies\n")
		fmt.Fprintf(w, "  packwatch --json           Output updates + vulnerabilities as JSON\n")
		fmt.Fprintf(w, "  packwatch --unused         Detect unused dependencies\n")
		fmt.Fprintf(w, "  packwatch --unused --json  Output unused dependencies as JSON\n")
		fmt.Fprintf(w, "  packwatch --json | jq '.packages[] | select(.vulnCount > 0)'\n")
		fmt.Fprintf(w, "  packwatch --json | jq '.packages[] | select(.updateType==\"major\")'\n")
		fmt.Fprintf(w, "\nEnvironment:\n")
		fmt.Fprintf(w, "  NO_COLOR=1                 Disable color output\n")
		fmt.Fprintf(w, "\nNode version detection (priority order):\n")
		fmt.Fprintf(w, "  .nvmrc → .node-version → node --version\n")
	}
}
