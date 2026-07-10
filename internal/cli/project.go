package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mayhl/mayhl_utils/internal/config"
	"github.com/mayhl/mayhl_utils/internal/hpc"
	"github.com/mayhl/mayhl_utils/internal/project"
	"github.com/mayhl/mayhl_utils/internal/queue"
	"github.com/mayhl/mayhl_utils/internal/render"
	"github.com/mayhl/mayhl_utils/internal/rsync"
	"github.com/mayhl/mayhl_utils/internal/shell"
)

// projectCmd is `mu project`: verbs over the project-structure contract (submit,
// later sync/pull/runs/clone). Gated behind MU_MODULES=project while it settles.
func projectCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "project",
		Short: "Project-layer verbs: push-and-run cases, data-tier sync.",
	}
	c.AddCommand(projectSubmitCmd())
	return c
}

func projectSubmitCmd() *cobra.Command {
	var node, script, account, queue_ string
	var yes, dryRun, verbose, keep bool
	c := &cobra.Command{
		Use:   "submit <case-dir>",
		Short: "Push a case to a cluster's $WORK staging and submit it (iterate mode).",
		Long: "The edit→run debug loop: rsync the case dir to the target's $WORKDIR at the\n" +
			"same $HOME-relative path (the remote git clone is never touched), drop a\n" +
			"submit-origin stamp (HEAD sha + dirty — `mu job prep` folds it into run.toml),\n" +
			"and qsub/sbatch the script from staging. Staging mirrors the case dir exactly\n" +
			"(stale files are deleted; scheduler logs from prior runs are kept) — it is the\n" +
			"disposable submit copy, not the authored source. --clean (commit-gated,\n" +
			"git-mediated study mode) comes later.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return projectSubmit(node, args[0], script, account, queue_, yes, dryRun, verbose, keep)
		},
	}
	f := c.Flags()
	f.StringVarP(&node, "node", "N", "", "cluster to target (required)")
	f.StringVarP(&script, "script", "s", "run.sh", "job script inside the case dir")
	f.StringVarP(&account, "account", "A", "", "allocation to charge (overrides the cluster's config default)")
	f.StringVarP(&queue_, "queue", "q", "", "queue / partition to submit to")
	f.BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	f.BoolVarP(&dryRun, "dry-run", "n", false, "show the plan without pushing or submitting")
	f.BoolVarP(&verbose, "verbose", "v", false, "per-file rsync output instead of the aggregate bar")
	f.BoolVar(&keep, "keep-extra", false, "keep staging files the case dir no longer has (skip rsync --delete)")
	_ = c.RegisterFlagCompletionFunc("node", func(_ *cobra.Command, _ []string, tc string) ([]string, cobra.ShellCompDirective) {
		return hpc.CompleteNode(tc), cobra.ShellCompDirectiveNoFileComp
	})
	return c
}

// stageProtect keeps --delete away from what staging legitimately accumulates
// beyond the case dir: scheduler stdout/stderr drops from prior runs (debug
// evidence; same shapes prep's copy skips) and the submit-origin stamp.
var stageProtect = []string{
	"--filter=P *.o[0-9]*",
	"--filter=P *.e[0-9]*",
	"--filter=P slurm-*.out",
	"--filter=P /" + project.StampFile,
}

// projectSubmit is the iterate-mode pipeline: resolve → preview+confirm → make
// staging (leg 1) → rsync (leg 2) → stamp + submit (leg 3).
func projectSubmit(node, caseDir, script, account, queue_ string, yes, dryRun, verbose, keep bool) error {
	caseAbs, err := filepath.Abs(caseDir)
	if err != nil {
		return usageErr("%s", err)
	}
	if fi, err := os.Stat(caseAbs); err != nil || !fi.IsDir() {
		return usageErr("%s is not a directory", caseDir)
	}
	if _, err := os.Stat(filepath.Join(caseAbs, script)); err != nil {
		return usageErr("script %s not found in %s", script, caseDir)
	}
	if _, err := project.FindRoot(caseAbs); err != nil {
		return usageErr("%s", err)
	}
	rel, err := project.HomeRel(caseAbs)
	if err != nil {
		return usageErr("%s", err)
	}
	if node == "" {
		return usageErr("needs --node <cluster> — submit runs from the authoring machine")
	}
	target, err := hpc.Resolve(node)
	if err != nil {
		return usageErr("%s", err)
	}
	scheduler := config.SchedulerFor(node)
	adapter := queue.For(scheduler)
	if adapter == nil {
		return errNoScheduler(node)
	}
	if account == "" {
		account = config.AccountFor(node)
	}
	opts := queue.SubmitOpts{Account: account, Queue: queue_}
	submitCmd := adapter.SubmitCmd(script, opts)
	stamp := project.NewStamp(caseAbs)

	render.Info(fmt.Sprintf("Submit case → %s (%s)", node, scheduler))
	render.Detail("case:    " + rel)
	render.Detail("stage:   $WORKDIR/" + rel)
	render.Detail("script:  " + script)
	if stamp.Commit != "" {
		render.Detail(fmt.Sprintf("origin:  %.12s dirty=%v", stamp.Commit, stamp.Dirty))
	} else {
		render.Detail("origin:  no commit yet — recorded dirty")
	}
	if d := adapter.Directives(opts); len(d) > 0 {
		render.Detail("applies: " + strings.Join(d, "  "))
	} else {
		render.Detail("applies: (scheduler defaults / script directives)")
	}
	render.Detail("command: " + submitCmd)
	if dryRun {
		render.Info("dry run — nothing pushed or submitted")
		return nil
	}
	if !yes {
		fmt.Fprintf(os.Stderr, "push + submit to %s? [y/N] ", node)
		var r string
		_, _ = fmt.Scanln(&r)
		if strings.ToLower(strings.TrimSpace(r)) != "y" {
			render.Info("aborted")
			return nil
		}
	}
	if err := hpc.EnsureTicket(); err != nil {
		return runErr("%s", err)
	}

	// Leg 1: create staging and resolve $WORKDIR remotely (the laptop can't know it).
	qrel := shell.Quote(rel)
	out, err := hpc.RemoteExec(target, fmt.Sprintf(`mkdir -p "$WORKDIR"/%s && cd "$WORKDIR"/%s && pwd`, qrel, qrel))
	if err != nil {
		return runErr("%s: staging dir: %s", node, err)
	}
	stage := strings.TrimSpace(out)
	if stage == "" {
		return runErr("%s: staging dir: empty $WORKDIR resolution", node)
	}

	// Leg 2: mirror the case dir into staging.
	o := rsync.Opts{Delete: !keep, PartialDir: true}
	if !keep {
		o.Ropt = stageProtect
	}
	label := "stage " + node
	code, _ := rsync.Run(rsync.BuildArgs(caseAbs+"/", target+":"+stage+"/", o), label, verbose)
	if code != 0 {
		render.EventErr("project", fmt.Sprintf("%s FAILED (rsync exit %d)", label, code))
		return codeErr(code)
	}

	// Leg 3: drop the stamp and submit from staging.
	out, err = hpc.RemoteExec(target, fmt.Sprintf("cd %s && printf '%%s' %s > %s && %s",
		shell.Quote(stage), shell.Quote(stamp.TOML()), project.StampFile, submitCmd))
	if err != nil {
		return runErr("%s: submit: %s", node, err)
	}
	if s := strings.TrimSpace(out); s != "" {
		render.Detail(s)
	}
	msg := "submitted " + rel + " → " + node
	render.OK(msg)
	render.EventOK("project", msg)
	return nil
}
