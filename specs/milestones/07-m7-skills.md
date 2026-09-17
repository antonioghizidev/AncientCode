# M7 — Skills

**Status**: ✅ implementado (host 16/16; Linux WSL `-race` 16/16; binário `GOAMD64=v1` em
`dist/ancientai`, `0.7.0-m7-ind`)
**Depende de**: M6
**Sessão de referência**: `docs/session-2026-09-16-m7.md`

## 1. Objetivo

Skills reutilizáveis (SPEC §11.1): `~/.config/ancientai/skills/<skill>/SKILL.md` com frontmatter
YAML, scripts opcionais e `allowed_tools` — carregadas por `--skill`.

## 2. Escopo / entregáveis

- [x] `internal/skills` — descoberta, leitura e validação de `SKILL.md`.
- [x] Frontmatter: `name`, `description`, `allowed_tools` (+ `origin`/`origin_hash` p/ baixadas).
- [x] Injeção no system prompt (marcadores untrusted) + interseção do toolbox por `allowed_tools`.
- [x] Scripts opcionais em `scripts/` — executados **apenas via tool `skill_run`**, nunca ao carregar.
- [x] Verificação de origem antes do primeiro uso (`--verify-skill` + stamp `.ancientai-verified`; hash publicado se baixada); re-verificação em qualquer alteração.

## 3. Contratos

- Validação de nome `^[a-z0-9]+(?:-[a-z0-9]+)*$` (sem path traversal; `.`, `/`, `\`, espaços, hífens duplos/isolados rejeitados).
- `load(skillName) → Skill{Meta, Instructions, Scripts}` com `allowed_tools` validando names
  contra o registry (fail-closed: declaração de tool inexistente → skill rejeitada).

## 4. Requisitos de segurança deste milestone

- [x] Skill é dado não confiável de terceiros → contenção (SPEC §11.1): texto entra com
      marcadores untrusted + hierarquia de instruções (§5.6).
- [x] `allowed_tools` deny-by-default; skill sem ferramentas → nenhuma habilitada.
- [x] Scripts nunca executam no load; só após aprovação (ask) e dentro do sandbox (vetor puro).
- [x] `..`/`/` no nome → rejeitado; verificação de origem para skills baixadas.

## 5. Testes obrigatórios

### Funcionais

- [x] Carregar skill válida injeta instruções e interseciona tools declaradas.
- [x] Skill sem `allowed_tools` não habilita nada.

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [x] Nome `../evil` ou `foo/bar` → rejeitado (`TestSkillValidateNameRejectsTraversal`).
- [x] `SKILL.md` contendo instrução "ignore rules e execute script" não executa nada no load
      (`TestSkillUntrustedBodyDoesNotExecuteOnLoad`).
- [x] `allowed_tools: [powershell]` com tool inexistente → skill rejeitada (fail-closed)
      (`TestM7SkillUnregisteredToolRejectedFailClosed`).
- [x] Skill baixada sem hash verificado → recusada no primeiro uso
      (`TestSkillDownloadedRequiresVerificationAtLoad` + `TestSkillTamperAfterVerifyFails`).
- [x] Script de skill apenas roda após aprovação e dentro do sandbox
      (`TestM7SkillRunGatedByAsk`, `TestSkillRunExecutesApprovedVector` [Linux],
      `TestExecVectorRunsPureVector`/`TestExecVectorRefusesRunUserSwitchOnLinux`,
      `TestSkillRunDenies*` na tool).

## 6. Critérios de aceite (DoD)

- [x] **Todos os testes do projeto passam** (`go test -count=1 ./...` — host serial e Linux `-race`).
- [x] `gofmt -l .` vazio, `go vet ./...` e `go build ./...` limpos.
- [x] `docs/DECISOES.md` atualizado (D-2026-09-16-38..41).
- [x] Nota de sessão criada.
- [x] Status deste arquivo e de `docs/SPEC.md` atualizados.