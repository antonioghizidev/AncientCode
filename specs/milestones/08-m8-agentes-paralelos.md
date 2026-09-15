# M8 — Agentes em paralelo [futuro]

**Status**: ⏳ pendente (futuro — não iniciar até M7 concluído)
**Depende de**: M7
**Sessão de referência**: (a preencher ao fechar)

## 1. Objetivo

Orquestração de múltiplos agentes em paralelo (SPEC §11.2): worker pool, isolamento por
subdiretório, orquestrador e auditoria por agente — respeitando os 4 GB de RAM do alvo.

## 2. Escopo / entregáveis

- [ ] `internal/agents` — worker pool com `max_parallel` (default 2), cada agente com sessão,
      toolbox e gate de aprovação próprios.
- [ ] Orquestrador: divide prompt, consolida resultados, reconciliação.
- [ ] Subdiretório distinto por agente (sem colisão de escrita).
- [ ] Auditoria com `agent_id`; fail-fast em timeout/recurso; mutex na camada de escrita para
      tools mutantes (paralelismo seguro apenas em read-only).
- [ ] Limite global de RAM/CPU (config).

## 3. Contratos

- `Agent{ID, Session, Cfg, Workdir}`; `Orchestrator.Run(task) → []AgentResult` com
  reconciliação.
- Mensagens entre agentes só por arquivos/mensagens auditados (sem estado mutável
  compartilhado).

## 4. Requisitos de segurança deste milestone

- [ ] Nenhuma mutação sem aprovação **individual** (lote nunca implícito).
- [ ] Escrita fora do subdiretório do agente → rejeitada.
- [ ] Dados de um agente não vazam pro histórico do outro.
- [ ] Recursos limitados globalmente (ler RAM → encerra politicamente, sem OOM do host).

## 5. Testes obrigatórios

### Funcionais

- [ ] 2 agentes resolvem tarefas disjuntas em subdiretórios próprios.
- [ ] Orquestrador consolida e apresenta merge.

### Segurança (obrigatórios — devem falhar se a proteção for removida)

- [ ] Agente A tentando gravar no subdir de B → rejeitado (colisão de escrita).
- [ ] Mutação pedida em paralelo só é executada após aprovação individual de cada uma.
- [ ] Auditoria lista `agent_id` correto em cada linha.
- [ ] Timeout de um agente não aborta os outros (com política explícita); estado global não
      fica inconsistente.

## 6. Critérios de aceite (DoD)

- [ ] **Todos os testes do projeto passam** (`go test -count=1 ./...`).
- [ ] `gofmt`/`go vet`/`go build` limpos.
- [ ] `docs/DECISOES.md` atualizado.
- [ ] Nota de sessão criada.
- [ ] Status deste arquivo e de `docs/SPEC.md` atualizados.
- [ ] Aceite SPEC §11.3/§12 M8: 2 agentes sem colisão, sem aprovação implícita, auditoria
      completa.