# Sessão 2026-09-16 — indicadores de progresso no REPL

> Resumo exclusivo desta sessão. Metodologia: `specs/methodology.md`.

## Objetivo da sessão

Dar feedback visual no REPL durante turnos longos (cold start do NVIDIA NIM no Inspiron):
"sabemos quando está funcionando e quando há problema". Sem mudanças de segurança — tudo
permanece TTY-only e fail-closed.

## O que foi feito

- `internal/loop`: gancho opcional `Loop.SetStatus(func(string))` com notificação de fases de
  bloqueio e de problema:
  - `modelo: aguardando o primeiro token...` antes de cada chamada de provider;
  - `modelo: recebendo tokens...` no primeiro evento de conteúdo/reasoning (SSE `onEvent`);
  - `gate humano: aguardando sua aprovacao...` antes de um pedido de aprovação ask;
  - `""` (limpeza da linha) ao fim de cada fase — algumas fases foram testadas como
    subsequência `aguardando|recebendo` e presença de `gate humano`.
- `cmd/ancientcode/main.go` (REPL):
  - `isCharDevice(os.Stdout)` — indicadores só em terminal (console/tty); pipes e testes
    continuam com a saída limpa de sempre;
  - `renderStatus(phase)` imprime `\r>>> <fase>` e limpa com espaços quando `phase == ""`;
  - teto do turno: `3*time.Minute` → `5*time.Minute` (`turnTimeout`), alinhado ao
    D-2026-09-16-32 para não matar cold starts legítimos antes do provider;
  - `version` → `0.6.0-m6-ind`.
- Build Linux `GOAMD64=v1` (`dist/ancientcode`), implantado no Inspiron em
  `/home/ancientcode/ancientcode-0.6.0-m6-ind` (o `.bashrc`/atalhos que apontam para
  `ancientcode-0.6.0-m6` continuam funcionando; o antigo `m6` estava em uso — sessão aberta).

## Decisões

- D-2026-09-16-35 — fases visíveis no REPL, TTY-only, com teto de turno alinhado ao provider.

## Testes

| Pacote | Foco | Status |
|---|---|---|
| `internal/loop` | `TestStatusHookStreamsPhases`, `TestStatusHookReportsHumanGate` | ✅ |
| suíte completa | regressão geral | ✅ |

**Total: 15 pacotes verdes, 0 falhas** (host serial `-p 1` e Linux WSL `nb`: gofmt/vet/build
limpos; flake conhecido `TestExecBasic` do Windows isolado, verde em re-rodada — não
relacionado).

## Critérios de aceite

| Critério | Status |
|---|---|
| Suíte completa verde | ✅ |
| Testes de segurança do milestone presentes | ✅ (regressão de gate/stream intacta) |
| Documentação atualizada | ✅ (DECISOES D-35) |

## Próximos passos

1. Conferir os indicadores numa sessão real no Inspiron com NVIDIA NIM (turno longo mostra
   `aguardando o primeiro token` → `recebendo tokens`; falha de rede mostra `erro:`).
2. Decidir canonizar binário `0.6.0-m6-ind` em `/opt/ancientcode/bin` (sudo `install`) quando a
   sessão aberta no `m6` encerrar; `--resume` com conversa real na NIM.
3. Quando fechado, avaliar M7 — Skills (`specs/milestones/07-m7-skills.md`).

## Observações / riscos

- Validação pty feita em WSL com `script` (util-linux); no Inspiron o `script` é BusyBox e o
  `-qec` não existe — hábito de validar fases TTY em WSL.
- `TestExecBasic` (sandbox) segue flaky no Windows sob carga (helper de 1s); estruturado para
  rodar isolado/serial e estável no Linux.