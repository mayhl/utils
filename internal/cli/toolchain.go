package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/render"
	"github.com/mayhl/mayhl_utils/internal/setup"
)

// toolchainCmd is `mu setup toolchain`: install the dev toolchain with mise. Phase 2 of
// machine onboarding (runs ON the box, decoupled from the phase-1 push in `mu setup
// onboard`). Bare = self-install; --prefix <path> --module installs to a shared path and
// writes a Tcl modulefile for the users who follow. The tool list is embedded in mu
// (self-contained). On macOS mise can't cover the Intel-mac-only tools, so it prints the
// Homebrew bootstrap for you to run instead of driving brew silently.
func toolchainCmd() *cobra.Command {
	var t toolchain
	var dump bool
	c := &cobra.Command{
		Use:   "toolchain",
		Short: "Install the mu dev toolchain (via mise).",
		Long: "Install the base CLI toolchain with mise. Bare installs it for you; --prefix\n" +
			"<path> --module installs to a shared path and writes a Tcl modulefile so other\n" +
			"users can `module load` it. The tool list is embedded in mu — print it with\n" +
			"--dump-manifest. On macOS this prints the Homebrew + mise bootstrap to run.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if dump {
				fmt.Print(setup.Manifest())
				return nil
			}
			if t.module && t.prefix == "" {
				return usageErr("--module needs --prefix <path> (the shared install root)")
			}
			specs, err := setup.Specs()
			if err != nil {
				return fmt.Errorf("read toolchain manifest: %w", err)
			}
			t.specs = specs
			if runtime.GOOS == "darwin" {
				return t.darwin()
			}
			return t.linux()
		},
	}
	f := c.Flags()
	f.StringVar(&t.prefix, "prefix", "", "shared install root (MISE_DATA_DIR); default is mise's per-user dir")
	f.BoolVar(&t.module, "module", false, "write a Tcl modulefile pointing at --prefix (HPC deployer)")
	f.BoolVar(&t.withBrew, "with-brew", false, "on macOS, run the Homebrew bootstrap instead of just printing it")
	f.BoolVar(&t.dryRun, "dry-run", false, "print the plan without installing")
	f.BoolVar(&dump, "dump-manifest", false, "print the embedded toolchain manifest and exit")
	return c
}

type toolchain struct {
	prefix   string
	module   bool
	withBrew bool
	dryRun   bool
	specs    []string
}

// linux installs the toolchain with mise: bootstrap mise if absent, install the embedded
// tool specs (to --prefix when shared), reshim, and optionally write a Tcl modulefile so
// followers on a shared system can `module load` it.
func (t *toolchain) linux() error {
	mise, present := misePath()
	render.Info("toolchain plan (linux):")
	if present {
		render.Detail("  mise: " + mise)
	} else {
		render.Detail("  mise: not found → bootstrap (curl https://mise.run | sh)")
	}
	dest := t.prefix
	if dest == "" {
		dest = "(mise per-user default)"
	}
	render.Detail("  install root: " + dest)
	render.Detail("  tools: " + strings.Join(t.specs, ", "))
	if t.module {
		render.Detail("  modulefile: " + t.modulefilePath())
	}
	if t.dryRun {
		render.OK("dry-run — nothing installed")
		return nil
	}

	if !present {
		if err := bootstrapMise(); err != nil {
			return err
		}
		mise, _ = misePath()
	}
	env := os.Environ()
	if t.prefix != "" {
		env = append(env, "MISE_DATA_DIR="+t.prefix)
	}
	if err := runEnv(env, mise, append([]string{"install"}, t.specs...)...); err != nil {
		return fmt.Errorf("mise install: %w", err)
	}
	_ = runEnv(env, mise, "reshim")
	render.OK("installed toolchain: " + strings.Join(t.specs, ", "))
	if t.module {
		return t.writeModulefile()
	}
	return nil
}

// darwin prints the Homebrew + mise bootstrap (some tools are brew-only on Intel macs, so
// mise can't cover everything) — running it only with --with-brew. Never drives brew silently.
func (t *toolchain) darwin() error {
	const brew = "brew install mise git-delta difftastic"
	const activate = `eval "$(mise activate zsh)"`
	if !t.withBrew || t.dryRun {
		render.Info("macOS toolchain bootstrap (run these yourself):")
		render.Detail("  " + brew)
		render.Detail("  " + activate + "   # add to ~/.zshrc")
		if !t.withBrew {
			render.Info("re-run with --with-brew to execute the brew step")
		}
		return nil
	}
	if err := runEnv(os.Environ(), "brew", "install", "mise", "git-delta", "difftastic"); err != nil {
		return fmt.Errorf("brew install: %w", err)
	}
	render.OK("brew tools installed — add to ~/.zshrc: " + activate)
	return nil
}

// modulefilePath is where --module writes the Tcl modulefile under the shared prefix.
func (t *toolchain) modulefilePath() string {
	return filepath.Join(t.prefix, "modulefiles", "mu-toolchain")
}

// writeModulefile emits a minimal Tcl modulefile that puts the shared install on PATH —
// `module load mu-toolchain` after `module use <prefix>/modulefiles`.
func (t *toolchain) writeModulefile() error {
	path := t.modulefilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := "#%Module1.0\n" +
		"## mu dev toolchain — generated by `mu setup toolchain --module`\n" +
		"prepend-path PATH " + t.prefix + "/shims\n" +
		"setenv MISE_DATA_DIR " + t.prefix + "\n" +
		// The module-provided marker: the zsh MISE_ENV composition skips the hpc tier
		// when set, so a per-user `fmt` opt-in never re-installs module-owned base+hpc.
		// Doubles as the doctor's provider signal (module vs personal mise).
		"setenv MU_TOOLCHAIN " + t.prefix + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	render.OK("wrote modulefile: " + path + "  (module use " + filepath.Dir(path) + ")")
	return nil
}

// misePath finds mise on PATH, or at the ~/.local/bin/mise the bootstrap installs to.
// Returns the fallback name "mise" and false when neither exists.
func misePath() (string, bool) {
	if p, err := exec.LookPath("mise"); err == nil {
		return p, true
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", "mise")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true
		}
	}
	return "mise", false
}

// bootstrapMise runs the official `curl https://mise.run | sh` installer, its chatter
// routed to stderr so mu's stdout stays clean.
func bootstrapMise() error {
	render.Info("Bootstrapping mise (curl https://mise.run | sh)…")
	curl := exec.Command("curl", "-fsSL", "https://mise.run")
	sh := exec.Command("sh")
	pipe, err := curl.StdoutPipe()
	if err != nil {
		return err
	}
	sh.Stdin = pipe
	sh.Stdout, sh.Stderr = os.Stderr, os.Stderr
	if err := sh.Start(); err != nil {
		return err
	}
	if err := curl.Run(); err != nil {
		return fmt.Errorf("curl mise.run: %w", err)
	}
	if err := sh.Wait(); err != nil {
		return fmt.Errorf("mise install script: %w", err)
	}
	return nil
}

// runEnv runs a command with the given env, streaming its output to stderr.
func runEnv(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}
