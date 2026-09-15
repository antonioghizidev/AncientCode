# Template — Spec de Milestone

Copiar para `specs/milestones/<NN>-<slug>.md` no início de cada milestone e preencher.

---

# M<N> — <Nome do milestone>

**Status**: ⏳ pendente | ▶ em andamento | ✅ concluído em <data>
**Depende de**: <M anterior ou "nenhuma">
**Sessão de referência**: (a preencher ao fechar)

## 1. Objetivo

<O que o milestone entrega em uma frase.>

## 2. Escopo / entregáveis

- [ ] <entregável 1>
- [ ] <entregável 2>

## 3. Contratos (interfaces públicas)

<Esboço de tipos/funções/fluxos que serão implementados. Referenciar docs/SPEC.md.>

## 4. Requisitos de segurança deste milestone

<O que pode dar errado / o que deve ser protegido (derivar de docs/SPEC.md §5).>

- [ ] <requisito de segurança 1>
- [ ] <requisito de segurança 2>

## 5. Testes obrigatórios

### Funcionais

- [ ] <teste funcional 1>
- [ ] <teste funcional 2>

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [ ] <teste de segurança 1>
- [ ] <teste de segurança 2>

## 6. Critérios de aceite (DoD)

- [ ] Implementação conforme escopo
- [ ] **Todos os testes do projeto passam** (`go test -count=1 ./...`)
- [ ] `gofmt`/`go vet`/`go build` limpos
- [ ] `docs/DECISOES.md` atualizado
- [ ] Nota de sessão em `docs/`
- [ ] Artefato do notebook (`GOAMD64=v1`) compilado quando aplicável
- [ ] Status deste arquivo e do `docs/SPEC.md` atualizados