# Metodologia de trabalho — Spec-Driven Development (SDD)

Toda sessão de desenvolvimento segue **esta** metodologia. Um milestone só é considerado
concluído quando todos os critérios da spec do milestone e o gate de aceite (§7) forem
atingidos.

## Princípios do SDD

1. **Spec primeiro**: o comportamento desejado (incluindo o comportamento de segurança) é
   escrito na spec do milestone antes do código. O código implementa a spec, não o contrário.
2. **Testes guiam o código**: testes unitários (incluindo os de segurança) são escritos junto
   com (ou antes de) cada unidade.
3. **Milestones fecháveis**: cada milestone entrega valor utilizável e termina com a suíte
   inteira verde.
4. **Sessões limpas**: qualquer sessão futura deve conseguir continuar lendo apenas os
   artefatos do repositório (AGENTS.md, constitution, methodology, spec do milestone).

## Fases de uma sessão

### Fase 0 — Preparação (leitura antes de escrever)

- Ler `AGENTS.md` e `specs/README.md`.
- Ler `specs/constitution.md` e `specs/methodology.md` (este arquivo).
- Ler a spec do **milestone ativo** em `specs/milestones/` e conferir o status no
  `docs/SPEC.md` (§12) e nas notas de sessão anteriores.
- Confirmar o critério de "pronto" do milestone e a lista de testes de segurança.

### Fase 1 — Análise e decomposição

- Traduzir o escopo da spec em uma lista de tarefas (todo list).
- Identificar: contratos públicos (tipos/funções), pontos de extensão e a lista completa de
  **casos de segurança obrigatórios** (da spec do milestone — caso a spec não os enumere,
  derivar do modelo de ameaça de `docs/SPEC.md` §5).
- Registrar decisões de design relevantes em `docs/DECISOES.md` antes de implementar.

### Fase 2 — TDD por unidade

Para cada unidade (pacote/função/ferramenta):

1. Escrever o teste (ou descrever o teste de segurança) que **falha hoje**.
2. Implementar o mínimo necessário para o teste passar.
3. Rodar o teste específico:
   `go test -count=1 ./internal/<pkg>/`.
4. Repetir até a unidade estar coberta. Não "empurrar" teste para depois.

### Fase 3 — Testes de segurança (obrigatórios)

- Implementar/adaptar **todos** os testes de segurança enumerados na spec do milestone.
- Regra de qualidade: um teste de segurança deve **falhar se a proteção for removida**
  (ex.: apagar a redação, trocar fail-closed por erro aberto, permitir http não-loopback).
- Rodar a suíte de segurança isolada para diagnóstico e depois o conjunto completo.

### Fase 4 — Verificação de qualidade

```sh
gofmt -l .            # deve listar nada
go vet ./...
go build ./...
go test -count=1 ./...
```

- Quando o ambiente tiver toolchain C (Linux/CI): `go test -race -count=1 ./...`.
- Quando o milestone exigir build do artefato do notebook:
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags="-s -w" -o dist/ancientcode ./cmd/ancientcode`.

### Fase 5 — Documentação

- Atualizar `docs/DECISOES.md` com decisões novas.
- Criar/atualizar nota de sessão em `docs/session-<data>-<slug>.md` (usar
  `specs/templates/session.md`).
- Marcar apenas o que realmente foi feito (checkboxes `[x]`); nunca marcar antes da fase 6.

### Fase 6 — Gate de aceite (DoD)

> **Critério obrigatório: todos os testes do projeto passam ao final da implementação.**

- [ ] `gofmt -l .` vazio
- [ ] `go vet ./...` limpo
- [ ] `go build ./...` limpo
- [ ] `go test -count=1 ./...` — **0 falhas**
- [ ] Testes de segurança do milestone presentes e passando (e que **falhariam se a proteção
      fosse removida**)
- [ ] `docs/DECISOES.md` atualizado
- [ ] Nota de sessão criada em `docs/`
- [ ] Spec do milestone atualizada (status/checkboxes)
- [ ] Diff revisado (`git diff`), sem segredos
- [ ] Compilação do artefato Linux (`GOAMD64=v1`) verificada quando aplicável

### Fase 7 — Commit

- Mensagem padronizada: `feat(M<N>): <resumo em minúsculas, imperativo>`.
  Ex.: `feat(M2): tool registry, files tool e fallback JSON de tool calling`.
- Commitar **apenas** o que pertence ao milestone; revisar `git status` e `git diff`.
- Nunca commitar segredos, binários ou estado de editor (`docs/.obsidian/`).

## Regras transversais

- **Deny-by-default**: qualquer relaxamento de segurança é mudança de comportamento e exige
  decisão registrada, além de preservar parsers/políticas em camadas.
- **Redação antes de tudo que sai**: logs, persistência, envio ao modelo.
- **Fail-closed**: dúvida de parser → erro; tool não registrada → recusa; milestone anterior
  não concluído → não avançar.
- **Um milestone por vez**: o milestone ativo é definido no `docs/SPEC.md` (§12). Só iniciar um
  novo após o anterior cumprir o gate (M7/M8 são "futuro" e ficam traveados até isso).

## Próximos passos para cada nova sessão

1. Abrir M2 — ver `specs/milestones/02-m2-tool-loop.md` e `docs/session-*.md` da sessão anterior.
2. Seguir fases 0→7; ao final, registrar na nota de sessão e atualizar o status no `docs/SPEC.md`.