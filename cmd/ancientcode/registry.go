package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/antonioghizidev/ancientcode/internal/audit"
	"github.com/antonioghizidev/ancientcode/internal/config"
	"github.com/antonioghizidev/ancientcode/internal/sandbox"
	"github.com/antonioghizidev/ancientcode/internal/skills"
	"github.com/antonioghizidev/ancientcode/internal/tools"
	apttools "github.com/antonioghizidev/ancientcode/internal/tools/apttools"
	"github.com/antonioghizidev/ancientcode/internal/tools/code"
	"github.com/antonioghizidev/ancientcode/internal/tools/docker"
	"github.com/antonioghizidev/ancientcode/internal/tools/files"
	"github.com/antonioghizidev/ancientcode/internal/tools/shell"
	skillstool "github.com/antonioghizidev/ancientcode/internal/tools/skills"
	"github.com/antonioghizidev/ancientcode/internal/workspace"
)

// newAuditRecorder opens the append-only audit log (SPEC §5.7).
//
// The path comes from cfg.Workdir — the AGENT HOME — and deliberately NOT from
// the project root. If it followed /cd, changing the root would open a fresh
// log file, i.e. a clean trail at exactly the moment the operator widens the
// blast radius. That is why this is a named function: the property is about
// THIS call site, and a test that built its own recorder from home would pass
// while main() did the wrong thing (D-75, for the third time).
func newAuditRecorder(cfg *config.Config) (*audit.Recorder, error) {
	return audit.NewRecorder(filepath.Join(cfg.Workdir, "audit.jsonl"), 10<<20, 5)
}

// buildRegistry assembles every tool the harness exposes, and the shared skills
// set that skill_run is bound to.
//
// It is a function rather than inline code in main() so a test can assert on
// what the MODEL can reach. The security claim of /cd is that no tool moves the
// project root — and a test that re-implements the registration in the test
// file would assert nothing about the real registry (this is the D-75 lesson:
// assert the call, not a copy of it).
func buildRegistry(cfg *config.Config, rootRef workspace.Ref, fs *files.Files) (*tools.Registry, *skills.Set, error) {
	reg := tools.NewRegistry()
	regAll := func(ts ...tools.Tool) error {
		for _, t := range ts {
			if err := reg.Register(t); err != nil {
				return err
			}
		}
		return nil
	}

	// Filesystem tools, rooted at the project root (SPEC §10.3, M15).
	if err := regAll(fs.ToolReadFile(), fs.ToolWriteFile(), fs.ToolListDir()); err != nil {
		return nil, nil, err
	}

	// M6: coding-agent tools (SPEC §10). code_review is read-only; scaffold and
	// edit always require human approval. Generated code is never executed.
	codeTools := code.NewToolsetLive(rootRef.CodeRoot, cfg.OutputMaxBytes)
	if err := regAll(codeTools.ToolCodeScaffold(), codeTools.ToolEditFile(), codeTools.ToolCodeReview()); err != nil {
		return nil, nil, err
	}

	if cfg.ShellTool == "enabled" {
		shellTimeout, err := cfg.ToolTimeout("shell")
		if err != nil {
			return nil, nil, fmt.Errorf("config: %w", err)
		}
		policy := sandbox.NewPolicy(sandbox.DefaultAllowed)
		exec := sandbox.NewExecutorLive(rootRef.Workdir, cfg.RunUser, shellTimeout, cfg.OutputMaxBytes)
		if err := regAll(shell.New(policy, exec, cfg.OutputMaxBytes)); err != nil {
			return nil, nil, err
		}
	}

	// M5: docker and apt only operate with human approval (ask), and only when
	// explicitly enabled. Both are argument-vector wrappers — model text never
	// becomes a shell line.
	if cfg.DockerTool == "enabled" {
		t, err := cfg.ToolTimeout("docker")
		if err != nil {
			return nil, nil, fmt.Errorf("config: %w", err)
		}
		if err := regAll(docker.New(t, cfg.OutputMaxBytes)); err != nil {
			return nil, nil, err
		}
	}
	if cfg.AptTool == "enabled" {
		t, err := cfg.ToolTimeout("apt")
		if err != nil {
			return nil, nil, fmt.Errorf("config: %w", err)
		}
		if err := regAll(apttools.New(t, cfg.OutputMaxBytes)); err != nil {
			return nil, nil, err
		}
	}

	// M7: skills (SPEC §11.1). One shared Set bonds the loop and skill_run;
	// skill_run is always registered but the loop only exposes it when a
	// scripted skill is active in run mode.
	set := skills.NewSet()
	exec := sandbox.NewExecutorLive(rootRef.Workdir, cfg.RunUser, 60*time.Second, cfg.OutputMaxBytes)
	if err := regAll(skillstool.New(set, exec, cfg.OutputMaxBytes)); err != nil {
		return nil, nil, err
	}
	return reg, set, nil
}
