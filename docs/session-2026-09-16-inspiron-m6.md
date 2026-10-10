# Sessão 2026-09-16 — validação no Inspiron (M1–M6) + tuning M6

> Resumo exclusivo desta sessão. Metodologia: `specs/methodology.md`.

## Objetivo da sessão

Validar M1–M6 no hardware-alvo real (Dell Inspiron 1545 via SSH, `USUARIO@SERVIDOR-LAN`,
Core 2 Duo T6600 / sem AVX / Ubuntu 24.04.5 LTS) com o binário `0.6.0-m6`, provider
`openrouter` + modelo `openrouter/free`, todas as tools habilitadas (`shell`, `docker`,
`apt`, `code_scaffold`, `edit_file`, `code_review`). Corrigir o que o smoke revelar e só
então avaliar M7.

## O que foi feito

- **Setup de acesso**: chave ed25519 de dev instalada para `USUARIO`; conectividade OK.
- **Ambiente alvo**: binário `dist/ancientcode` (0.6.0-m6) copiado e executando; usuário
  `ancientcode` (uid 1001) criado; `/opt/ancientcode/code` com dono `root:ancientcode`, `g+rwX`
  e setgid; exports de `OPENROUTER_API_KEY` no `~/.bashrc` do USUARIO (guard interativo de
  `.bashrc` impede source em `ssh host cmd`; smokes carregam as vars via `awk`).
- **Smoke A** (config/startup): config validada fail-closed (code_root strict-child do
  workdir), startup limpo, sessão `default.jsonl`, auditoria funcionando.
- **Smoke B** (scaffold real): modo plan refletiu exposição correta (só `read_file`/
  `list_dir`/`code_review`); após `y` no plano, `code_scaffold` aprovado e executado,
  criando `bark/{go.mod,cmd/bark/main.go,internal/answer.go,.gitignore,README.md}` no
  `code_root`; audição `policy=ask decision=allow status=executed` com output hash.
- **Smoke C/C2** (review read-only): primeira tentativa com path fora da árvore
  (`cmd/bark/main.go` sem prefixo `bark/`) → **confinamento strict-child recusou**
  (`status=error`, hash do vazio); com `bark/...` correto retornou `{"findings":null}` e o
  checksum do arquivo permaneceu — read-only comprovado em hardware real.
- **Smoke D** (edit_file real): gating M6 em ação — o modelo relatou no plano que
  `edit_file` não estava disponível (estava oculto) e, após o `y`, executou com aprovação;
  arquivo editado (`Hello AncientCode!`), audit `executed`. Revelou o **defeito do preview**
  (`+package internal` duplicado) → **corrigido** (D-2026-09-16-29, LCS stdlib) e binário
  re-implantado.
- **Smoke E** (shell + run_user): com `run_user: ancientcode` e harness rodando como USUARIO,
  `su - ancientcode -c 'id'` pediu senha → `Authentication failure` exit 1, `id` **nunca
  rodou** (fail-secure). O modelo *inventou* `uid=1000(admin)` na narração do resultado —
  validação ao vivo de que output de modelo é untrusted. Conclusão: implantar o harness **no
  usuário dedicado `ancientcode`** (D-2026-09-16-30). Com `run_user == atual`, o executor roda
  direto (validado de ponta a ponta no caminho docker).
- **Smoke F** (docker real): `docker ps` via tool listou os containers projeto reais;
  2 chamadas aprovadas+executadas+auditadas (<150 ms cada).
- **Smoke G** (apt real): `apt update` executado de verdade; sem root → exit 100 (lock de
  `/var/lib/apt/lists`), erro propagado com honestidade e auditado (D-2026-09-16-31).
  Em seguida o OpenRouter free atingiu **HTTP 429 (limite diário 50/dia esgotado)** — o loop
  tratou cada erro e continuou (resiliência).
- **Smoke H** (sessões): `--cleanup 30` removeu só a sessão envelhecida (opt-in explícito);
  `--resume default` preservou o histórico (114→114 linhas); `--resume + --reset` → recusado
  com exit 2. Acidente do próprio smoke (linha `dummy` injetada no JSONL) provou que história
  corrompida é recusada em fail-closed; o session foi reparado.
- **Smoke I** (--run): incompatível com tools mutantes habilitadas → recusado com exit 1.

## Decisões

- D-2026-09-16-29 (`edit_file` diff com contexto real, LCS stdlib).
- D-2026-09-16-30 (`run_user` exige usuário dedicado / processo privilegiado).
- D-2026-09-16-31 (apt não-root com efeito limitado; docker sem privilégio).

## Testes

| Pacote | Foco | Status |
|---|---|---|
| `internal/tools/code` (host) | `TestEditFile*` (incl. regressão do preview) | ✅ |
| `internal/tools/code` (WSL) | heurísticas POSIX reais | ✅ |
| Suíte completa (host) | `go test -count=1 ./...` | ✅ 178 pass / 14 skip |
| `gofmt`/`go vet`/`go build` | limites | ✅ |

Smoke no Inspiron: configuração+startup, scaffold, review (read-only + strict-child),
edit_file (gate+diff), docker real, apt real (non-root), `--cleanup`/`--resume`/`--reset`/
`--run` recusa — todos verificados. `shell`/`apt` de efeito completo adiados para a
implantação como usuário dedicado.

## Critérios de aceite

| Critério | Status |
|---|---|
| Binário 0.6.0-m6 roda no hardware-alvo (m1 sem AVX) | ✅ |
| M1–M6 validados ao vivo (smoke acima) | ✅ (com ajustes de implantação) |
| Defeito do preview `edit_file` corrigido + teste | ✅ |
| Documentação atualizada (DECISOES, esta nota, AGENTS.md) | ✅ |

## Próximos passos

1. Deploy no usuário dedicado: `sudo -u ancientcode` (ou login) e revalidar `shell` e `apt`
   com efeito real (3–5 min; depende do reset do quota free do OpenRouter ou de crédito).
2. Rodar um turno `--resume` completo quando o quota voltar (validar continuidade real da
   conversa, não só a preservação do arquivo).
3. Avaliar **M7 — Skills** (`specs/milestones/07-m7-skills.md`) com o gate dos anteriores
   fechado.

## Observações / riscos

- **OpenRouter free tier**: limite 50 req/dia — esgotou no meio da validação (429). Para o
  uso contínuo avaliar modelo pago barato ou NVIDIA NIM/LiteLLM local.
- **`su`/run_user**: fora de root, o drop de usuário requer senha; fica documentado que a
  implantação usa usuário dedicado (não é falha de confinamento: é falha-seguro).
- **Flake conhecido**: `TestExecBasic` deu um rerun vermelho uma vez no Windows (ver nota do
  M6); não reproduziu aqui.
- **Smoke scripts** ficaram em `~/smoke-*.sh` no Inspiron para reutilização.