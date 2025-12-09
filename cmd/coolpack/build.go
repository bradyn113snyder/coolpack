package coolpack

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/coollabsio/coolpack/pkg/app"
	"github.com/coollabsio/coolpack/pkg/detector"
	"github.com/coollabsio/coolpack/pkg/generator"
	"github.com/spf13/cobra"
)

var (
	buildPath         string
	buildImageName    string
	buildTag          string
	buildNoCache      bool
	buildBuildEnvs    []string
	buildInstallCmd   string
	buildBuildCmd     string
	buildStartCmd     string
	buildStaticServer string
	buildOutputDir    string
	buildSPA          bool
	buildNoSPA        bool
	buildPackages     []string
	buildPlanFile     string
)

var buildCmd = &cobra.Command{
	Use:   "build [path]",
	Short: "Build a container image for the application",
	Long: `Build a container image for the application at the given path.
This command runs detection (like 'plan'), generates a Dockerfile
in .coolpack/, and then builds the container image.

If a coolpack.json file exists in the project root, it will be used
instead of running detection. Use --plan to specify a different file.

Environment Variables:
  COOLPACK_INSTALL_CMD     Override install command
  COOLPACK_BUILD_CMD       Override build command
  COOLPACK_START_CMD       Override start command
  COOLPACK_BASE_IMAGE      Override base Docker image (e.g., node:20)
  COOLPACK_NODE_VERSION    Override Node.js version
  COOLPACK_STATIC_SERVER   Static file server: caddy (default), nginx
  COOLPACK_SPA_OUTPUT_DIR  Override static output directory (e.g., dist, build)
  COOLPACK_SPA             Enable SPA mode (serves index.html for all routes)
  COOLPACK_PACKAGES        Additional APT packages (comma-separated)

Build-time env vars (--build-env) are available during build (e.g., for
Next.js NEXT_PUBLIC_*, Vite VITE_*, SvelteKit $env/static/*).

Runtime env vars should be passed via 'docker run -e' for secrets and
config that changes per environment.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBuild,
}

func init() {
	buildCmd.Flags().StringVarP(&buildPath, "path", "p", "", "Path to the application (defaults to current directory)")
	buildCmd.Flags().StringVarP(&buildImageName, "name", "n", "", "Image name (defaults to directory name)")
	buildCmd.Flags().StringVarP(&buildTag, "tag", "t", "latest", "Image tag")
	buildCmd.Flags().BoolVar(&buildNoCache, "no-cache", false, "Build without cache")
	buildCmd.Flags().StringArrayVar(&buildBuildEnvs, "build-env", nil, "Build-time environment variables (KEY=value or KEY to use current env)")
	buildCmd.Flags().StringVarP(&buildInstallCmd, "install-cmd", "i", "", "Override install command")
	buildCmd.Flags().StringVarP(&buildBuildCmd, "build-cmd", "b", "", "Override build command")
	buildCmd.Flags().StringVarP(&buildStartCmd, "start-cmd", "s", "", "Override start command")
	buildCmd.Flags().StringVar(&buildStaticServer, "static-server", "", "Static file server: caddy (default), nginx")
	buildCmd.Flags().StringVar(&buildOutputDir, "output-dir", "", "Override static output directory (e.g., dist, build, out)")
	buildCmd.Flags().BoolVar(&buildSPA, "spa", false, "Enable SPA mode (serves index.html for all routes)")
	buildCmd.Flags().BoolVar(&buildNoSPA, "no-spa", false, "Disable SPA mode (overrides auto-detection)")
	buildCmd.Flags().StringArrayVar(&buildPackages, "packages", nil, "Additional APT packages to install (e.g., curl, wget)")
	buildCmd.Flags().StringVar(&buildPlanFile, "plan", "", "Use plan file instead of detection (e.g., coolpack.json)")
}

func runBuild(cmd *cobra.Command, args []string) error {
	// Determine the path to analyze
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	if buildPath != "" {
		path = buildPath
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

	// Determine image name
	imageName := buildImageName
	if imageName == "" {
		imageName = filepath.Base(absPath)
		// Sanitize image name (lowercase, replace invalid chars)
		imageName = strings.ToLower(imageName)
		imageName = strings.ReplaceAll(imageName, " ", "-")
	}

	fullImageName := fmt.Sprintf("%s:%s", imageName, buildTag)

	fmt.Printf("Building image: %s\n", fullImageName)

	var plan *app.Plan

	// Check for plan file: --plan flag > coolpack.json in project root
	planFile := buildPlanFile
	if planFile == "" {
		defaultPlanFile := filepath.Join(absPath, "coolpack.json")
		if _, err := os.Stat(defaultPlanFile); err == nil {
			planFile = defaultPlanFile
		}
	}

	if planFile != "" {
		// Load plan from file
		fmt.Printf("Using plan file: %s\n", planFile)
		plan, err = loadPlanFromFile(planFile)
		if err != nil {
			return fmt.Errorf("failed to load plan file: %w", err)
		}
	} else {
		// Run detection
		fmt.Println("Detecting application...")
		d := detector.New(absPath)
		plan, err = d.Detect()
		if err != nil {
			return fmt.Errorf("detection failed: %w", err)
		}

		if plan == nil {
			return fmt.Errorf("no supported application detected")
		}
	}

	// Apply command overrides (CLI > env > detected)
	applyCommandOverrides(plan, buildInstallCmd, buildBuildCmd, buildStartCmd)

	// Apply static server setting (CLI > env > default)
	applyStaticServerSetting(plan, buildStaticServer)

	// Apply SPA setting (CLI > env > auto-detected)
	applySPASetting(plan, buildSPA, buildNoSPA)

	// Apply output directory override (CLI > env > framework default)
	applyOutputDirSetting(plan, buildOutputDir)

	// Apply custom packages (CLI > env > detected)
	applyCustomPackagesBuild(plan, buildPackages)

	// Print detection summary
	framework := plan.Framework
	if framework == "" {
		framework = "generic"
	}
	fmt.Printf("Detected: %s %s", plan.Language, framework)
	if plan.PackageManager != "" {
		pmVersion := ""
		if plan.PackageManagerVersion != "" {
			pmVersion = "@" + plan.PackageManagerVersion
		}
		fmt.Printf(" (%s%s)", plan.PackageManager, pmVersion)
	}
	// Print output type and SPA mode
	if ot, ok := plan.Metadata["output_type"].(string); ok {
		fmt.Printf(" [%s", ot)
		if isSPA, ok := plan.Metadata["is_spa"].(bool); ok && isSPA {
			fmt.Printf("/spa")
		}
		fmt.Printf("]")
	}
	fmt.Println()

	// Parse build environment variables
	envMap := parseEnvVars(buildBuildEnvs)
	if len(envMap) > 0 {
		plan.BuildEnv = envMap
	}

	// Create .coolpack directory
	coolpackDir := filepath.Join(absPath, ".coolpack")
	if err := os.MkdirAll(coolpackDir, 0755); err != nil {
		return fmt.Errorf("failed to create .coolpack directory: %w", err)
	}

	// Generate Dockerfile
	fmt.Println("Generating Dockerfile...")
	gen := generator.New(plan)
	dockerfile, err := gen.GenerateDockerfile()
	if err != nil {
		return fmt.Errorf("failed to generate Dockerfile: %w", err)
	}

	// Write Dockerfile
	dockerfilePath := filepath.Join(coolpackDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	// Build Docker image
	fmt.Println("Building Docker image...")
	dockerArgs := []string{
		"build",
		"-t", fullImageName,
		"-f", dockerfilePath,
	}

	if buildNoCache {
		dockerArgs = append(dockerArgs, "--no-cache")
	}

	// Add build args for environment variables
	for key, value := range envMap {
		dockerArgs = append(dockerArgs, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	dockerArgs = append(dockerArgs, absPath)

	dockerCmd := exec.Command("docker", dockerArgs...)
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr
	dockerCmd.Dir = absPath

	if err := dockerCmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	fmt.Printf("\nSuccessfully built image: %s\n", fullImageName)

	// Show correct port based on output type
	port := "3000"
	outputType := "server"
	if ot, ok := plan.Metadata["output_type"].(string); ok && ot == "static" {
		port = "80"
		outputType = "static"
	}

	// Show output type and SPA mode
	if isSPA, ok := plan.Metadata["is_spa"].(bool); ok && isSPA {
		fmt.Printf("Output: %s (SPA mode enabled)\n", outputType)
	} else {
		fmt.Printf("Output: %s\n", outputType)
	}

	fmt.Printf("Run with: docker run -p %s:%s %s\n", port, port, fullImageName)
	fmt.Printf("Run (development only): docker run --rm -it -p %s:%s %s\n", port, port, fullImageName)

	return nil
}

// parseEnvVars parses environment variable arguments
// Supports KEY=value format or KEY (pulls from current environment)
func parseEnvVars(envArgs []string) map[string]string {
	result := make(map[string]string)
	for _, env := range envArgs {
		if idx := strings.Index(env, "="); idx != -1 {
			// KEY=value format
			key := env[:idx]
			value := env[idx+1:]
			result[key] = value
		} else {
			// KEY only - pull from current environment
			if value, exists := os.LookupEnv(env); exists {
				result[env] = value
			}
		}
	}
	return result
}

// applyCommandOverrides applies command overrides from CLI flags or env vars
// Priority: CLI flags > Environment variables > Auto-detected
func applyCommandOverrides(plan *detector.Plan, installCmd, buildCmd, startCmd string) {
	// Install command: CLI > env > detected
	if installCmd != "" {
		plan.InstallCommand = installCmd
	} else if env := os.Getenv("COOLPACK_INSTALL_CMD"); env != "" {
		plan.InstallCommand = env
	}

	// Build command: CLI > env > detected
	if buildCmd != "" {
		plan.BuildCommand = buildCmd
	} else if env := os.Getenv("COOLPACK_BUILD_CMD"); env != "" {
		plan.BuildCommand = env
	}

	// Start command: CLI > env > detected
	if startCmd != "" {
		plan.StartCommand = startCmd
	} else if env := os.Getenv("COOLPACK_START_CMD"); env != "" {
		plan.StartCommand = env
	}
}

// applyStaticServerSetting applies static server setting from CLI or env var
// Priority: CLI flag > Environment variable > default (caddy)
func applyStaticServerSetting(plan *detector.Plan, staticServer string) {
	if plan.Metadata == nil {
		plan.Metadata = make(map[string]interface{})
	}

	if staticServer != "" {
		plan.Metadata["static_server"] = staticServer
	} else if env := os.Getenv("COOLPACK_STATIC_SERVER"); env != "" {
		plan.Metadata["static_server"] = env
	}
	// Default is "caddy" which is handled in generator
}

// applySPASetting applies SPA setting from CLI or env var
// Priority: --no-spa/COOLPACK_NO_SPA > --spa/COOLPACK_SPA > auto-detected
func applySPASetting(plan *detector.Plan, spa bool, noSPA bool) {
	if plan.Metadata == nil {
		plan.Metadata = make(map[string]interface{})
	}

	// --no-spa and COOLPACK_NO_SPA take highest priority
	if noSPA {
		delete(plan.Metadata, "is_spa")
		return
	}
	if env := os.Getenv("COOLPACK_NO_SPA"); env == "true" || env == "1" {
		delete(plan.Metadata, "is_spa")
		return
	}

	if spa {
		plan.Metadata["is_spa"] = true
	} else if env := os.Getenv("COOLPACK_SPA"); env == "true" || env == "1" {
		plan.Metadata["is_spa"] = true
	}
	// Auto-detected value is already in metadata from provider
}

// applyOutputDirSetting applies output directory override from CLI or env var
// Priority: CLI flag > Environment variable > framework default (handled in generator)
func applyOutputDirSetting(plan *detector.Plan, outputDir string) {
	if plan.Metadata == nil {
		plan.Metadata = make(map[string]interface{})
	}

	if outputDir != "" {
		plan.Metadata["output_dir_override"] = outputDir
	} else if env := os.Getenv("COOLPACK_SPA_OUTPUT_DIR"); env != "" {
		plan.Metadata["output_dir_override"] = env
	}
}

// applyCustomPackagesBuild adds custom APT packages to the plan (merges with existing)
func applyCustomPackagesBuild(plan *detector.Plan, packages []string) {
	if plan.Metadata == nil {
		plan.Metadata = make(map[string]interface{})
	}

	// Start with existing custom packages from plan file
	var customPackages []string
	if existing, ok := plan.Metadata["custom_packages"].([]interface{}); ok {
		for _, pkg := range existing {
			if s, ok := pkg.(string); ok {
				customPackages = append(customPackages, s)
			}
		}
	} else if existing, ok := plan.Metadata["custom_packages"].([]string); ok {
		customPackages = append(customPackages, existing...)
	}

	// Add CLI packages
	if len(packages) > 0 {
		customPackages = append(customPackages, packages...)
	}

	// Add environment variable packages (comma-separated)
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

// loadPlanFromFile loads a build plan from a JSON file
func loadPlanFromFile(path string) (*app.Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var plan app.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &plan, nil
}
