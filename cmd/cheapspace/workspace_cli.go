package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"cheapspace/internal/config"
	"cheapspace/internal/db"
	"cheapspace/internal/service"
)

func workspaceCommand(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cheapspace workspace [list|view|new|delete]")
	}

	switch args[0] {
	case "list":
		return workspaceListCommand(cfg, args[1:], stdout, stderr)
	case "view":
		return workspaceViewCommand(cfg, args[1:], stdout, stderr)
	case "new":
		return workspaceNewCommand(cfg, args[1:], stdout, stderr)
	case "delete":
		return workspaceDeleteCommand(cfg, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown workspace subcommand %q", args[0])
	}
}

func workspaceListCommand(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("workspace list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showDeleted := fs.Bool("all", false, "include deleted workspaces")
	if err := fs.Parse(args); err != nil {
		return err
	}

	_, svc, cleanup, err := openAppService(cfg, true)
	if err != nil {
		return err
	}
	defer cleanup()

	workspaces, err := svc.ListWorkspaces(context.Background(), *showDeleted)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSTATE\tBRANCH\tSSH")
	for _, workspace := range workspaces {
		sshCommand := "-"
		if workspace.SSHHost != "" && workspace.SSHPort > 0 {
			sshCommand = formatSSHCommand(workspace)
		}
		branch := workspace.RepoBranch
		if branch == "" {
			branch = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", workspace.ID, workspace.Name, workspace.State, branch, sshCommand)
	}
	return tw.Flush()
}

func workspaceViewCommand(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("workspace view", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: cheapspace workspace view <workspace-id>")
	}

	_, svc, cleanup, err := openAppService(cfg, true)
	if err != nil {
		return err
	}
	defer cleanup()

	details, err := svc.GetWorkspaceDetails(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}

	branch := details.Workspace.RepoBranch
	if branch == "" {
		branch = "default"
	}
	sshCommand := "pending"
	if details.Workspace.SSHHost != "" && details.Workspace.SSHPort > 0 {
		sshCommand = formatSSHCommand(details.Workspace)
	}

	fmt.Fprintf(stdout, "ID: %s\n", details.Workspace.ID)
	fmt.Fprintf(stdout, "Name: %s\n", details.Workspace.Name)
	fmt.Fprintf(stdout, "State: %s\n", details.Workspace.State)
	fmt.Fprintf(stdout, "Repository: %s\n", details.Workspace.RepoURL)
	fmt.Fprintf(stdout, "Branch: %s\n", branch)
	fmt.Fprintf(stdout, "Source: %s\n", details.Workspace.SourceType)
	fmt.Fprintf(stdout, "Resolved image: %s\n", emptyFallback(details.Workspace.ResolvedImageRef, "pending"))
	fmt.Fprintf(stdout, "CPU: %s\n", formatMillicores(details.Workspace.CPUMillis))
	fmt.Fprintf(stdout, "Memory: %d MB\n", details.Workspace.MemoryMB)
	fmt.Fprintf(stdout, "SSH: %s\n", sshCommand)
	fmt.Fprintf(stdout, "Jobs: %d\n", len(details.Jobs))
	return nil
}

func workspaceNewCommand(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("workspace new", flag.ContinueOnError)
	fs.SetOutput(stderr)

	name := fs.String("name", "", "display name")
	repoURL := fs.String("repo-url", "", "repository URL")
	repoBranch := fs.String("repo-branch", "", "repository branch")
	dotfilesURL := fs.String("dotfiles-url", "", "dotfiles repository URL")
	sourceType := fs.String("source-type", "builtin_image", "builtin_image|image_ref|dockerfile|nixpacks")
	sourceRef := fs.String("source-ref", "", "image reference, Dockerfile contents, or Nixpacks notes")
	sshPort := fs.Int("ssh-port", 0, "requested SSH host port")
	httpProxy := fs.String("http-proxy", "", "HTTP proxy")
	httpsProxy := fs.String("https-proxy", "", "HTTPS proxy")
	noProxy := fs.String("no-proxy", "", "NO_PROXY value")
	proxyPACURL := fs.String("proxy-pac-url", "", "proxy PAC URL")
	cpuCores := fs.Int("cpu-cores", 2, "CPU cores")
	memoryMB := fs.Int("memory-mb", 4096, "memory in MB")
	ttlMinutes := fs.Int("ttl-minutes", 480, "auto shutdown timeout in minutes")
	traefikEnabled := fs.Bool("traefik-enabled", false, "enable Traefik labels")
	traefikBaseDomain := fs.String("traefik-base-domain", "", "Traefik base domain")
	wait := fs.Bool("wait", true, "wait for queued jobs to complete")

	var sshKeys stringListFlag
	var sshKeyFiles stringListFlag
	fs.Var(&sshKeys, "ssh-key", "inline SSH public key (repeatable)")
	fs.Var(&sshKeyFiles, "ssh-key-file", "path to an SSH public key file (repeatable)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	collectedKeys, err := loadCLIKeys(sshKeys, sshKeyFiles)
	if err != nil {
		return err
	}

	_, svc, cleanup, err := openAppService(cfg, true)
	if err != nil {
		return err
	}
	defer cleanup()

	workspace, err := svc.CreateWorkspace(context.Background(), service.CreateWorkspaceInput{
		Name:              strings.TrimSpace(*name),
		RepoURL:           strings.TrimSpace(*repoURL),
		RepoBranch:        strings.TrimSpace(*repoBranch),
		DotfilesURL:       strings.TrimSpace(*dotfilesURL),
		SourceType:        strings.TrimSpace(*sourceType),
		SourceRef:         *sourceRef,
		SSHKeys:           collectedKeys,
		SSHPort:           *sshPort,
		HTTPProxy:         strings.TrimSpace(*httpProxy),
		HTTPSProxy:        strings.TrimSpace(*httpsProxy),
		NoProxy:           strings.TrimSpace(*noProxy),
		ProxyPACURL:       strings.TrimSpace(*proxyPACURL),
		CPUMillis:         cliCoresToMillicores(*cpuCores),
		MemoryMB:          *memoryMB,
		TTLMinutes:        *ttlMinutes,
		TraefikEnabled:    *traefikEnabled,
		TraefikBaseDomain: strings.TrimSpace(*traefikBaseDomain),
	})
	if err != nil {
		return err
	}

	if !*wait {
		fmt.Fprintf(stdout, "Queued workspace %s\n", workspace.ID)
		return nil
	}

	if err := svc.RunUntilIdle(context.Background(), 64); err != nil {
		return err
	}
	details, err := svc.GetWorkspaceDetails(context.Background(), workspace.ID)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Workspace %s is %s\n", details.Workspace.ID, details.Workspace.State)
	if details.Workspace.SSHHost != "" && details.Workspace.SSHPort > 0 {
		fmt.Fprintln(stdout, formatSSHCommand(details.Workspace))
	}
	return nil
}

func workspaceDeleteCommand(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("workspace delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	wait := fs.Bool("wait", true, "wait for queued jobs to complete")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: cheapspace workspace delete <workspace-id>")
	}

	_, svc, cleanup, err := openAppService(cfg, true)
	if err != nil {
		return err
	}
	defer cleanup()

	workspaceID := fs.Arg(0)
	if err := svc.QueueWorkspaceDelete(context.Background(), workspaceID); err != nil {
		return err
	}
	if !*wait {
		fmt.Fprintf(stdout, "Queued delete for workspace %s\n", workspaceID)
		return nil
	}

	if err := svc.RunUntilIdle(context.Background(), 64); err != nil {
		return err
	}

	details, err := svc.GetWorkspaceDetails(context.Background(), workspaceID)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Workspace %s is %s\n", details.Workspace.ID, details.Workspace.State)
	return nil
}

func openAppService(cfg config.Config, migrateDB bool) (*db.Store, *service.Service, func(), error) {
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, nil, err
	}
	if migrateDB {
		if err := db.Migrate(sqlDB); err != nil {
			_ = sqlDB.Close()
			return nil, nil, nil, err
		}
	}
	store := db.NewStore(sqlDB)
	rt, err := runtimeFromConfig(cfg)
	if err != nil {
		_ = store.Close()
		return nil, nil, nil, err
	}
	svc := service.New(cfg, store, rt)
	return store, svc, func() {
		_ = store.Close()
	}, nil
}

func loadCLIKeys(inline stringListFlag, keyFiles stringListFlag) ([]string, error) {
	keys := make([]string, 0, len(inline)+len(keyFiles))
	keys = append(keys, inline...)
	for _, path := range keyFiles {
		payload, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		keys = append(keys, string(payload))
	}
	return keys, nil
}

func formatSSHCommand(workspace db.Workspace) string {
	return fmt.Sprintf("ssh -p %d codespace@%s", workspace.SSHPort, workspace.SSHHost)
}

func formatMillicores(millicores int) string {
	if millicores <= 0 {
		return "-"
	}
	if millicores%1000 == 0 {
		cores := millicores / 1000
		if cores == 1 {
			return "1 core"
		}
		return fmt.Sprintf("%d cores", cores)
	}
	return fmt.Sprintf("%.1f cores", float64(millicores)/1000.0)
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func cliCoresToMillicores(cores int) int {
	if cores <= 0 {
		return 0
	}
	return cores * 1000
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}
