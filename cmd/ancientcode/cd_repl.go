package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/antonioghizidev/ancientcode/internal/workspace"
)

// runCdREPL handles /cd in the line REPL.
//
// The confirmation lives here rather than in cdCommand because asking is an
// interaction with a person, and a pure function cannot ask. cdCommand decides
// and validates; this decides when to ask and what to print.
func runCdREPL(w *workspace.Workspace, cmd command, ask func(string) (string, error), persist rootPersister) {
	res := cdCommand(w, cmd.arg)
	if res.Err != nil {
		fmt.Fprintln(cmdErrOut(), "erro:", res.Message)
		return
	}
	// No argument: /cd is a question, and the answer IS the command.
	if cmd.arg == "" {
		for _, l := range strings.Split(res.Message, "\n") {
			fmt.Println(l)
		}
		return
	}
	confirmed := true
	if res.NeedsConfirm {
		// The resolved path is what gets approved, not what was typed: a
		// symlinked destination is not the folder the agent will write in.
		line, _ := ask(fmt.Sprintf("%s\nEntrar nessa pasta? [y/N] ", res.Message))
		confirmed = strings.EqualFold(strings.TrimSpace(line), "y")
		if !confirmed {
			fmt.Println("mantido em:", w.ProjectRoot())
			return
		}
	}
	out := res.Apply(w, confirmed)
	if out.Err != nil {
		fmt.Fprintln(cmdErrOut(), "erro:", out.Err)
		return
	}
	if !out.Changed {
		fmt.Println("ja estamos em", out.Root)
		return
	}
	persistRoot(persist, out.Root)
	fmt.Println("raiz do projeto:", out.Root)
}

// cmdErrOut lets tests capture the error stream without touching os.Stderr
// globally; in the REPL it is the process stderr.
func cmdErrOut() *os.File { return os.Stderr }
