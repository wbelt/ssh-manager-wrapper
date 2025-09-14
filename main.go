package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// HostConfig represents an SSH host configuration loaded from YAML
// and optionally overridden by flags or environment variables.
// Example YAML:
//   host: ipa.diydev.io
//   user: root
//   port: 25022
//   identity: C:/Users/me/.ssh/id_ed25519
// Keys are case-insensitive; dashes/underscores are normalized by Viper.

type HostConfig struct {
	Host         string `mapstructure:"host"`
	User         string `mapstructure:"user"`
	Port         int    `mapstructure:"port"`
	Identity string `mapstructure:"identity"`
}

const Version = "1.0.1" // Increment as needed

func main() {
	fmt.Println("gssh wrapper -- version", Version)
	v := viper.New()

	// Flags
	var (
		flagTarget    string
		flagConfigDir string
		flagHost      string
		flagUser      string
		flagPort      int
		flagIdentity  string
		flagList      bool
		flagDryRun    bool
		flagVerbose   bool
	)

	pflag.StringVarP(&flagTarget, "target", "t", "", "Target name (basename of YAML in --config, e.g. 'ipa' for hosts/ipa.yaml)")
	pflag.StringVarP(&flagConfigDir, "config", "c", "hosts", "Directory containing host YAML files")
	pflag.StringVar(&flagHost, "host", "", "Hostname or IP to connect to (overrides config)")
	pflag.StringVar(&flagUser, "user", "", "SSH user (overrides config)")
	pflag.IntVar(&flagPort, "port", 22, "SSH port (overrides config)")
	pflag.StringVarP(&flagIdentity, "identity", "i", "", "Path to private key file (overrides config)")
	pflag.BoolVar(&flagList, "list", false, "List available targets from --config-dir and exit")
	pflag.BoolVar(&flagDryRun, "dry-run", false, "Print the ssh command that would be executed and exit")
	pflag.BoolVarP(&flagVerbose, "verbose", "v", false, "Enable verbose logging")

	pflag.Parse()

	// Bind flags and env.
	must(v.BindPFlags(pflag.CommandLine))
	v.SetEnvPrefix("GSSH")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	// Defaults
	v.SetDefault("port", 22)
	v.SetDefault("config", flagConfigDir)

	// Determine possible config directories
	homeDir, err := os.UserHomeDir()
	must(err)
	configDirs := []string{
		flagConfigDir,
		filepath.Join(homeDir, "hosts"),
	}

	// Handle --list early (no need to load a specific target)
	if flagList {
		found := false
		for _, dir := range configDirs {
			targets, err := listTargets(dir)
			if err == nil && len(targets) > 0 {
				for _, t := range targets {
					fmt.Printf("%s (%s)\n", t, dir)
				}
				found = true
			}
		}
		if !found {
			fmt.Println("No targets found.")
		}
		os.Exit(0)
	}

	// Load target config file if target provided.
	if flagTarget != "" {
		v.SetConfigName(flagTarget)
		v.SetConfigType("yaml")
		for _, dir := range configDirs {
			v.AddConfigPath(dir)
		}
		if err := v.ReadInConfig(); err != nil {
			fatal("failed to read config for target '%s': %v", flagTarget, err)
		}
	}

	// At this point precedence is: flags > env > config > defaults.
	cfg := HostConfig{
		Host:         v.GetString("host"),
		User:         v.GetString("user"),
		Port:         v.GetInt("port"),
		Identity: v.GetString("identity"),
	}

	// Validate minimal requirements.
	if cfg.Host == "" {
		fatal("host is required. Set --host, GSSH_HOST, or provide a target via --target with host in YAML")
	}
	if cfg.User == "" {
		// Not fatal; ssh defaults to current user if empty. But we can keep empty to let ssh pick it.
		// If you want to enforce, uncomment the following:
		// fatal("user is required. Set --user, GSSH_USER, or provide via config")
	}
	if cfg.Port <= 0 {
		cfg.Port = 22
	}

	// Remaining args after flags are treated as the remote command.
	remoteCmd := pflag.Args()

	// Build ssh arguments
	sshArgs := buildSSHArgs(cfg, remoteCmd)

	if flagDryRun || flagVerbose {
		fmt.Println(renderCommand("ssh", sshArgs))
	}

	if flagDryRun {
		return
	}

	// Ensure ssh is available
	if _, err := exec.LookPath("ssh"); err != nil {
		fatal("ssh client not found in PATH. Install OpenSSH client and ensure 'ssh' is available: %v", err)
	}

	// Execute ssh
	cmd := exec.Command("ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Propagate exit code if possible
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fatal("ssh execution failed: %v", err)
	}
}

func listTargets(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".yaml" || ext == ".yml" {
			base := strings.TrimSuffix(name, ext)
			out = append(out, base)
		}
	}
	sort.Strings(out)
	return out, nil
}

func buildSSHArgs(cfg HostConfig, remoteCmd []string) []string {
	var args []string
	if cfg.Identity != "" {
		args = append(args, "-i", cfg.Identity)
	}
	if cfg.Port != 0 && cfg.Port != 22 {
		args = append(args, "-p", fmt.Sprintf("%d", cfg.Port))
	}

	dest := cfg.Host
	if cfg.User != "" {
		dest = fmt.Sprintf("%s@%s", cfg.User, cfg.Host)
	}
	args = append(args, dest)

	if len(remoteCmd) > 0 {
		args = append(args, remoteCmd...)
	}
	return args
}

func renderCommand(cmd string, args []string) string {
	parts := []string{cmd}
	for _, a := range args {
		if needsQuoting(a) {
			parts = append(parts, quote(a))
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

func needsQuoting(s string) bool {
	return strings.ContainsAny(s, " \t\"'")
}

func quote(s string) string {
	// Use double quotes and escape internal quotes/backslashes
	r := strings.ReplaceAll(s, "\\", "\\\\")
	r = strings.ReplaceAll(r, "\"", "\\\"")
	return "\"" + r + "\""
}

func warn(verbose bool, format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, "warn: "+format+"\n", args...)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func must(err error) {
	if err != nil {
		fatal("%v", err)
	}
}
