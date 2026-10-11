package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Gate de privacidade: nada de caminho REAL de máquina em arquivo versionado.
//
// Existe porque vazou. O M15 escreveu o path verdadeiro do projeto do operador
// como fixture de teste (`/home/<usuario>/workspace/<projeto>`), e a nota de
// smoke trazia usuário + IP privado de LAN. Nada disso é necessário para nenhum
// teste passar, e num repositório público o conjunto "usuário + nome do projeto +
// caminho da pasta de trabalho" descreve onde a pessoa trabalha.
//
// A lição: eu não escolhi aquele path por engano, eu o escolhi porque era o
// exemplo "real" que tornava o teste convincente. Teste não precisa de dado
// real para ser convincente — precisa ser hermético. Um gate aqui é a diferença
// entre lembrar e não lembrar.

// reHomePath casa /home/<algo> onde <algo> NÃO é o usuário do próprio projeto
// (ancientcode) nem um placeholder.
//
// A exigencia de TERMINADOR depois do nome é o que separa um caminho real de prosa.
// Sem ela, "↑/↓/home/end CSI sequences" casaria com /home/end — e foi
// exatamente o falso positivo que a primeira versão do gate produziu. Um
// caminho de home é seguido de outra barra, de aspas ou de fim de linha; uma
// palavra em inglês no meio de uma frase, não.
var reHomePath = regexp.MustCompile(`/home/([a-z][a-z0-9_-]*)(?:/|$|["'` + "`" + `])`)

// reIPv4Any casa qualquer IPv4 com quatro octetos, para o filtro de faixa
// acontecer em código.
//
// A primeira versão embutia o prefixo no regex e errava o número de octetos:
// pedia três depois de "192.168" (cinco no total) e três depois de "10"
// (quatro). IPv4 tem QUATRO, sempre. Casar o genérico e filtrar em Go é o que
// a mutação [C] expôs — `SERVIDOR-LAN` passava direto.
var reIPv4Any = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)

// isPrivateIP reports whether ip is RFC1918.
func isPrivateIP(ip string) bool {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return false
	}
	a := parts[0]
	switch a {
	case "10":
		return true
	case "192":
		return parts[1] == "168"
	case "172":
		n := 0
		for _, c := range parts[1] {
			if c < '0' || c > '9' {
				return false
			}
			n = n*10 + int(c-'0')
		}
		return n >= 16 && n <= 31
	}
	return false
}

// reWindowsUser casa C:\Users\<nome>: o dev machine é Windows neste projeto,
// e o caminho do usuário do dev também é dado de máquina.
var reWindowsUser = regexp.MustCompile(`(?i)[A-Z]:\\Users\\[A-Za-z0-9._-]+`)

// homePathAllowed is the short allowlist. Everything else under /home is a
// real machine and must not be committed.
func homePathAllowed(user string) bool {
	switch user {
	case "ancientcode": // o run_user padrao deste projeto
		return true
	}
	// Placeholders em maiúsculas sao a convencao dos testes.
	if strings.ToUpper(user) == user && len(user) > 2 {
		return true
	}
	return false
}

func TestNoRealMachinePathsInVersionedFiles(t *testing.T) {
	root, rerr := repoRoot()
	if rerr != nil {
		t.Fatal(rerr)
	}
	var problems []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "node_modules" || base == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		// O proprio gate se declara: ele contem os padroes que procura.
		if strings.HasSuffix(filepath.ToSlash(path), "privacy_gate_test.go") {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".md" && ext != ".yaml" && ext != ".yml" &&
			ext != ".json" && ext != ".toml" && ext != ".sh" && ext != "" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil // arquivo inacessivel nao e motivo para falhar o gate
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		for _, m := range reHomePath.FindAllStringSubmatch(string(raw), -1) {
			if !homePathAllowed(m[1]) {
				problems = append(problems, rel+": /home/"+m[1]+" (caminho de maquina real?)")
			}
		}
		for _, m := range reIPv4Any.FindAllString(string(raw), -1) {
			if isPrivateIP(m) {
				problems = append(problems, rel+": "+m+" (IP privado de LAN)")
			}
		}
		for _, m := range reWindowsUser.FindAllString(string(raw), -1) {
			problems = append(problems, rel+": "+m+" (caminho do usuario do dev)")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("varrer a arvore: %v", err)
	}

	if len(problems) > 0 {
		t.Fatalf("%d ocorrencia(s) de dado de maquina em arquivo versionado:\n  %s\n"+
			"Use placeholders: /home/USUARIO/workspace/PROJETO, SERVIDOR-LAN.",
			len(problems), strings.Join(problems, "\n  "))
	}
}
