package main

import "fmt"

// savedRootNotice explains that the project root remembered from the last
// session could not be used, and where the harness went instead.
//
// Silence here is the failure mode worth avoiding. The harness falls back to
// the config, the session starts, everything works — and the operator has no
// idea that the project they were working in yesterday is not the one the agent
// is touching today. That is a class of surprise the /cd milestone exists to
// remove, so the fallback says so out loud.
//
// All three facts matter: what was saved, why it failed, and where the agent
// will actually write.
func savedRootNotice(saved, reason, fallback string) string {
	return fmt.Sprintf(
		"aviso: a raiz do projeto guardada (%s) nao pode ser usada: %s.\n"+
			"  Usando a do config: %s\n"+
			"  Para conferir o que estava guardado: cat ~/.local/share/ancientcode/state.json\n"+
			"  Ou aponte manualmente: /cd <pasta>",
		saved, reason, fallback)
}
