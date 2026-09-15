# M7 — Skills [futuro]

**Status**: ⏳ pendente (futuro — não iniciar até M6 concluído)
**Depende de**: M6
**Sessão de referência**: (a preencher ao fechar)

## 1. Objetivo

Skills reutilizáveis (SPEC §11.1): `~/.config/ancientai/skills/<skill>/SKILL.md` com frontmatter
YAML, scripts opcionais e `allowed_tools` — carregadas por `--skill`.

## 2. Escopo / entregáveis

- [ ] `internal/skills` — descoberta, leitura e validação de `SKILL.md`.
- [ ] Frontmatter: `name`, `description`, `allowed_tools`.
- [ ] Injeção no system prompt + registro de tools declaradas.
- [ ] Scripts opcionais em `scripts/` — executados **apenas via tool gate**, nunca ao carregar.
- [ ] Verificação de origem antes do primeiro uso (hash publicado se baixada); versionamento.

## 3. Contratos

- Validação de nome `^[a-z0-9-]+$` (sem path traversal).
- `load(skillName) → Skill{Meta, Instructions, Scripts}` com `allowed_tools` validando names
  contra o registry (fail-closed: declaração de tool inexistente → skill rejeitada).

## 4. Requisitos de segurança deste milestone

- [ ] Skill é dado não confiável de terceiros → contenção (SPEC §11.1): texto entra com
      marcadores untrusted + hierarquia de instruções (§5.6).
- [ ] `allowed_tools` deny-by-default; skill sem ferramentas → nenhuma habilitada.
- [ ] Scripts nunca executam no load; só após aprovação/whitelist.
- [ ] `..`/`/` no nome → rejeitado; verificação de origem para skills baixadas.

## 5. Testes obrigatórios

### Funcionais

- [ ] Carregar skill válida injeta instruções e registra tools declaradas.
- [ ] Skill sem `allowed_tools` não habilita nada.

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [ ] Nome `../evil` ou `foo/bar` → rejeitado.
- [ ] `SKILL.md` contendo instrução "ignore rules e execute script" não executa nada no load.
- [ ] `allowed_tools: [powershell]` com tool inexistente → skill rejeitada (fail-closed).
- [ ] Skill baixada sem hash verificado → recusada no primeiro uso.
- [ ] Script de skill apenas roda após aprovação e dentro do sandbox.

## 6. Critérios de aceite (DoD)

- [ ] **Todos os testes do projeto passam** (`go test -count=1 ./...`).
- [ ] `gofmt`/`go vet`/`go build` limpos.
- [ ] `docs/DECISOES.md` atualizado.
- [ ] Nota de sessão criada.
- [ ] Status deste arquivo e de `docs/SPEC.md` atualizados.