package skills_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ancientai/internal/sandbox"
	"ancientai/internal/skills"
	"ancientai/internal/tools"
	sk "ancientai/internal/tools/skills"
)

// writeSkill materializes a loadable skill directory under root.
func writeSkill(t *testing.T, root, name string, scripts map[string]string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "instrucoes de teste\nnunca executar no load\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\nname: "+name+"\ndescription: skill de teste\nallowed_tools:\n  - read_file\n---\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	for n, c := range scripts {
		if err := os.WriteFile(filepath.Join(dir, "scripts", n), []byte(c), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func newTool(t *testing.T, set *skills.Set, workdir string) tools.Tool {
	t.Helper()
	ex := sandbox.NewExecutor(workdir, "", 5*time.Second, 64<<10)
	return sk.New(set, ex, 64<<10)
}

func loadIntoSet(t *testing.T, root, name string, set *skills.Set) {
	t.Helper()
	l, err := skills.Load(root, name)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Add(l); err != nil {
		t.Fatal(err)
	}
}

func TestSkillRunDeniesUnloadedSkill(t *testing.T) {
	tool := newTool(t, skills.NewSet(), "")
	var de *tools.DeniedError
	_, err := tool.Execute(context.Background(), []byte(`{"skill":"ghost","script":"a.sh"}`))
	if !errors.As(err, &de) {
		t.Fatalf("expected DeniedError, got %v", err)
	}
	if !strings.Contains(de.Reason, "nao esta ativa") {
		t.Fatalf("reason = %q", de.Reason)
	}
}

func TestSkillRunDeniesSkillWithoutScripts(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "noscr", nil)
	os.RemoveAll(filepath.Join(root, "noscr", "scripts"))
	set := skills.NewSet()
	loadIntoSet(t, root, "noscr", set)
	tool := newTool(t, set, "")
	var de *tools.DeniedError
	_, err := tool.Execute(context.Background(), []byte(`{"skill":"noscr","script":"x.sh"}`))
	if !errors.As(err, &de) {
		t.Fatalf("expected DeniedError, got %v", err)
	}
	if !strings.Contains(de.Reason, "nao possui scripts") {
		t.Fatalf("reason = %q", de.Reason)
	}
}

func TestSkillRunDeniesUnknownOrEscapingScript(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "dep", map[string]string{"ok.sh": "#!/bin/sh\necho ok\n"})
	set := skills.NewSet()
	loadIntoSet(t, root, "dep", set)
	tool := newTool(t, set, "")

	for _, script := range []string{"../evil.sh", "sub/../ok.sh", "ok2.sh", "/etc/passwd"} {
		var de *tools.DeniedError
		_, err := tool.Execute(context.Background(), []byte(`{"skill":"dep","script":"`+script+`"}`))
		if !errors.As(err, &de) {
			t.Fatalf("script %q: expected DeniedError, got %v", script, err)
		}
	}
}

func TestSkillRunDeniesOversizedArgv(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "dep", map[string]string{"ok.sh": "#!/bin/sh\necho ok\n"})
	set := skills.NewSet()
	loadIntoSet(t, root, "dep", set)
	tool := newTool(t, set, "")

	// Too many args.
	many := make([]string, 0, 70)
	for i := 0; i < 70; i++ {
		many = append(many, "a")
	}
	b, _ := jsonMarshal(skillArgs{"dep", "ok.sh", many})
	var de *tools.DeniedError
	if _, err := tool.Execute(context.Background(), b); !errors.As(err, &de) {
		t.Fatalf("too many args: expected DeniedError, got %v", err)
	}

	// One oversized argument.
	huge := []string{strings.Repeat("x", 20<<10)}
	b, _ = jsonMarshal(skillArgs{"dep", "ok.sh", huge})
	if _, err := tool.Execute(context.Background(), b); !errors.As(err, &de) {
		t.Fatalf("huge arg: expected DeniedError, got %v", err)
	}
}

func TestSkillRunRequiresSkillAndScript(t *testing.T) {
	tool := newTool(t, skills.NewSet(), "")
	if _, err := tool.Execute(context.Background(), []byte(`{"skill":""}`)); err == nil {
		t.Fatal("empty skill accepted")
	}
}

// TestSkillRunExecutesApprovedVector is Linux-only (real script execution in
// the sandbox); on the host it covers the deny-only paths.
func TestSkillRunExecutesApprovedVector(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real script execution requires a POSIX shell (run on the Linux gate)")
	}
	root := t.TempDir()
	writeSkill(t, root, "dep", map[string]string{"greet.sh": "#!/bin/sh\necho skill-ran $1\n"})
	set := skills.NewSet()
	loadIntoSet(t, root, "dep", set)
	tool := newTool(t, set, "")

	res, err := tool.Execute(context.Background(),
		[]byte(`{"skill":"dep","script":"greet.sh","argv":["alpha","beta"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, output = %q", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, "skill-ran alpha") {
		t.Fatalf("output = %q", res.Output)
	}
}

type skillArgs struct {
	Skill  string   `json:"skill"`
	Script string   `json:"script"`
	Argv   []string `json:"argv"`
}

func jsonMarshal(v skillArgs) ([]byte, error) {
	return json.Marshal(v)
}
