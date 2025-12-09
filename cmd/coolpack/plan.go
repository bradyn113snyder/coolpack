package coolpack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/coollabsio/coolpack/pkg/detector"
	"github.com/spf13/cobra"
)

var (
	planOutputJSON bool
	planPath       string
	planOutFile    string
	planPackages   []string
)

var planCmd = &cobra.Command{
	Use:   "plan [path]",
	Short: "Detect and plan the build for an application",
	Long: `Analyze the application at the given path (or current directory),
detect the language, framework, and package manager, then output a build plan.

Environment Variables:
  COOLPACK_BASE_IMAGE      Override base Docker image
  COOLPACK_NODE_VERSION    Override Node.js version`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPlan,
}

func init() {
	planCmd.Flags().BoolVar(&planOutputJSON, "json", false, "Output plan as JSON")
	planCmd.Flags().StringVarP(&planPath, "path", "p", "", "Path to the application (defaults to current directory)")
	planCmd.Flags().StringVarP(&planOutFile, "out", "o", "", "Write plan to file (default: coolpack.json if flag used without value)")
	planCmd.Flags().Lookup("out").NoOptDefVal = "coolpack.json"
	planCmd.Flags().StringArrayVar(&planPackages, "packages", nil, "Additional APT packages to install (e.g., curl, wget)")
}

func runPlan(cmd *cobra.Command, args []string) error {
	// Determine the path to analyze
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	if planPath != "" {
		path = planPath
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Check if path exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	// Run detection
	d := detector.New(absPath)
	plan, err := d.Detect()
	if err != nil {
		return fmt.Errorf("detection failed: %w", err)
	}

	if plan == nil {
		fmt.Println("No supported application detected")
		return nil
	}

	// Apply custom packages (CLI > env > detected)
	applyCustomPackages(plan, planPackages)

	// Write to file if --out is specified
	if planOutFile != "" {
		outPath := planOutFile
		if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(absPath, outPath)
		}
		file, err := os.Create(outPath)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer file.Close()

		enc := json.NewEncoder(file)
		enc.SetIndent("", "  ")
		if err := enc.Encode(plan); err != nil {
			return fmt.Errorf("failed to write plan: %w", err)
		}
		fmt.Printf("Plan written to %s\n", outPath)
		return nil
	}

	// Output the plan
	if planOutputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(plan)
	}

	// Pretty print the plan
	printPlan(plan)
	return nil
}

// applyCustomPackages adds custom APT packages to the plan
func applyCustomPackages(plan *detector.Plan, packages []string) {
	if plan.Metadata == nil {
		plan.Metadata = make(map[string]interface{})
	}

	// Collect packages from CLI and env
	var customPackages []string

	// CLI packages
	if len(packages) > 0 {
		customPackages = append(customPackages, packages...)
	}

	// Environment variable (comma-separated)
	if env := os.Getenv("COOLPACK_PACKAGES"); env != "" {
		for _, pkg := range strings.Split(env, ",") {
			pkg = strings.TrimSpace(pkg)
			if pkg != "" {
				customPackages = append(customPackages, pkg)
			}
		}
	}

	if len(customPackages) == 0 {
		return
	}

	// Deduplicate
	seen := make(map[string]bool)
	unique := make([]string, 0, len(customPackages))
	for _, pkg := range customPackages {
		if !seen[pkg] {
			seen[pkg] = true
			unique = append(unique, pkg)
		}
	}

	plan.Metadata["custom_packages"] = unique
}

func printPlan(plan *detector.Plan) {
	fmt.Println("=== Coolpack Build Plan ===")
	fmt.Println()
	fmt.Printf("Provider:                %s\n", plan.Provider)
	fmt.Printf("Language:                %s\n", plan.Language)
	if plan.LanguageVersion != "" {
		fmt.Printf("Language Version:        %s\n", plan.LanguageVersion)
	}
	if plan.Framework != "" {
		fmt.Printf("Framework:               %s\n", plan.Framework)
	}
	if plan.FrameworkVersion != "" {
		fmt.Printf("Framework Version:       %s\n", plan.FrameworkVersion)
	}
	if plan.PackageManager != "" {
		fmt.Printf("Package Manager:         %s\n", plan.PackageManager)
	}
	if plan.PackageManagerVersion != "" {
		fmt.Printf("Package Manager Version: %s\n", plan.PackageManagerVersion)
	}
	if plan.InstallCommand != "" {
		fmt.Printf("Install Command:         %s\n", plan.InstallCommand)
	}
	if plan.BuildCommand != "" {
		fmt.Printf("Build Command:           %s\n", plan.BuildCommand)
	}
	if plan.StartCommand != "" {
		fmt.Printf("Start Command:           %s\n", plan.StartCommand)
	}
	if len(plan.DetectedFiles) > 0 {
		fmt.Println()
		fmt.Println("Detected Files:")
		for _, f := range plan.DetectedFiles {
			fmt.Printf("  - %s\n", f)
		}
	}
	if len(plan.Metadata) > 0 {
		fmt.Println()
		fmt.Println("Metadata:")
		// Sort keys for consistent output
		keys := make([]string, 0, len(plan.Metadata))
		for k := range plan.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %s: %v\n", k, plan.Metadata[k])
		}
	}
}
